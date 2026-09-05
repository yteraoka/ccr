package ccr

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
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

// sessionInfo is what one scan of a session file yields. It is a struct
// rather than a row of return values because the scan keeps growing what
// it can report.
type sessionInfo struct {
	cwd     string
	aiTitle string
	prompts []string
	// startTime and endTime come from the first and last lines with a
	// usable timestamp; zero when the file has none.
	startTime time.Time
	endTime   time.Time
	usage     tokenUsage
	// cost is the last "cost-state" line, which supersedes the ones before
	// it. It is nil when the file records none.
	cost *costState
}

// parseSessionInfo scans a session JSONL file and extracts:
//   - cwd: from the first line containing a non-empty "cwd" field
//   - aiTitle: the value of "aiTitle" from the last line where
//     type == "ai-title"
//   - prompts: lastPrompt from up to the last 3 lines where
//     type == "last-prompt", collapsing consecutive repeats of the same
//     value like uniq(1)
//   - startTime/endTime: parsed from the first and last lines (in file
//     order) with a valid "timestamp" field; zero value if none found
//   - usage: cumulative token usage, split by kind, summed across every
//     assistant message and de-duplicated by message id
//   - cost: the last "cost-state" line, if the file has any
func parseSessionInfo(path string) (sessionInfo, error) {
	var info sessionInfo

	f, err := os.Open(path)
	if err != nil {
		return sessionInfo{}, err
	}
	defer f.Close() //nolint:errcheck

	seen := make(map[string]bool)
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

		if info.cwd == "" && rec.Cwd != "" {
			info.cwd = rec.Cwd
		}
		if rec.Type == "ai-title" && rec.AiTitle != "" {
			info.aiTitle = rec.AiTitle
		}
		if rec.Type == "last-prompt" && rec.LastPrompt != "" {
			if len(info.prompts) == 0 || info.prompts[len(info.prompts)-1] != rec.LastPrompt {
				info.prompts = append(info.prompts, rec.LastPrompt)
			}
		}
		if rec.Type == "cost-state" {
			// decoded separately: only these lines carry it, and it is far
			// more than the rest of the scan needs from a line
			var cost costState
			if err := json.Unmarshal(line, &cost); err == nil {
				info.cost = &cost
			}
		}
		addTokens(seen, rec, &info.usage)
		if rec.Timestamp != "" {
			if t, err := time.Parse(time.RFC3339, rec.Timestamp); err == nil {
				if info.startTime.IsZero() {
					info.startTime = t
				}
				info.endTime = t
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return sessionInfo{}, err
	}

	if len(info.prompts) > 3 {
		info.prompts = info.prompts[len(info.prompts)-3:]
	}

	return info, nil
}
