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

	_, _, _, start, end, err := parseSessionInfo(path)
	if err != nil {
		t.Fatalf("parseSessionInfo: %v", err)
	}

	wantStart := time.Date(2026, 7, 24, 13, 32, 51, 34*int(time.Millisecond), time.UTC)
	wantEnd := time.Date(2026, 7, 24, 13, 54, 53, 368*int(time.Millisecond), time.UTC)
	if !start.Equal(wantStart) {
		t.Errorf("start = %v, want %v", start, wantStart)
	}
	if !end.Equal(wantEnd) {
		t.Errorf("end = %v, want %v", end, wantEnd)
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

	_, _, _, start, end, err := parseSessionInfo(path)
	if err != nil {
		t.Fatalf("parseSessionInfo: %v", err)
	}

	if !start.IsZero() {
		t.Errorf("start = %v, want zero value", start)
	}
	if !end.IsZero() {
		t.Errorf("end = %v, want zero value", end)
	}
}
