package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
)

// Record represents a single line of a Claude Code session JSONL file.
type Record struct {
	Type       string `json:"type"`
	Cwd        string `json:"cwd,omitempty"`
	LastPrompt string `json:"lastPrompt,omitempty"`
	AiTitle    string `json:"aiTitle,omitempty"`
}

// configDir returns CLAUDE_CONFIG_DIR if set, otherwise ${HOME}/.claude.
func configDir() string {
	if v := os.Getenv("CLAUDE_CONFIG_DIR"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".claude"
	}
	return filepath.Join(home, ".claude")
}

// projectsDir returns ${CLAUDE_CONFIG_DIR}/projects.
func projectsDir() string {
	return filepath.Join(configDir(), "projects")
}

var nonAlnumPattern = regexp.MustCompile(`[^a-zA-Z0-9]`)

// encodeProjectDir converts a working directory path into the directory
// name Claude Code uses for it under ${CLAUDE_CONFIG_DIR}/projects: every
// character outside a-zA-Z0-9 becomes '-'.
func encodeProjectDir(path string) string {
	return nonAlnumPattern.ReplaceAllString(path, "-")
}

// readSessionCwd returns the cwd recorded in a session file, taken from the
// first line that has a non-empty "cwd" field.
func readSessionCwd(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var rec Record
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		if rec.Cwd != "" {
			return rec.Cwd, nil
		}
	}
	return "", scanner.Err()
}
