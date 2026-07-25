package ccr

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAttachRunningPIDs(t *testing.T) {
	entries := []sessionEntry{
		{id: "session-a"},
		{id: "session-b"},
		{id: "session-c"},
	}
	pids := map[string]int{
		"session-a": 111,
		"session-c": 333,
	}

	got := attachRunningPIDs(entries, pids)

	want := map[string]int{"session-a": 111, "session-b": 0, "session-c": 333}
	for _, e := range got {
		if e.pid != want[e.id] {
			t.Errorf("entry %s: pid = %d, want %d", e.id, e.pid, want[e.id])
		}
	}
}

func TestCollectSessionsMissingRoot(t *testing.T) {
	if _, err := collectSessions(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Fatal("expected an error when the root directory doesn't exist")
	}
}

func TestCollectSessionsSkipsUnreadableProjectDir(t *testing.T) {
	root := t.TempDir()
	// A file (not a directory) alongside a real project dir: collectSessions
	// should skip it via the IsDir() check rather than erroring.
	if err := os.WriteFile(filepath.Join(root, "not-a-dir"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	projDir := filepath.Join(root, "project")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projDir, "session.jsonl"), []byte(`{"type":"user","cwd":"/tmp"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := collectSessions(root)
	if err != nil {
		t.Fatalf("collectSessions: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
}

func TestCollectSessionsInDirUsesLastTimestamp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "with-ts.jsonl")
	content := `{"type":"user","cwd":"/tmp/proj","timestamp":"2020-01-01T00:00:00Z"}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	// mtime is set far from the jsonl timestamp so the test can tell which
	// one collectSessionsInDir actually picked.
	future := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}

	entries, err := collectSessionsInDir(dir)
	if err != nil {
		t.Fatalf("collectSessionsInDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	want := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if !entries[0].timestamp.Equal(want) {
		t.Errorf("timestamp = %v, want %v (jsonl timestamp, not mtime)", entries[0].timestamp, want)
	}
}

func TestCollectSessionsInDirFallsBackToMtime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "no-ts.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"user","cwd":"/tmp/proj"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mtime := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}

	entries, err := collectSessionsInDir(dir)
	if err != nil {
		t.Fatalf("collectSessionsInDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if !entries[0].timestamp.Equal(mtime) {
		t.Errorf("timestamp = %v, want mtime %v", entries[0].timestamp, mtime)
	}
}
