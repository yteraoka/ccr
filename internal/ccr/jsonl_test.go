package ccr

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadSessionCwdAndLastTimestamp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	content := `{"type":"mode","mode":"normal"}
{"type":"user","cwd":"/tmp/proj","timestamp":"2026-07-24T13:32:51.034Z"}
{"type":"assistant","timestamp":"2026-07-24T13:54:53.368Z"}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cwd, last, err := readSessionCwdAndLastTimestamp(path)
	if err != nil {
		t.Fatalf("readSessionCwdAndLastTimestamp: %v", err)
	}
	if cwd != "/tmp/proj" {
		t.Errorf("cwd = %q, want /tmp/proj", cwd)
	}
	want := time.Date(2026, 7, 24, 13, 54, 53, 368*int(time.Millisecond), time.UTC)
	if !last.Equal(want) {
		t.Errorf("last = %v, want %v", last, want)
	}
}

func TestReadSessionCwdAndLastTimestampNoTimestamp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"user","cwd":"/tmp/proj"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cwd, last, err := readSessionCwdAndLastTimestamp(path)
	if err != nil {
		t.Fatalf("readSessionCwdAndLastTimestamp: %v", err)
	}
	if cwd != "/tmp/proj" {
		t.Errorf("cwd = %q, want /tmp/proj", cwd)
	}
	if !last.IsZero() {
		t.Errorf("last = %v, want zero value", last)
	}
}
