package ccr

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestPromptText(t *testing.T) {
	cases := []struct {
		content string
		text    string
		ok      bool
	}{
		{`"  hello  "`, "hello", true},
		{`[{"type":"text","text":"look at this"},{"type":"image"}]`, "look at this", true},
		{`[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]`, "", false},
		{`[{"type":"text","text":"[Request interrupted by user]"}]`, "", true},
		{`"<local-command-stdout>done</local-command-stdout>"`, "", true},
		{`"<command-name>/review</command-name>\n<command-message>review</command-message>\n<command-args>123</command-args>"`, "/review 123", true},
		{`"<command-message>clear</command-message>\n<command-name>/clear</command-name>\n<command-args></command-args>"`, "/clear", true},
	}
	for _, c := range cases {
		text, ok := promptText(json.RawMessage(c.content))
		if text != c.text || ok != c.ok {
			t.Errorf("promptText(%s) = %q, %v; want %q, %v", c.content, text, ok, c.text, c.ok)
		}
	}
}

func writeTranscript(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var data []byte
	for _, l := range lines {
		data = append(data, l...)
		data = append(data, '\n')
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

const (
	tPrompt1   = `{"type":"user","cwd":"/p","timestamp":"2026-01-01T00:00:00Z","promptSource":"typed","message":{"content":"first question"}}`
	tThinking  = `{"type":"assistant","timestamp":"2026-01-01T00:00:01Z","message":{"id":"m1","stop_reason":"tool_use","content":[{"type":"thinking","thinking":"hmm"}]}}`
	tMidText   = `{"type":"assistant","timestamp":"2026-01-01T00:00:02Z","message":{"id":"m1","stop_reason":"tool_use","content":[{"type":"text","text":"let me look"}]}}`
	tToolUse   = `{"type":"assistant","timestamp":"2026-01-01T00:00:03Z","message":{"id":"m1","stop_reason":"tool_use","content":[{"type":"tool_use","id":"t1","name":"Bash"}]}}`
	tResult    = `{"type":"user","cwd":"/p","timestamp":"2026-01-01T00:00:04Z","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}`
	tFinal1    = `{"type":"assistant","cwd":"/p","timestamp":"2026-01-01T00:00:05Z","message":{"id":"m2","stop_reason":"end_turn","content":[{"type":"text","text":"the answer"}]}}`
	tFinal2    = `{"type":"assistant","cwd":"/p","timestamp":"2026-01-01T00:00:05Z","message":{"id":"m2","stop_reason":"end_turn","content":[{"type":"text","text":"and more"}]}}`
	tMeta      = `{"type":"user","isMeta":true,"message":{"content":"Stop hook feedback: commit"}}`
	tNotice    = `{"type":"user","promptSource":"system","message":{"content":"<task-notification>done</task-notification>"}}`
	tSidechain = `{"type":"user","isSidechain":true,"message":{"content":"sub agent prompt"}}`
	tPrompt2   = `{"type":"user","cwd":"/p/sub","timestamp":"2026-01-01T00:01:00Z","message":{"content":"second question"}}`
	tRunning   = `{"type":"assistant","timestamp":"2026-01-01T00:01:01Z","message":{"id":"m3","stop_reason":"tool_use","content":[{"type":"text","text":"working on it"},{"type":"tool_use","id":"t2","name":"Read"}]}}`
)

func messageSummary(msgs []historyMessage) []string {
	var out []string
	for _, m := range msgs {
		out = append(out, m.role+"|"+m.text+"|"+m.cwd+"|"+m.createdAt.UTC().Format(time.TimeOnly))
	}
	return out
}

func TestExtractHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "projects", "-p", "sess-1.jsonl")
	writeTranscript(t, path, tPrompt1, tThinking, tMidText, tToolUse, tResult, tFinal1, tFinal2,
		tMeta, tNotice, tSidechain, `not json`, "", tPrompt2, tRunning)

	msgs, err := extractHistory(path, "/cfg")
	if err != nil {
		t.Fatal(err)
	}
	// the turn's last message with text, all of its blocks; the second
	// turn is still running, so only its prompt is in
	want := []string{
		"user|first question|/p|00:00:00",
		"assistant|the answer\n\nand more|/p|00:00:05",
		"user|second question|/p/sub|00:01:00",
	}
	if got := messageSummary(msgs); !reflect.DeepEqual(got, want) {
		t.Errorf("extractHistory = %q, want %q", got, want)
	}
	for _, m := range msgs {
		if m.sessionID != "sess-1" || m.configDir != "/cfg" {
			t.Errorf("message %+v: want session sess-1 in /cfg", m)
		}
	}

	// once the turn finishes, its answer is there
	writeTranscript(t, path, tPrompt2, tRunning,
		`{"type":"assistant","timestamp":"2026-01-01T00:01:09Z","message":{"id":"m4","stop_reason":"end_turn","content":[{"type":"text","text":"done now"}]}}`)
	msgs, err = extractHistory(path, "/cfg")
	if err != nil {
		t.Fatal(err)
	}
	if got := messageSummary(msgs); len(got) != 2 || got[1] != "assistant|done now||00:01:09" {
		t.Errorf("extractHistory after the turn ended = %q", got)
	}
}

func TestRunImport(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	dbPath := filepath.Join(t.TempDir(), "history.db")
	t.Setenv("CCR_HISTORY_DB", dbPath)
	embedCfg, calls := fakeEmbeddings(t)
	data, _ := json.Marshal(fileConfig{History: historyConfig{Embedding: embedCfg}})
	writeCCRConfig(t, string(data))

	sess := filepath.Join(cfg, "projects", "-p", "sess-1.jsonl")
	writeTranscript(t, sess, tPrompt1, tFinal1)
	// a sub agent's transcript is not the conversation
	writeTranscript(t, filepath.Join(cfg, "projects", "-p", "sess-1", "subagents", "agent-a.jsonl"), tPrompt2)

	// the hook already recorded the first prompt
	h, err := openHistoryDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	addMessages(t, h, historyMessage{sessionID: "sess-1", role: "user", text: "first question"})
	h.Close() //nolint:errcheck

	if err := RunImport(nil); err != nil {
		t.Fatalf("RunImport: %v", err)
	}
	countAll := func() (rows, pending int) {
		t.Helper()
		h, err := openHistoryDB(dbPath)
		if err != nil {
			t.Fatal(err)
		}
		defer h.Close() //nolint:errcheck
		if err := h.db.QueryRow(`SELECT count(*) FROM messages`).Scan(&rows); err != nil {
			t.Fatal(err)
		}
		pending, _ = h.pendingCount()
		return rows, pending
	}
	if rows, pending := countAll(); rows != 2 || pending != 0 {
		t.Errorf("after import: %d messages, %d unembedded; want the prompt kept once, the answer added, all embedded", rows, pending)
	}
	if *calls == 0 {
		t.Error("import embedded nothing")
	}

	// unchanged: nothing read; grown: only the new message added
	before := *calls
	if err := RunImport(nil); err != nil {
		t.Fatal(err)
	}
	if rows, _ := countAll(); rows != 2 || *calls != before {
		t.Errorf("re-import of an unchanged transcript: %d messages, %d embedding calls", rows, *calls-before)
	}
	writeTranscript(t, sess, tPrompt1, tFinal1, tPrompt2)
	if err := RunImport([]string{"-no-embed"}); err != nil {
		t.Fatal(err)
	}
	if rows, pending := countAll(); rows != 3 || pending != 1 {
		t.Errorf("after the transcript grew: %d messages, %d unembedded; want 3 and the new one left for later", rows, pending)
	}
}
