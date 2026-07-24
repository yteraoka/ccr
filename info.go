package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// findSessionFile locates ${CLAUDE_CONFIG_DIR}/projects/<dir>/<session_id>.jsonl
// by searching every project directory for a matching file name.
func findSessionFile(root, sessionID string) (string, error) {
	target := sessionID + ".jsonl"

	projectDirs, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	for _, pd := range projectDirs {
		if !pd.IsDir() {
			continue
		}
		candidate := filepath.Join(root, pd.Name(), target)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("session not found: %s", sessionID)
}

// parseSessionInfo scans a session JSONL file and extracts:
//   - cwd: from the first line containing a non-empty "cwd" field
//   - aiTitle: the value of "aiTitle" from the last line where
//     type == "ai-title"
//   - prompts: lastPrompt from up to the last 3 lines where
//     type == "last-prompt", collapsing consecutive repeats of the same
//     value like uniq(1)
func parseSessionInfo(path string) (cwd string, aiTitle string, prompts []string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", nil, err
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

		if cwd == "" && rec.Cwd != "" {
			cwd = rec.Cwd
		}
		if rec.Type == "ai-title" && rec.AiTitle != "" {
			aiTitle = rec.AiTitle
		}
		if rec.Type == "last-prompt" && rec.LastPrompt != "" {
			if len(prompts) == 0 || prompts[len(prompts)-1] != rec.LastPrompt {
				prompts = append(prompts, rec.LastPrompt)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", "", nil, err
	}

	if len(prompts) > 3 {
		prompts = prompts[len(prompts)-3:]
	}

	return cwd, aiTitle, prompts, nil
}
