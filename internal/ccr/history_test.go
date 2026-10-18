package ccr

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func tokens(text string, query bool) []string {
	var out []string
	_ = tokenizeJA(text, query, func(token string, start, end int) error {
		out = append(out, token+"@"+text[start:end])
		return nil
	})
	return out
}

func TestTokenizeJA(t *testing.T) {
	cases := []struct {
		text  string
		query bool
		want  []string
	}{
		// a CJK run becomes bigrams plus its last character; words stay whole
		{"Goで全文検索", false, []string{"go@Go", "で全@で全", "全文@全文", "文検@文検", "検索@検索", "索@索"}},
		// a query leaves the last character out, so the bigrams form a phrase
		{"全文検索", true, []string{"全文@全文", "文検@文検", "検索@検索"}},
		{"検", true, []string{"検@検"}},
		// full-width ASCII and half-width katakana fold; punctuation splits
		{"ＳＱＬite、ﾃｽﾄ。", false, []string{"sqlite@ＳＱＬite", "テス@ﾃｽ", "スト@ｽﾄ", "ト@ﾄ"}},
		{"ユーザー_id 123", false, []string{"ユー@ユー", "ーザ@ーザ", "ザー@ザー", "ー@ー", "_id@_id", "123@123"}},
		{"  ...  ", false, nil},
	}
	for _, c := range cases {
		if got := tokens(c.text, c.query); !reflect.DeepEqual(got, c.want) {
			t.Errorf("tokenizeJA(%q, %v) = %q, want %q", c.text, c.query, got, c.want)
		}
	}
}

func TestFTSQuery(t *testing.T) {
	cases := map[string]string{
		"検索 Go":     `"検索" "Go"`,
		"検":        `"検"*`,
		`say "hi"`: `"say" """hi"""`,
		"、 。":       "",
		"":         "",
	}
	for in, want := range cases {
		if got := ftsQuery(in); got != want {
			t.Errorf("ftsQuery(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFuseRankings(t *testing.T) {
	// 2 is near the top of both lists, so it beats 1, first in only one
	got := fuseRankings([]int64{1, 2, 3}, []int64{2, 4})
	if want := []int64{2, 1, 4, 3}; !reflect.DeepEqual(got, want) {
		t.Errorf("fuseRankings = %v, want %v", got, want)
	}
}

func TestSnippet(t *testing.T) {
	text := strings.Repeat("あ", 100) + "全文検索" + strings.Repeat("い", 100)
	got := snippet(text, searchTerms("検索"), 20, false)
	if !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "…") || !strings.Contains(got, "全文検索") {
		t.Errorf("snippet = %q, want the match in the middle of an elided window", got)
	}
	if got := snippet("line one\n\tline two", nil, 100, false); got != "line one line two" {
		t.Errorf("snippet = %q, want whitespace collapsed", got)
	}
	if got := snippet("ＧＯ rocks", searchTerms("go"), 100, true); !strings.Contains(got, "\x1b[") {
		t.Errorf("snippet = %q, want the folded match highlighted", got)
	}
}

func newTestHistoryDB(t *testing.T) *historyDB {
	t.Helper()
	h, err := openHistoryDB(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatalf("openHistoryDB: %v", err)
	}
	t.Cleanup(func() { h.Close() }) //nolint:errcheck
	return h
}

func addMessages(t *testing.T, h *historyDB, msgs ...historyMessage) []int64 {
	t.Helper()
	var ids []int64
	for _, m := range msgs {
		if m.createdAt.IsZero() {
			m.createdAt = time.Now()
		}
		id, err := h.add(m)
		if err != nil {
			t.Fatalf("add: %v", err)
		}
		ids = append(ids, id)
	}
	return ids
}

func TestKeywordSearch(t *testing.T) {
	h := newTestHistoryDB(t)
	ids := addMessages(t, h,
		historyMessage{sessionID: "s1", role: "user", cwd: "/a", text: "全文検索を日本語に対応させたい"},
		historyMessage{sessionID: "s1", role: "assistant", cwd: "/a", text: "FTS5 で検索できるようにしました"},
		historyMessage{sessionID: "s2", role: "user", cwd: "/b", text: "Ｇｏ のテストを直して"},
	)

	cases := []struct {
		query string
		f     historyFilter
		want  []int64
	}{
		{"検索", historyFilter{}, []int64{ids[0], ids[1]}},
		{"全文検索", historyFilter{}, []int64{ids[0]}},
		{"検", historyFilter{}, []int64{ids[0], ids[1]}},
		{"索", historyFilter{}, []int64{ids[0], ids[1]}}, // last character of a run
		{"go", historyFilter{}, []int64{ids[2]}},
		{"fts5 検索", historyFilter{}, []int64{ids[1]}},
		{"検索", historyFilter{role: "assistant"}, []int64{ids[1]}},
		{"検索", historyFilter{cwd: "/b"}, nil},
		{"検索日本", historyFilter{}, nil}, // not a substring
	}
	for _, c := range cases {
		got, err := h.keywordSearch(ftsQuery(c.query), c.f, 10)
		if err != nil {
			t.Fatalf("keywordSearch(%q): %v", c.query, err)
		}
		// equal BM25 scores may come back in either order
		if !sameIDs(got, c.want) {
			t.Errorf("keywordSearch(%q, %+v) = %v, want %v", c.query, c.f, got, c.want)
		}
	}
}

func sameIDs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[int64]int)
	for _, id := range a {
		seen[id]++
	}
	for _, id := range b {
		seen[id]--
	}
	for _, n := range seen {
		if n != 0 {
			return false
		}
	}
	return true
}

// fakeEmbeddings serves an OpenAI-style embeddings endpoint whose vectors
// count the letters a, b and c in each input, so nearness is predictable.
func fakeEmbeddings(t *testing.T) (*embeddingConfig, *int) {
	t.Helper()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Model != "fake" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		type item struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		}
		var out struct {
			Data []item `json:"data"`
		}
		for i, in := range req.Input {
			v := []float32{0.01, 0.01, 0.01}
			for _, r := range in {
				if r >= 'a' && r <= 'c' {
					v[r-'a']++
				}
			}
			out.Data = append(out.Data, item{i, v})
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(srv.Close)
	return &embeddingConfig{URL: srv.URL, Model: "fake"}, &calls
}

func TestVectorSearch(t *testing.T) {
	cfg, _ := fakeEmbeddings(t)
	e, err := newEmbedder(cfg)
	if err != nil {
		t.Fatal(err)
	}
	h := newTestHistoryDB(t)
	ids := addMessages(t, h,
		historyMessage{sessionID: "s", role: "user", cwd: "/a", text: "aaaa"},
		historyMessage{sessionID: "s", role: "user", cwd: "/a", text: "bbbb"},
		historyMessage{sessionID: "s", role: "assistant", cwd: "/b", text: "aaab"},
	)

	if err := h.prepareVectors(e.cfg); err != nil {
		t.Fatal(err)
	}
	if err := h.embedPending(t.Context(), e, nil); err != nil {
		t.Fatalf("embedPending: %v", err)
	}
	if n, _ := h.pendingCount(); n != 0 {
		t.Errorf("pendingCount = %d after embedPending, want 0", n)
	}

	vec, err := e.embedQuery(t.Context(), "a")
	if err != nil {
		t.Fatal(err)
	}
	got, err := h.vectorSearch(vec, historyFilter{}, 2)
	if err != nil {
		t.Fatalf("vectorSearch: %v", err)
	}
	if want := []int64{ids[0], ids[2]}; !reflect.DeepEqual(got, want) {
		t.Errorf("vectorSearch = %v, want %v", got, want)
	}
	got, err = h.vectorSearch(vec, historyFilter{cwd: "/a"}, 5)
	if err != nil {
		t.Fatalf("vectorSearch: %v", err)
	}
	if want := []int64{ids[0], ids[1]}; !reflect.DeepEqual(got, want) {
		t.Errorf("vectorSearch(cwd=/a) = %v, want %v", got, want)
	}

	// a different model invalidates every stored vector
	other := *cfg
	other.Model = "other"
	if err := h.prepareVectors(other); err != nil {
		t.Fatal(err)
	}
	if n, _ := h.pendingCount(); n != 3 {
		t.Errorf("pendingCount = %d after a model change, want 3", n)
	}
	if ok, _ := h.hasVectors(); ok {
		t.Error("vector table kept after a model change")
	}
}

func runHook(t *testing.T, in hookInput) {
	t.Helper()
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if err := RunHook(strings.NewReader(string(data))); err != nil {
		t.Fatalf("RunHook(%s): %v", in.HookEventName, err)
	}
}

func TestRunHookRecordsPromptAndAnswer(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "history.db")
	t.Setenv("CCR_HISTORY_DB", dbPath)
	t.Setenv("CLAUDE_CONFIG_DIR", "/cfg")
	cfg, calls := fakeEmbeddings(t)
	data, _ := json.Marshal(fileConfig{History: historyConfig{Embedding: cfg}})
	writeCCRConfig(t, string(data))

	// the answer comes from the transcript when the hook input lacks it
	transcript := filepath.Join(t.TempDir(), "s.jsonl")
	lines := `{"type":"user","message":{"content":"first"}}
{"type":"assistant","message":{"content":[{"type":"text","text":"old answer"}]}}
{"type":"user","message":{"content":"second"}}
{"type":"assistant","message":{"content":[{"type":"text","text":"looking"},{"type":"tool_use","id":"t1","name":"Bash"}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}
{"type":"assistant","message":{"content":[{"type":"text","text":"final answer"}]}}
`
	if err := os.WriteFile(transcript, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}

	runHook(t, hookInput{SessionID: "s", Cwd: "/p", HookEventName: "UserPromptSubmit", Prompt: "second"})
	runHook(t, hookInput{SessionID: "s", Cwd: "/p", HookEventName: "Stop", LastAssistantMessage: "given answer"})
	runHook(t, hookInput{SessionID: "s", Cwd: "/p", HookEventName: "Stop", TranscriptPath: transcript})
	runHook(t, hookInput{SessionID: "s", Cwd: "/p", HookEventName: "PreToolUse"})
	runHook(t, hookInput{SessionID: "s", Cwd: "/p", HookEventName: "UserPromptSubmit", Prompt: "  "})

	h, err := openHistoryDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close() //nolint:errcheck
	msgs, err := h.messages([]int64{1, 2, 3, 4})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range msgs {
		got = append(got, m.role+":"+m.text+":"+m.cwd+":"+m.configDir)
	}
	want := []string{"user:second:/p:/cfg", "assistant:given answer:/p:/cfg", "assistant:final answer:/p:/cfg"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("recorded %q, want %q", got, want)
	}
	if n, _ := h.pendingCount(); n != 0 || *calls != 3 {
		t.Errorf("pending = %d after %d embedding calls, want every message embedded by its hook", n, *calls)
	}
}

func TestRunHookKeepsMessageWhenEmbeddingFails(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "history.db")
	t.Setenv("CCR_HISTORY_DB", dbPath)
	writeCCRConfig(t, `{"history": {"embedding": {"url": "http://127.0.0.1:1/v1/embeddings", "model": "fake"}}}`)

	data, _ := json.Marshal(hookInput{SessionID: "s", HookEventName: "UserPromptSubmit", Prompt: "hello"})
	if err := RunHook(strings.NewReader(string(data))); err == nil {
		t.Error("RunHook: want the embedding error reported")
	}

	h, err := openHistoryDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close() //nolint:errcheck
	if n, _ := h.pendingCount(); n != 1 {
		t.Errorf("pendingCount = %d, want the prompt kept for a later search to embed", n)
	}
}
