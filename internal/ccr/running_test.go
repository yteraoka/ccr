package ccr

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParsePidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "123.json")
	if err := os.WriteFile(path, []byte(`{"pid":123,"sessionId":"abc-123","cwd":"/tmp"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	rec, err := parsePidFile(path)
	if err != nil {
		t.Fatalf("parsePidFile: %v", err)
	}
	if rec.PID != 123 || rec.SessionID != "abc-123" {
		t.Fatalf("got %+v", rec)
	}
}

func TestParsePidFileInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte(`not json`), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := parsePidFile(path); err == nil {
		t.Fatal("expected error for invalid json")
	}
}

func TestLoadRunningSessionPIDs(t *testing.T) {
	dir := t.TempDir()

	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// valid: pid passes isClaudeProcess
	write("111.json", `{"pid":111,"sessionId":"live-session"}`)
	// stale: pid fails isClaudeProcess (process gone / not claude)
	write("222.json", `{"pid":222,"sessionId":"dead-session"}`)
	// invalid json: skipped
	write("333.json", `not json`)
	// missing sessionId: skipped
	write("444.json", `{"pid":444}`)
	// non-json file: ignored
	write("notes.txt", `hello`)

	origIsClaudeProcess := isClaudeProcess
	isClaudeProcess = func(pid int) bool { return pid == 111 }
	defer func() { isClaudeProcess = origIsClaudeProcess }()

	pids := loadRunningSessionPIDs(dir)

	if got, want := len(pids), 1; got != want {
		t.Fatalf("len(pids) = %d, want %d (%+v)", got, want, pids)
	}
	if pids["live-session"] != 111 {
		t.Fatalf("pids[live-session] = %d, want 111", pids["live-session"])
	}
	if _, ok := pids["dead-session"]; ok {
		t.Fatal("dead-session should not be present")
	}
}

func TestLoadRunningSessionPIDsMissingDir(t *testing.T) {
	pids := loadRunningSessionPIDs(filepath.Join(t.TempDir(), "does-not-exist"))
	if len(pids) != 0 {
		t.Fatalf("expected empty map, got %+v", pids)
	}
}
