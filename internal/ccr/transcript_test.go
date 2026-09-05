package ccr

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseTranscript(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	content := `{"type":"mode","mode":"normal"}
{"type":"assistant","timestamp":"2026-07-24T10:00:00Z","message":{"content":[{"type":"tool_use","id":"tu1","name":"Bash","input":{"command":"echo hi"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu1","content":"hi\n"}]},"toolUseResult":{"stdout":"hi\n","stderr":""}}
{"type":"user","isMeta":true,"timestamp":"2026-07-24T10:00:01Z","message":{"content":"<system-reminder>note</system-reminder>"}}
{"type":"user","timestamp":"2026-07-24T10:00:02Z","message":{"content":"real prompt"}}
{"type":"assistant","timestamp":"2026-07-24T10:00:03Z","message":{"content":[{"type":"thinking","thinking":""},{"type":"text","text":"hello"}]}}
{"type":"assistant","timestamp":"2026-07-24T10:00:04Z","message":{"content":[{"type":"thinking","thinking":""}]}}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := parseTranscript(path)
	if err != nil {
		t.Fatalf("parseTranscript: %v", err)
	}

	if len(entries) != 4 {
		t.Fatalf("got %d entries, want 4: %+v", len(entries), entries)
	}

	// entry 0: assistant tool_use with correlated result
	e0 := entries[0]
	if e0.role != "assistant" || len(e0.blocks) != 1 || e0.blocks[0].kind != "tool_use" {
		t.Fatalf("entry 0 = %+v, want assistant tool_use", e0)
	}
	if e0.blocks[0].outcome == nil {
		t.Fatal("entry 0 tool_use has no correlated outcome")
	}
	if e0.blocks[0].outcome.isError {
		t.Error("entry 0 outcome should not be an error")
	}
	if string(e0.blocks[0].outcome.toolUseResult) == "" {
		t.Error("entry 0 outcome missing toolUseResult")
	}

	// entry 1: isMeta system note
	e1 := entries[1]
	if e1.role != "user" || !e1.isMeta {
		t.Fatalf("entry 1 = %+v, want isMeta user", e1)
	}

	// entry 2: real human prompt
	e2 := entries[2]
	if e2.role != "user" || e2.isMeta || e2.blocks[0].text != "real prompt" {
		t.Fatalf("entry 2 = %+v, want real prompt", e2)
	}
	if e2.isNotification {
		t.Errorf("entry 2 = %+v, a typed prompt is not a notification", e2)
	}

	// entry 3: assistant text-only (empty thinking skipped)
	e3 := entries[3]
	if e3.role != "assistant" || len(e3.blocks) != 1 || e3.blocks[0].kind != "text" || e3.blocks[0].text != "hello" {
		t.Fatalf("entry 3 = %+v, want single text block \"hello\"", e3)
	}
}

// A sub agent's report reaches the main session as an ordinary user line
// whose content is a plain string, distinguished only by promptSource, so
// it must not be attributed to the human.
func TestParseTranscriptMarksSystemPromptsAsNotifications(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	content := `{"type":"user","promptSource":"typed","timestamp":"2026-07-24T10:00:00Z","message":{"content":"review the diff"}}
{"type":"user","promptSource":"system","timestamp":"2026-07-24T10:05:00Z","message":{"content":"<task-notification><summary>Agent \"/code-review\" finished</summary></task-notification>"}}
{"type":"user","promptSource":"suggestion_accepted","timestamp":"2026-07-24T10:06:00Z","message":{"content":"apply the fixes"}}
{"type":"user","promptSource":"queued","timestamp":"2026-07-24T10:07:00Z","message":{"content":"then commit"}}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := parseTranscript(path)
	if err != nil {
		t.Fatalf("parseTranscript: %v", err)
	}
	if len(entries) != 4 {
		t.Fatalf("got %d entries, want 4: %+v", len(entries), entries)
	}

	if entries[0].isNotification {
		t.Errorf(`promptSource "typed" marked as a notification: %+v`, entries[0])
	}
	if !entries[1].isNotification {
		t.Errorf(`promptSource "system" not marked as a notification: %+v`, entries[1])
	}
	// the other human sources must keep their Human attribution
	if entries[2].isNotification {
		t.Errorf(`promptSource "suggestion_accepted" marked as a notification: %+v`, entries[2])
	}
	if entries[3].isNotification {
		t.Errorf(`promptSource "queued" marked as a notification: %+v`, entries[3])
	}
}

func TestReadJSONLLinesFileNotFound(t *testing.T) {
	if _, err := readJSONLLines(filepath.Join(t.TempDir(), "missing.jsonl")); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}

func TestReadJSONLLinesSkipsBlankAndInvalidLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	content := "\n" + `{"type":"user"}` + "\n" + "not json\n" + "   \n" + `{"type":"assistant"}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	lines, err := readJSONLLines(path)
	if err != nil {
		t.Fatalf("readJSONLLines: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2 (blank/invalid skipped): %+v", len(lines), lines)
	}
	if lines[0].Type != "user" || lines[1].Type != "assistant" {
		t.Errorf("got %+v, want user then assistant", lines)
	}
}

func TestParseAssistantLineInvalidMessage(t *testing.T) {
	raw := rawLine{Type: "assistant", Message: []byte(`not json`)}
	_, ok := parseAssistantLine(raw, time.Time{}, nil, map[string]bool{})
	if ok {
		t.Error("expected parseAssistantLine to fail on invalid message JSON")
	}
}

func TestParseAssistantLineInvalidContent(t *testing.T) {
	raw := rawLine{Type: "assistant", Message: []byte(`{"content":"not an array"}`)}
	_, ok := parseAssistantLine(raw, time.Time{}, nil, map[string]bool{})
	if ok {
		t.Error("expected parseAssistantLine to fail when content isn't a block array")
	}
}

func TestParseUserLineInvalidMessage(t *testing.T) {
	raw := rawLine{Type: "user", Message: []byte(`not json`)}
	_, ok := parseUserLine(raw, time.Time{})
	if ok {
		t.Error("expected parseUserLine to fail on invalid message JSON")
	}
}

func TestParseUserLineEmptyText(t *testing.T) {
	raw := rawLine{Type: "user", Message: []byte(`""`)}
	_, ok := parseUserLine(raw, time.Time{})
	if ok {
		t.Error("expected parseUserLine to skip empty-text user lines")
	}
}

func TestParseTranscriptIsErrorOutcome(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	content := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu1","name":"Bash","input":{"command":"false"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu1","content":"boom","is_error":true}]}}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := parseTranscript(path)
	if err != nil {
		t.Fatalf("parseTranscript: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	outcome := entries[0].blocks[0].outcome
	if outcome == nil || !outcome.isError {
		t.Fatalf("outcome = %+v, want isError true", outcome)
	}
}

// One assistant message is written to the jsonl once per content block,
// each line repeating the same usage, so only the first line of a message
// may report its tokens.
func TestParseTranscriptAttributesUsageOncePerMessage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	usage := `"usage":{"input_tokens":2,"output_tokens":144,"cache_creation_input_tokens":29552,"cache_read_input_tokens":0}`
	content := `{"type":"assistant","message":{"id":"msg_1",` + usage + `,"content":[{"type":"thinking","thinking":"hmm"}]}}
{"type":"assistant","message":{"id":"msg_1",` + usage + `,"content":[{"type":"text","text":"hello"}]}}
{"type":"assistant","message":{"id":"msg_2","usage":{"input_tokens":1,"output_tokens":2,"cache_creation_input_tokens":0,"cache_read_input_tokens":3},"content":[{"type":"text","text":"bye"}]}}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := parseTranscript(path)
	if err != nil {
		t.Fatalf("parseTranscript: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3: %+v", len(entries), entries)
	}

	if !entries[0].hasUsage || entries[0].usage.total() != 29698 {
		t.Errorf("entry 0 = %+v, want the message's usage", entries[0])
	}
	if entries[1].hasUsage {
		t.Errorf("entry 1 = %+v, want no usage on the repeat of msg_1", entries[1])
	}
	if !entries[2].hasUsage || entries[2].usage.total() != 6 {
		t.Errorf("entry 2 = %+v, want msg_2's own usage", entries[2])
	}
}

// A message whose first line renders nothing (an empty thinking block) is
// dropped, so its usage has to survive to the next line of that message
// rather than being claimed by the dropped one.
func TestParseTranscriptKeepsUsageWhenFirstLineIsDropped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	usage := `"usage":{"input_tokens":2,"output_tokens":144,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}`
	content := `{"type":"assistant","message":{"id":"msg_1",` + usage + `,"content":[{"type":"thinking","thinking":""}]}}
{"type":"assistant","message":{"id":"msg_1",` + usage + `,"content":[{"type":"text","text":"hello"}]}}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := parseTranscript(path)
	if err != nil {
		t.Fatalf("parseTranscript: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1 (the empty thinking line is dropped): %+v", len(entries), entries)
	}
	if !entries[0].hasUsage || entries[0].usage.total() != 146 {
		t.Errorf("entry = %+v, want the message's usage carried over to the rendered line", entries[0])
	}
}

// The page carries only where each event's jsonl line is, so those spans
// have to land exactly on the line in the file.
func TestReadJSONLLinesRecordsWhereEachLineIs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	first := `{"type":"user","message":{"content":"最初"}}`
	second := `{"type":"assistant","message":{"content":[{"type":"text","text":"hello"}]}}`
	// blank lines, indentation and \r\n all have to be accounted for
	content := first + "\r\n\n   " + second + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	lines, err := readJSONLLines(path)
	if err != nil {
		t.Fatalf("readJSONLLines: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{first, second} {
		span := lines[i].span
		if !span.ok() {
			t.Errorf("line %d has no span", i)
			continue
		}
		got := string(raw[span.offset : span.offset+int64(span.length)])
		if got != want {
			t.Errorf("line %d span points at %q, want %q", i, got, want)
		}
	}
}

func TestParseTranscriptCarriesSpansOntoEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	content := `{"type":"user","message":{"content":"prompt"}}
{"type":"assistant","message":{"content":[{"type":"text","text":"reply"}]}}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := parseTranscript(path)
	if err != nil {
		t.Fatalf("parseTranscript: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for i, e := range entries {
		if !e.span.ok() {
			t.Fatalf("entry %d has no span: %+v", i, e)
		}
		line := string(raw[e.span.offset : e.span.offset+int64(e.span.length)])
		if !json.Valid([]byte(line)) {
			t.Errorf("entry %d span does not cover valid JSON: %q", i, line)
		}
	}
	if entries[0].span.offset != 0 {
		t.Errorf("first entry starts at %d, want 0", entries[0].span.offset)
	}
	if entries[1].span.offset <= entries[0].span.offset {
		t.Error("the second entry should start after the first")
	}
}
