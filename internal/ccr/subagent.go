package ccr

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Claude Code records each sub agent a session spawns in its own transcript
// beside the session's jsonl:
//
//	<project>/<session_id>.jsonl                                  the session
//	<project>/<session_id>/subagents/agent-<agent_id>.jsonl       one sub agent
//	<project>/<session_id>/subagents/agent-<agent_id>.meta.json   its metadata
//
// The transcripts hold the same user/assistant lines as a session, so they
// render through exactly the same pipeline.
const (
	subagentDirName    = "subagents"
	subagentFilePrefix = "agent-"
	subagentFileSuffix = ".jsonl"
	subagentMetaSuffix = ".meta.json"
)

// subagentInfo is one sub agent transcript found next to a session.
type subagentInfo struct {
	// id is the part between "agent-" and ".jsonl", used in the URL.
	id   string
	path string
	// agentType, name and description come from the sibling .meta.json.
	agentType   string
	name        string
	description string
	// toolUseID is set for sub agents spawned by a tool call, and matches
	// the id of the tool_use block that spawned it. Sub agents started
	// another way (a forked skill, say) have none.
	toolUseID string
}

// label is what the link to this sub agent reads as.
func (s subagentInfo) label() string {
	switch {
	case s.description != "":
		return s.description
	case s.name != "":
		return s.name
	case s.agentType != "":
		return s.agentType
	default:
		return s.id
	}
}

// subagentMeta mirrors agent-<id>.meta.json.
type subagentMeta struct {
	AgentType   string `json:"agentType"`
	Name        string `json:"name"`
	Description string `json:"description"`
	ToolUseID   string `json:"toolUseId"`
}

// subagentsDir returns the directory holding the sub agent transcripts of
// the session whose jsonl is at sessionPath.
func subagentsDir(sessionPath string) string {
	return filepath.Join(strings.TrimSuffix(sessionPath, ".jsonl"), subagentDirName)
}

// findSubagents returns every sub agent transcript belonging to the session
// at sessionPath, sorted by id so the page is stable across renders. A
// session with no sub agents (the common case) yields none, and an
// unreadable directory is treated the same way.
func findSubagents(sessionPath string) []subagentInfo {
	dir := subagentsDir(sessionPath)
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var agents []subagentInfo
	for _, f := range files {
		name := f.Name()
		if f.IsDir() || !strings.HasPrefix(name, subagentFilePrefix) || !strings.HasSuffix(name, subagentFileSuffix) {
			continue
		}
		id := strings.TrimSuffix(strings.TrimPrefix(name, subagentFilePrefix), subagentFileSuffix)
		if id == "" {
			continue
		}
		agent := subagentInfo{id: id, path: filepath.Join(dir, name)}
		applySubagentMeta(&agent, filepath.Join(dir, subagentFilePrefix+id+subagentMetaSuffix))
		agents = append(agents, agent)
	}
	sort.Slice(agents, func(i, j int) bool { return agents[i].id < agents[j].id })
	return agents
}

// applySubagentMeta fills in what agent-<id>.meta.json knows, leaving the
// agent as-is when the file is missing or unreadable: the transcript is
// still viewable without it.
func applySubagentMeta(agent *subagentInfo, metaPath string) {
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return
	}
	var meta subagentMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return
	}
	agent.agentType, agent.name = meta.AgentType, meta.Name
	agent.description, agent.toolUseID = meta.Description, meta.ToolUseID
}

// findSubagent looks up one sub agent of a session by id. The id is matched
// against the directory listing rather than pasted into a path, so an id
// arriving from a URL cannot reach outside the session's own directory.
func findSubagent(sessionPath, agentID string) (subagentInfo, bool) {
	for _, a := range findSubagents(sessionPath) {
		if a.id == agentID {
			return a, true
		}
	}
	return subagentInfo{}, false
}
