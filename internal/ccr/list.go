package ccr

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

type sessionEntry struct {
	id string
	// timestamp is the last "timestamp" found in the session's jsonl file,
	// falling back to the file's mtime when the jsonl has none.
	timestamp time.Time
	cwd       string
	pid       int // 0 if no live claude process is running this session
	// tokens is the session's cumulative token usage: input + output +
	// cache tokens summed across every assistant message.
	tokens int
}

// collectSessions walks ${CLAUDE_CONFIG_DIR}/projects/<dir>/<session_id>.jsonl
// and returns one entry per session file found across every project directory.
func collectSessions(root string) ([]sessionEntry, error) {
	projectDirs, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}

	var entries []sessionEntry
	for _, pd := range projectDirs {
		if !pd.IsDir() {
			continue
		}
		sub, err := collectSessionsInDir(filepath.Join(root, pd.Name()))
		if err != nil {
			continue
		}
		entries = append(entries, sub...)
	}
	return entries, nil
}

// attachRunningPIDs sets pid on every entry whose id has a matching live
// claude process in pids (sessionID -> pid, as returned by
// loadRunningSessionPIDs).
func attachRunningPIDs(entries []sessionEntry, pids map[string]int) []sessionEntry {
	for i := range entries {
		entries[i].pid = pids[entries[i].id]
	}
	return entries
}

// collectSessionsInDir returns one entry per <session_id>.jsonl file found
// directly inside dir.
func collectSessionsInDir(dir string) ([]sessionEntry, error) {
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var entries []sessionEntry
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
			continue
		}
		info, err := f.Info()
		if err != nil {
			continue
		}
		cwd, usage, lastTimestamp, _ := readSessionSummary(filepath.Join(dir, f.Name()))
		timestamp := lastTimestamp
		if timestamp.IsZero() {
			timestamp = info.ModTime()
		}
		entries = append(entries, sessionEntry{
			id:        strings.TrimSuffix(f.Name(), ".jsonl"),
			timestamp: timestamp,
			cwd:       cwd,
			tokens:    usage.total(),
		})
	}
	return entries, nil
}
