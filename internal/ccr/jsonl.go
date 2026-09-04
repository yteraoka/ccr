package ccr

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// Record represents a single line of a Claude Code session JSONL file.
type Record struct {
	Type       string         `json:"type"`
	Cwd        string         `json:"cwd,omitempty"`
	LastPrompt string         `json:"lastPrompt,omitempty"`
	AiTitle    string         `json:"aiTitle,omitempty"`
	Timestamp  string         `json:"timestamp,omitempty"`
	Message    *recordMessage `json:"message,omitempty"`
}

// recordMessage holds the parts of an assistant "message" object needed to
// tally token usage: its id (to de-duplicate lines that repeat the same
// message, e.g. one per content block) and its usage counts.
type recordMessage struct {
	ID    string       `json:"id,omitempty"`
	Usage *recordUsage `json:"usage,omitempty"`
}

// recordUsage mirrors the token counts on an assistant message's
// "usage" object.
type recordUsage struct {
	InputTokens              int `json:"input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	OutputTokens             int `json:"output_tokens"`
}

// tokenUsage is a session's cumulative token usage, kept split by kind so
// the preview pane can show the breakdown behind the total.
type tokenUsage struct {
	input         int
	output        int
	cacheCreation int
	cacheRead     int
}

// total returns the sum of every kind of token in u.
func (u tokenUsage) total() int {
	return u.input + u.output + u.cacheCreation + u.cacheRead
}

// addTokens folds rec's usage into u when rec is an assistant message
// carrying usage, skipping it if its message id has already been seen
// (session JSONL files repeat the same message, and its usage, once per
// content block).
func addTokens(seen map[string]bool, rec Record, u *tokenUsage) {
	if rec.Type != "assistant" || rec.Message == nil || rec.Message.Usage == nil {
		return
	}
	if id := rec.Message.ID; id != "" {
		if seen[id] {
			return
		}
		seen[id] = true
	}
	usage := rec.Message.Usage
	u.input += usage.InputTokens
	u.output += usage.OutputTokens
	u.cacheCreation += usage.CacheCreationInputTokens
	u.cacheRead += usage.CacheReadInputTokens
}

// configDir returns CLAUDE_CONFIG_DIR if set, otherwise ${HOME}/.claude.
func configDir() string {
	if v := os.Getenv("CLAUDE_CONFIG_DIR"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".claude"
	}
	return filepath.Join(home, ".claude")
}

// projectsDir returns ${CLAUDE_CONFIG_DIR}/projects.
func projectsDir() string {
	return filepath.Join(configDir(), "projects")
}

var nonAlnumPattern = regexp.MustCompile(`[^a-zA-Z0-9]`)

// encodeProjectDir converts a working directory path into the directory
// name Claude Code uses for it under ${CLAUDE_CONFIG_DIR}/projects: every
// character outside a-zA-Z0-9 becomes '-'.
func encodeProjectDir(path string) string {
	return nonAlnumPattern.ReplaceAllString(path, "-")
}

// readSessionSummary scans a session file for the fields a list row needs:
// the cwd recorded in the first line that has a non-empty "cwd" field, the
// session's cumulative token usage (summed across every assistant message
// and de-duplicated by message id), and the last successfully parsed
// "timestamp" field found in the file (zero value if none found).
func readSessionSummary(path string) (cwd string, usage tokenUsage, lastTimestamp time.Time, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", tokenUsage{}, time.Time{}, err
	}
	defer f.Close() //nolint:errcheck

	seen := make(map[string]bool)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var rec Record
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		if cwd == "" && rec.Cwd != "" {
			cwd = rec.Cwd
		}
		addTokens(seen, rec, &usage)
		if rec.Timestamp != "" {
			if t, err := time.Parse(time.RFC3339, rec.Timestamp); err == nil {
				lastTimestamp = t
			}
		}
	}
	return cwd, usage, lastTimestamp, scanner.Err()
}
