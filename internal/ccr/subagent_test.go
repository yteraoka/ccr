package ccr

import (
	"os"
	"path/filepath"
	"testing"
)

// writeSubagentFixture lays out a session jsonl with sub agent transcripts
// beside it, the way Claude Code stores them.
func writeSubagentFixture(t *testing.T, agents map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session-1.jsonl")
	if err := os.WriteFile(sessionPath, []byte(`{"type":"user","message":{"content":"hi"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	subDir := filepath.Join(dir, "session-1", subagentDirName)
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for id, meta := range agents {
		body := `{"type":"assistant","message":{"content":[{"type":"text","text":"done"}]}}` + "\n"
		if err := os.WriteFile(filepath.Join(subDir, "agent-"+id+".jsonl"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if meta != "" {
			if err := os.WriteFile(filepath.Join(subDir, "agent-"+id+".meta.json"), []byte(meta), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	return sessionPath
}

func TestFindSubagentsReadsMetadata(t *testing.T) {
	sessionPath := writeSubagentFixture(t, map[string]string{
		"b222": `{"agentType":"Explore","description":"Check the dialog conventions","toolUseId":"toolu_1","spawnDepth":1}`,
		"a111": `{"agentType":"general-purpose","description":"/code-review","name":"code-review","spawnDepth":1}`,
	})

	agents := findSubagents(sessionPath)
	if len(agents) != 2 {
		t.Fatalf("got %d agents, want 2: %+v", len(agents), agents)
	}
	// sorted by id so the page is stable across renders
	if agents[0].id != "a111" || agents[1].id != "b222" {
		t.Errorf("agents = %+v, want them sorted by id", agents)
	}
	if agents[0].label() != "/code-review" {
		t.Errorf("label = %q, want the description", agents[0].label())
	}
	if agents[1].toolUseID != "toolu_1" {
		t.Errorf("toolUseID = %q, want toolu_1", agents[1].toolUseID)
	}
}

func TestFindSubagentsWithoutMetadataStillUsable(t *testing.T) {
	sessionPath := writeSubagentFixture(t, map[string]string{"a111": ""})

	agents := findSubagents(sessionPath)
	if len(agents) != 1 {
		t.Fatalf("got %d agents, want 1: %+v", len(agents), agents)
	}
	// the transcript is viewable even with no metadata to name it by
	if agents[0].label() != "a111" {
		t.Errorf("label = %q, want the id as a fallback", agents[0].label())
	}
	if agents[0].path == "" {
		t.Error("agent has no transcript path")
	}
}

func TestFindSubagentsNoneForPlainSession(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session-1.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if agents := findSubagents(path); agents != nil {
		t.Errorf("findSubagents = %+v, want none for a session with no subagents dir", agents)
	}
}

func TestFindSubagentRejectsUnknownAndTraversalIDs(t *testing.T) {
	sessionPath := writeSubagentFixture(t, map[string]string{"a111": ""})

	if _, ok := findSubagent(sessionPath, "a111"); !ok {
		t.Error("findSubagent did not find the agent that exists")
	}
	// ids are matched against the directory listing, so nothing outside it
	// can be reached however the id is shaped
	for _, id := range []string{"nope", "../../etc/passwd", "..", ""} {
		if _, ok := findSubagent(sessionPath, id); ok {
			t.Errorf("findSubagent(%q) resolved, want no match", id)
		}
	}
}
