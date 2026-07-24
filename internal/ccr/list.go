package ccr

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

type sessionEntry struct {
	id      string
	modTime time.Time
	cwd     string
	pid     int // 0 if no live claude process is running this session
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
		cwd, _ := readSessionCwd(filepath.Join(dir, f.Name()))
		entries = append(entries, sessionEntry{
			id:      strings.TrimSuffix(f.Name(), ".jsonl"),
			modTime: info.ModTime(),
			cwd:     cwd,
		})
	}
	return entries, nil
}
