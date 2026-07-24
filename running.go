package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// sessionsDir returns ${CLAUDE_CONFIG_DIR}/sessions, where Claude Code
// records a {pid}.json snapshot for every running (or not cleanly
// terminated) claude process.
func sessionsDir() string {
	return filepath.Join(configDir(), "sessions")
}

// sessionPidRecord is the subset of a ${CLAUDE_CONFIG_DIR}/sessions/{pid}.json
// snapshot that ccr needs.
type sessionPidRecord struct {
	PID       int    `json:"pid"`
	SessionID string `json:"sessionId"`
}

// parsePidFile decodes a single {pid}.json snapshot file.
func parsePidFile(path string) (sessionPidRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return sessionPidRecord{}, err
	}
	var rec sessionPidRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return sessionPidRecord{}, err
	}
	return rec, nil
}

// isClaudeProcess reports whether pid is a currently running claude
// process. It is a package variable so tests can stub it out.
var isClaudeProcess = func(pid int) bool {
	if pid <= 0 {
		return false
	}
	comm, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "comm"))
	if err == nil {
		return strings.TrimSpace(string(comm)) == "claude"
	}
	if os.IsNotExist(err) {
		return false
	}

	// /proc is unavailable on this platform (e.g. macOS): fall back to a
	// liveness-only check, since we can't inspect the command name.
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// loadRunningSessionPIDs reads every {pid}.json snapshot in dir and returns
// a map of sessionID -> pid for the ones whose pid is a live claude
// process. Stale or invalid snapshots are silently skipped; the files
// themselves are left untouched on disk.
func loadRunningSessionPIDs(dir string) map[string]int {
	pids := make(map[string]int)

	files, err := os.ReadDir(dir)
	if err != nil {
		return pids
	}
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		rec, err := parsePidFile(filepath.Join(dir, f.Name()))
		if err != nil || rec.SessionID == "" {
			continue
		}
		if !isClaudeProcess(rec.PID) {
			continue
		}
		pids[rec.SessionID] = rec.PID
	}
	return pids
}
