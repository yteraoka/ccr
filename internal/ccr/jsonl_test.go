package ccr

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadSessionSummary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	content := `{"type":"mode","mode":"normal"}
{"type":"user","cwd":"/tmp/proj","timestamp":"2026-07-24T13:32:51.034Z"}
{"type":"assistant","timestamp":"2026-07-24T13:40:00Z","message":{"id":"msg_1","usage":{"input_tokens":10,"output_tokens":20,"cache_creation_input_tokens":5,"cache_read_input_tokens":3}}}
{"type":"assistant","timestamp":"2026-07-24T13:40:00Z","message":{"id":"msg_1","usage":{"input_tokens":10,"output_tokens":20,"cache_creation_input_tokens":5,"cache_read_input_tokens":3}}}
{"type":"assistant","timestamp":"2026-07-24T13:54:53.368Z","message":{"id":"msg_2","usage":{"input_tokens":100,"output_tokens":200,"cache_creation_input_tokens":50,"cache_read_input_tokens":30}}}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cwd, usage, last, err := readSessionSummary(path)
	if err != nil {
		t.Fatalf("readSessionSummary: %v", err)
	}
	if cwd != "/tmp/proj" {
		t.Errorf("cwd = %q, want /tmp/proj", cwd)
	}
	// msg_1 is counted once despite appearing twice, msg_2 adds to it.
	wantUsage := tokenUsage{input: 110, output: 220, cacheCreation: 55, cacheRead: 33}
	if usage != wantUsage {
		t.Errorf("usage = %+v, want %+v", usage, wantUsage)
	}
	if usage.total() != 418 {
		t.Errorf("usage.total() = %d, want 418", usage.total())
	}
	want := time.Date(2026, 7, 24, 13, 54, 53, 368*int(time.Millisecond), time.UTC)
	if !last.Equal(want) {
		t.Errorf("last = %v, want %v", last, want)
	}
}

func TestConfigDirUsesEnvVarWhenSet(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/custom/config")
	if got := configDir(); got != "/custom/config" {
		t.Errorf("configDir() = %q, want /custom/config", got)
	}
}

func TestConfigDirFallsBackToHomeClaudeDir(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir available: %v", err)
	}
	want := filepath.Join(home, ".claude")
	if got := configDir(); got != want {
		t.Errorf("configDir() = %q, want %q", got, want)
	}
}

func TestReadSessionSummaryNoTimestamp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"user","cwd":"/tmp/proj"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cwd, usage, last, err := readSessionSummary(path)
	if err != nil {
		t.Fatalf("readSessionSummary: %v", err)
	}
	if cwd != "/tmp/proj" {
		t.Errorf("cwd = %q, want /tmp/proj", cwd)
	}
	if usage != (tokenUsage{}) {
		t.Errorf("usage = %+v, want zero value", usage)
	}
	if !last.IsZero() {
		t.Errorf("last = %v, want zero value", last)
	}
}
