package ccr

import (
	"errors"
	"fmt"
	"os"
	"sort"
)

// errNoHistory says the history has nothing recorded for a session.
var errNoHistory = errors.New("no history recorded for this session")

// buildHistorySessionHTML renders the transcript page of a session whose
// jsonl is gone, from what the history database kept of it: each prompt
// and Claude's final answer to it, in the order they were said. Tool calls
// and everything else the transcript held are lost with it, which the page
// says at the top. errNoHistory means there is nothing to show.
func buildHistorySessionHTML(sessionID string) (string, error) {
	msgs, err := recordedSessionMessages(sessionID)
	if err != nil {
		return "", err
	}

	entries := make([]transcriptEntry, len(msgs))
	for i, m := range msgs {
		entries[i] = transcriptEntry{
			role:      m.role,
			timestamp: m.createdAt,
			blocks:    []transcriptBlock{{kind: "text", text: m.text}},
		}
	}
	meta := sessionHeaderInfo{
		sessionID: sessionID,
		cwd:       msgs[0].cwd,
		startTime: msgs[0].createdAt,
		endTime:   msgs[len(msgs)-1].createdAt,
		notice: fmt.Sprintf("This session's transcript file no longer exists. "+
			"The page is rebuilt from the %d messages ccr's history recorded: "+
			"the prompts and Claude's final answer to each, without tool calls "+
			"or the messages in between.", len(msgs)),
	}
	return renderTranscriptHTML(meta, entries), nil
}

// recordedSessionMessages returns what the history database holds of
// sessionID, oldest first, or errNoHistory. It never creates the database:
// a page asked for by a missing session is no reason to start a history.
func recordedSessionMessages(sessionID string) ([]historyMessage, error) {
	cfg, err := loadFileConfig(ccrConfigPath())
	if err != nil {
		return nil, err
	}
	path, err := historyDBPath(cfg.History)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); err != nil {
		return nil, errNoHistory
	}
	h, err := openHistoryDB(path)
	if err != nil {
		return nil, err
	}
	defer h.Close() //nolint:errcheck

	msgs, err := h.queryMessages(`SELECT id, session_id, role, text, cwd, config_dir, created_at FROM messages WHERE session_id = ?`, sessionID)
	if err != nil {
		return nil, err
	}
	if len(msgs) == 0 {
		return nil, errNoHistory
	}
	// by time, not id: an import can add a session's earlier messages after
	// the hook recorded its later ones
	sort.SliceStable(msgs, func(i, j int) bool {
		if !msgs[i].createdAt.Equal(msgs[j].createdAt) {
			return msgs[i].createdAt.Before(msgs[j].createdAt)
		}
		return msgs[i].id < msgs[j].id
	})
	return msgs, nil
}

// transcriptExists reports whether sessionID's jsonl can still be found,
// looking in configDir as well (where the history saw the session).
func transcriptExists(sessionID, configDir string) bool {
	rememberConfigDir(configDir)
	_, err := lookupSessionFile(sessionID)
	return err == nil
}
