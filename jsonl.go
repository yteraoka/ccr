package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Origin represents the "origin" field of a session JSONL record.
type Origin struct {
	Kind string `json:"kind"`
}

// Message represents the "message" field of a session JSONL record.
type Message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// Record represents a single line of a Claude Code session JSONL file.
type Record struct {
	Type      string   `json:"type"`
	Message   *Message `json:"message,omitempty"`
	Cwd       string   `json:"cwd,omitempty"`
	Timestamp string   `json:"timestamp,omitempty"`
	Origin    *Origin  `json:"origin,omitempty"`
}

// IsHumanUser reports whether this record is a user message typed by a human.
func (r *Record) IsHumanUser() bool {
	return r.Type == "user" && r.Origin != nil && r.Origin.Kind == "human"
}

// ContentString returns message.content as a string, when it is one.
func (r *Record) ContentString() (string, bool) {
	if r.Message == nil || len(r.Message.Content) == 0 {
		return "", false
	}
	var s string
	if err := json.Unmarshal(r.Message.Content, &s); err != nil {
		return "", false
	}
	return s, true
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
