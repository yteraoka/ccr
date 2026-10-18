package ccr

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// RunImport implements `ccr import`: it reads the session transcripts
// already on disk into the history database, so what was said before the
// hook was set up is searchable too. Running it again only picks up what
// is new.
func RunImport(args []string) error {
	fs := flag.NewFlagSet("ccr import", flag.ExitOnError)
	all := fs.Bool("A", false, "import from every config directory in the config file, not just the current CLAUDE_CONFIG_DIR")
	noEmbed := fs.Bool("no-embed", false, "do not embed the imported messages now (ccr search will)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: ccr import [-A] [-no-embed]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument: %s", fs.Arg(0))
	}

	dirs := []string{configDir()}
	if *all {
		var err error
		if dirs, err = configDirs(); err != nil {
			return err
		}
	}
	b, err := openHistoryBackend()
	if err != nil {
		return err
	}
	defer b.h.Close() //nolint:errcheck

	var files []transcriptFile
	for _, dir := range dirs {
		files = append(files, listTranscripts(dir)...)
	}

	sessions, added, unchanged := 0, 0, 0
	for i, f := range files {
		fmt.Fprintf(os.Stderr, "\rimporting %d/%d sessions...", i+1, len(files))
		same, err := b.h.importedFileUnchanged(f.path, f.size, f.mtime)
		if err != nil {
			return err
		}
		if same {
			unchanged++
			continue
		}
		msgs, err := extractHistory(f.path, f.configDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\nccr: %s: %v\n", f.path, err)
			continue
		}
		n, err := b.h.importMessages(f.path, f.size, f.mtime, msgs)
		if err != nil {
			return err
		}
		if n > 0 {
			sessions++
			added += n
		}
	}
	if len(files) > 0 {
		fmt.Fprintln(os.Stderr)
	}
	fmt.Fprintf(os.Stderr, "imported %d messages from %d sessions (%d transcripts unchanged since the last import)\n",
		added, sessions, unchanged)

	if b.e == nil || *noEmbed {
		return nil
	}
	if err := b.h.prepareVectors(b.e.key()); err != nil {
		return err
	}
	return b.h.embedPending(context.Background(), b.e, func(done, total int) {
		fmt.Fprintf(os.Stderr, "\rembedding %d/%d messages...", done, total)
		if done == total {
			fmt.Fprintln(os.Stderr)
		}
	})
}

// transcriptFile is a session transcript to import, with what tells
// whether it changed since it was last imported.
type transcriptFile struct {
	path      string
	configDir string
	size      int64
	mtime     time.Time
}

// listTranscripts returns every session transcript under configDir:
// projects/<dir>/<session_id>.jsonl. Sub agent transcripts, which sit a
// level deeper, are left out: the history is what the person and Claude
// said to each other.
func listTranscripts(configDir string) []transcriptFile {
	paths, _ := filepath.Glob(filepath.Join(configDir, "projects", "*", "*.jsonl"))
	var files []transcriptFile
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		files = append(files, transcriptFile{path: p, configDir: configDir, size: info.Size(), mtime: info.ModTime()})
	}
	return files
}

// importLine is the part of a transcript line extractHistory reads.
type importLine struct {
	Type             string `json:"type"`
	IsMeta           bool   `json:"isMeta"`
	IsSidechain      bool   `json:"isSidechain"`
	IsCompactSummary bool   `json:"isCompactSummary"`
	PromptSource     string `json:"promptSource"`
	Cwd              string `json:"cwd"`
	Timestamp        string `json:"timestamp"`
	Message          struct {
		ID         string          `json:"id"`
		StopReason string          `json:"stop_reason"`
		Content    json.RawMessage `json:"content"`
	} `json:"message"`
}

// extractHistory reads the transcript at path into the messages the hook
// would have recorded had it been running: each prompt the person typed,
// and for each turn the text of Claude's last message that had any (what
// the Stop hook gets as last_assistant_message). A turn still running in a
// live session is left out: its text so far is not its answer, and a
// later import, once it has finished, records the real one.
func extractHistory(path, configDir string) ([]historyMessage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck
	sessionID := strings.TrimSuffix(filepath.Base(path), ".jsonl")

	var msgs []historyMessage
	// the turn's latest assistant message with text, not yet recorded
	var answer historyMessage
	var answerID string
	var answerTexts []string
	// lastStop is the stop_reason of the latest assistant line: "tool_use"
	// at the end of the file means Claude is still at work on the turn
	var lastStop string
	flushAnswer := func() {
		if len(answerTexts) > 0 {
			answer.text = strings.Join(answerTexts, "\n\n")
			msgs = append(msgs, answer)
		}
		answerID, answerTexts = "", nil
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	for scanner.Scan() {
		raw := bytes.TrimSpace(scanner.Bytes())
		if len(raw) == 0 {
			continue
		}
		var l importLine
		if err := json.Unmarshal(raw, &l); err != nil || l.IsSidechain {
			continue
		}
		ts, _ := time.Parse(time.RFC3339, l.Timestamp)
		m := historyMessage{sessionID: sessionID, cwd: l.Cwd, configDir: configDir, createdAt: ts}

		switch l.Type {
		case "user":
			if l.IsMeta || l.IsCompactSummary || l.PromptSource == promptSourceSystem {
				continue
			}
			text, ok := promptText(l.Message.Content)
			if !ok {
				continue // a tool result, not the person
			}
			flushAnswer()
			if text != "" {
				m.role, m.text = "user", text
				msgs = append(msgs, m)
			}
		case "assistant":
			lastStop = l.Message.StopReason
			texts := textBlocks(l.Message.Content)
			if len(texts) == 0 {
				continue
			}
			// a message is spread over one line per content block
			if l.Message.ID == "" || l.Message.ID != answerID {
				answerTexts = nil
			}
			m.role = "assistant"
			answer, answerID = m, l.Message.ID
			answerTexts = append(answerTexts, texts...)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if lastStop != "tool_use" {
		flushAnswer()
	}
	return msgs, nil
}

// textBlocks returns the non-empty text blocks of an assistant message's
// content.
func textBlocks(content json.RawMessage) []string {
	var blocks []rawContentBlock
	if err := json.Unmarshal(content, &blocks); err != nil {
		return nil
	}
	var texts []string
	for _, b := range blocks {
		if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
			texts = append(texts, b.Text)
		}
	}
	return texts
}

var (
	commandNamePattern = regexp.MustCompile(`<command-name>\s*(.*?)\s*</command-name>`)
	commandArgsPattern = regexp.MustCompile(`(?s)<command-args>\s*(.*?)\s*</command-args>`)
)

// promptText returns what the person typed in a user line's content, as
// the UserPromptSubmit hook would see it. ok is false for a line that is
// not a prompt at all (tool results). The text is "" for prompts not worth
// keeping: an interruption marker, or the output of a local command.
func promptText(content json.RawMessage) (text string, ok bool) {
	if !isJSONArray(content) {
		if err := json.Unmarshal(content, &text); err != nil {
			return "", false
		}
	} else {
		var blocks []rawContentBlock
		if err := json.Unmarshal(content, &blocks); err != nil {
			return "", false
		}
		var texts []string
		for _, b := range blocks {
			switch b.Type {
			case "tool_result":
				return "", false
			case "text":
				texts = append(texts, b.Text)
			}
		}
		text = strings.Join(texts, "\n")
	}

	text = strings.TrimSpace(text)
	switch {
	case strings.HasPrefix(text, "[Request interrupted"),
		strings.HasPrefix(text, "<local-command-"),
		strings.HasPrefix(text, "<bash-"):
		return "", true
	case strings.HasPrefix(text, "<command-name>") || strings.HasPrefix(text, "<command-message>"):
		// a slash command, stored as markup: keep it as it was typed
		name := commandNamePattern.FindStringSubmatch(text)
		if name == nil {
			return "", true
		}
		typed := name[1]
		if a := commandArgsPattern.FindStringSubmatch(text); a != nil && a[1] != "" {
			typed += " " + a[1]
		}
		return typed, true
	}
	return text, true
}
