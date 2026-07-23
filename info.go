package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func runInfo(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: cc-resume info <session_id>")
	}
	sessionID := args[0]

	path, err := findSessionFile(projectsDir(), sessionID)
	if err != nil {
		return err
	}

	cwd, timestamp, contents, err := parseSessionInfo(path)
	if err != nil {
		return err
	}

	fmt.Println(cwd)
	fmt.Println(timestamp)
	for _, c := range contents {
		fmt.Println()
		fmt.Println(c)
	}
	return nil
}

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
//   - timestamp: the value of "timestamp" from the last line that has one
//   - contents: message.content of up to the last 3 lines where
//     type == "user" and origin.kind == "human"
func parseSessionInfo(path string) (cwd string, timestamp string, contents []string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", nil, err
	}
	defer f.Close()

	var humanContents []string

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
		if rec.Timestamp != "" {
			timestamp = rec.Timestamp
		}
		if rec.IsHumanUser() {
			if c, ok := rec.ContentString(); ok {
				humanContents = append(humanContents, c)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", "", nil, err
	}

	if len(humanContents) > 3 {
		humanContents = humanContents[len(humanContents)-3:]
	}

	return cwd, timestamp, humanContents, nil
}
