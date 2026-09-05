package ccr

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseSessionInfoTimestamps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	content := `{"type":"mode","mode":"normal"}
{"type":"user","cwd":"/tmp/proj","timestamp":"2026-07-24T13:32:51.034Z"}
{"type":"assistant","timestamp":"2026-07-24T13:40:00Z"}
{"type":"user","timestamp":"2026-07-24T13:54:53.368Z"}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := parseSessionInfo(path)
	if err != nil {
		t.Fatalf("parseSessionInfo: %v", err)
	}
	start, end := got.startTime, got.endTime

	wantStart := time.Date(2026, 7, 24, 13, 32, 51, 34*int(time.Millisecond), time.UTC)
	wantEnd := time.Date(2026, 7, 24, 13, 54, 53, 368*int(time.Millisecond), time.UTC)
	if !start.Equal(wantStart) {
		t.Errorf("start = %v, want %v", start, wantStart)
	}
	if !end.Equal(wantEnd) {
		t.Errorf("end = %v, want %v", end, wantEnd)
	}
}

func TestParseSessionInfoAiTitleAndPrompts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	content := `{"type":"user","cwd":"/tmp/proj"}
{"type":"ai-title","aiTitle":"First Title"}
{"type":"ai-title","aiTitle":"Latest Title"}
{"type":"last-prompt","lastPrompt":"one"}
{"type":"last-prompt","lastPrompt":"one"}
{"type":"last-prompt","lastPrompt":"two"}
{"type":"last-prompt","lastPrompt":"three"}
{"type":"last-prompt","lastPrompt":"four"}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := parseSessionInfo(path)
	if err != nil {
		t.Fatalf("parseSessionInfo: %v", err)
	}
	cwd, aiTitle, prompts := got.cwd, got.aiTitle, got.prompts
	if cwd != "/tmp/proj" {
		t.Errorf("cwd = %q, want /tmp/proj", cwd)
	}
	if aiTitle != "Latest Title" {
		t.Errorf("aiTitle = %q, want the last ai-title line's value", aiTitle)
	}
	want := []string{"two", "three", "four"}
	if len(prompts) != len(want) {
		t.Fatalf("prompts = %v, want %v", prompts, want)
	}
	for i := range want {
		if prompts[i] != want[i] {
			t.Errorf("prompts[%d] = %q, want %q", i, prompts[i], want[i])
		}
	}
}

func TestParseSessionInfoTokenUsage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	content := `{"type":"user","cwd":"/tmp/proj"}
{"type":"assistant","message":{"id":"msg_1","usage":{"input_tokens":1,"output_tokens":2,"cache_creation_input_tokens":3,"cache_read_input_tokens":4}}}
{"type":"assistant","message":{"id":"msg_1","usage":{"input_tokens":1,"output_tokens":2,"cache_creation_input_tokens":3,"cache_read_input_tokens":4}}}
{"type":"assistant","message":{"id":"msg_2","usage":{"input_tokens":10,"output_tokens":20,"cache_creation_input_tokens":30,"cache_read_input_tokens":40}}}
{"type":"user","message":{"id":"msg_3","usage":{"input_tokens":999,"output_tokens":999}}}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	parsed, err := parseSessionInfo(path)
	if err != nil {
		t.Fatalf("parseSessionInfo: %v", err)
	}
	usage := parsed.usage

	// msg_1 counted once, msg_2 added, and the non-assistant line ignored.
	want := tokenUsage{input: 11, output: 22, cacheCreation: 33, cacheRead: 44}
	if usage != want {
		t.Errorf("usage = %+v, want %+v", usage, want)
	}
	if usage.total() != 110 {
		t.Errorf("usage.total() = %d, want 110", usage.total())
	}
}

func TestFindSessionFileFound(t *testing.T) {
	root := t.TempDir()
	projDir := filepath.Join(root, "some-project")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(projDir, "session-a.jsonl")
	if err := os.WriteFile(target, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := findSessionFile(root, "session-a")
	if err != nil {
		t.Fatalf("findSessionFile: %v", err)
	}
	if got != target {
		t.Errorf("findSessionFile = %q, want %q", got, target)
	}
}

func TestFindSessionFileNotFound(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "some-project"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := findSessionFile(root, "does-not-exist"); err == nil {
		t.Fatal("expected an error for a missing session file")
	}
}

func TestFindSessionFileMissingRoot(t *testing.T) {
	if _, err := findSessionFile(filepath.Join(t.TempDir(), "does-not-exist"), "session-a"); err == nil {
		t.Fatal("expected an error when the root directory doesn't exist")
	}
}

func TestParseSessionInfoNoTimestamps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	content := `{"type":"mode","mode":"normal"}
{"type":"user","cwd":"/tmp/proj"}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := parseSessionInfo(path)
	if err != nil {
		t.Fatalf("parseSessionInfo: %v", err)
	}
	start, end := got.startTime, got.endTime

	if !start.IsZero() {
		t.Errorf("start = %v, want zero value", start)
	}
	if !end.IsZero() {
		t.Errorf("end = %v, want zero value", end)
	}
}
