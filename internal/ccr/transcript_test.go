package ccr

import (
	"os"
	"path/filepath"
	"testing"
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

	// entry 3: assistant text-only (empty thinking skipped)
	e3 := entries[3]
	if e3.role != "assistant" || len(e3.blocks) != 1 || e3.blocks[0].kind != "text" || e3.blocks[0].text != "hello" {
		t.Fatalf("entry 3 = %+v, want single text block \"hello\"", e3)
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
