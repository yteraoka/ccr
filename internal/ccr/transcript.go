package ccr

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"time"
)

// transcriptEntry is one rendered turn in the transcript: either a human
// (or injected system/meta) message, or an assistant turn made of
// text/thinking/tool_use blocks.
type transcriptEntry struct {
	role   string // "user" or "assistant"
	isMeta bool
	// isNotification marks a "user" line the harness injected rather than
	// the human typing it: a sub agent's report or a monitor event. They
	// arrive as ordinary user lines, so without this they would be
	// attributed to the human.
	isNotification bool
	timestamp      time.Time
	blocks         []transcriptBlock
}

// transcriptBlock is one piece of an entry: prose text, a thinking block,
// or a tool call (with its correlated result, if one was found).
type transcriptBlock struct {
	kind string // "text", "thinking", "tool_use"
	text string // for "text"/"thinking"

	toolUseID string
	toolName  string
	input     json.RawMessage
	outcome   *toolOutcome // nil if no matching tool_result was found
}

// toolOutcome holds the result of a tool_use, gathered from the "user" line
// (with array content) whose tool_result.tool_use_id matches the tool_use
// block's id.
type toolOutcome struct {
	// resultContent is the tool_result block's own "content" field: either
	// a JSON string or an array of {"type":"text","text":...} blocks.
	resultContent json.RawMessage
	isError       bool
	// toolUseResult is Claude Code's own structured, tool-specific summary
	// of the result (richer than resultContent), when present.
	toolUseResult json.RawMessage
}

// rawLine is the subset of a jsonl line needed to route parsing.
type rawLine struct {
	Type   string `json:"type"`
	IsMeta bool   `json:"isMeta"`
	// PromptSource says where a user line came from: "typed",
	// "suggestion_accepted" and "queued" are the human, while "system" is
	// the harness injecting a notification.
	PromptSource  string          `json:"promptSource"`
	Timestamp     string          `json:"timestamp"`
	Message       json.RawMessage `json:"message"`
	ToolUseResult json.RawMessage `json:"toolUseResult"`
}

// promptSourceSystem is the promptSource of a user line the harness
// injected on the human's behalf, such as a sub agent's result report or a
// monitor event.
const promptSourceSystem = "system"

type rawMessage struct {
	Content json.RawMessage `json:"content"`
}

type rawContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

// isJSONArray reports whether raw's first non-whitespace byte is '[', i.e.
// it decodes to a JSON array rather than a string/object/etc.
func isJSONArray(raw json.RawMessage) bool {
	b := bytes.TrimLeft(raw, " \t\r\n")
	return len(b) > 0 && b[0] == '['
}

// parseTranscript reads a session jsonl file end to end and returns the
// entries worth rendering as a conversation: assistant turns, and user
// turns that carry actual text (real prompts, or injected system/meta
// text such as system-reminders). User lines whose content is only
// tool_result blocks are not entries of their own — their results are
// attached to the matching tool_use block instead.
func parseTranscript(path string) ([]transcriptEntry, error) {
	lines, err := readJSONLLines(path)
	if err != nil {
		return nil, err
	}

	outcomes := make(map[string]toolOutcome)
	for _, raw := range lines {
		if raw.Type != "user" {
			continue
		}
		var msg rawMessage
		if err := json.Unmarshal(raw.Message, &msg); err != nil {
			continue
		}
		if !isJSONArray(msg.Content) {
			continue
		}
		var blocks []rawContentBlock
		if err := json.Unmarshal(msg.Content, &blocks); err != nil {
			continue
		}
		for _, b := range blocks {
			if b.Type != "tool_result" || b.ToolUseID == "" {
				continue
			}
			outcomes[b.ToolUseID] = toolOutcome{
				resultContent: b.Content,
				isError:       b.IsError,
				toolUseResult: raw.ToolUseResult,
			}
		}
	}

	var entries []transcriptEntry
	for _, raw := range lines {
		var ts time.Time
		if raw.Timestamp != "" {
			ts, _ = time.Parse(time.RFC3339, raw.Timestamp)
		}

		switch raw.Type {
		case "assistant":
			entry, ok := parseAssistantLine(raw, ts, outcomes)
			if ok {
				entries = append(entries, entry)
			}
		case "user":
			entry, ok := parseUserLine(raw, ts)
			if ok {
				entries = append(entries, entry)
			}
		}
	}

	return entries, nil
}

func parseAssistantLine(raw rawLine, ts time.Time, outcomes map[string]toolOutcome) (transcriptEntry, bool) {
	var msg rawMessage
	if err := json.Unmarshal(raw.Message, &msg); err != nil {
		return transcriptEntry{}, false
	}
	var blocks []rawContentBlock
	if err := json.Unmarshal(msg.Content, &blocks); err != nil {
		return transcriptEntry{}, false
	}

	entry := transcriptEntry{role: "assistant", timestamp: ts}
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if b.Text == "" {
				continue
			}
			entry.blocks = append(entry.blocks, transcriptBlock{kind: "text", text: b.Text})
		case "thinking":
			if b.Thinking == "" {
				continue
			}
			entry.blocks = append(entry.blocks, transcriptBlock{kind: "thinking", text: b.Thinking})
		case "tool_use":
			tb := transcriptBlock{kind: "tool_use", toolUseID: b.ID, toolName: b.Name, input: b.Input}
			if o, ok := outcomes[b.ID]; ok {
				tb.outcome = &o
			}
			entry.blocks = append(entry.blocks, tb)
		}
	}
	return entry, len(entry.blocks) > 0
}

func parseUserLine(raw rawLine, ts time.Time) (transcriptEntry, bool) {
	var msg rawMessage
	if err := json.Unmarshal(raw.Message, &msg); err != nil {
		return transcriptEntry{}, false
	}
	if isJSONArray(msg.Content) {
		return transcriptEntry{}, false // tool_result carrier, already consumed
	}
	var text string
	if err := json.Unmarshal(msg.Content, &text); err != nil || text == "" {
		return transcriptEntry{}, false
	}
	return transcriptEntry{
		role:           "user",
		isMeta:         raw.IsMeta,
		isNotification: raw.PromptSource == promptSourceSystem,
		timestamp:      ts,
		blocks:         []transcriptBlock{{kind: "text", text: text}},
	}, true
}

// readJSONLLines reads every non-empty line of path and decodes it into a
// rawLine, skipping lines that fail to parse.
func readJSONLLines(path string) ([]rawLine, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck

	var lines []rawLine
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var raw rawLine
		if err := json.Unmarshal(line, &raw); err != nil {
			continue
		}
		lines = append(lines, raw)
	}
	return lines, scanner.Err()
}
