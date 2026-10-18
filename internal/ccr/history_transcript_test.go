package ccr

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildHistorySessionHTML(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	// recorded out of order, as an import after the hook would
	historyFixture(t,
		historyMessage{sessionID: "s1", role: "assistant", cwd: "/p", text: "the **answer**", createdAt: t0.Add(time.Minute)},
		historyMessage{sessionID: "s1", role: "user", cwd: "/p", text: "the question <b>", createdAt: t0},
		historyMessage{sessionID: "other", role: "user", text: "not this one", createdAt: t0},
	)

	page, err := buildHistorySessionHTML("s1")
	if err != nil {
		t.Fatalf("buildHistorySessionHTML: %v", err)
	}
	for _, want := range []string{
		`class="notice"`, "transcript file no longer exists", "2 messages",
		"<strong>Directory:</strong> /p",
		"<strong>answer</strong>", // Markdown rendered
		"the question &lt;b&gt;",  // and tags shown as typed
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Index(page, "the question") > strings.Index(page, "answer</strong>") {
		t.Error("messages are not in the order they were said")
	}
	if strings.Contains(page, "not this one") {
		t.Error("page shows another session's message")
	}
	if strings.Contains(page, `class="raw-json"`) {
		t.Error("page offers the original JSON, which is gone")
	}

	if _, err := buildHistorySessionHTML("unknown"); !errors.Is(err, errNoHistory) {
		t.Errorf("unknown session: err = %v, want errNoHistory", err)
	}
}

func TestBuildHistorySessionHTMLWithoutDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	t.Setenv("CCR_HISTORY_DB", path)
	if _, err := buildHistorySessionHTML("s1"); !errors.Is(err, errNoHistory) {
		t.Errorf("err = %v, want errNoHistory", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("looking a session up created the history database")
	}
}

func TestTranscriptServerFallsBackToHistory(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir()) // no transcripts at all
	const id = "66666666-6666-6666-6666-666666666666"
	historyFixture(t, historyMessage{sessionID: id, role: "user", text: "kept in the history"})

	get := func(path string) (int, string) {
		rec := httptest.NewRecorder()
		handleTranscriptRequest(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec.Code, rec.Body.String()
	}
	if code, body := get("/" + id); code != http.StatusOK || !strings.Contains(body, "kept in the history") {
		t.Errorf("GET /%s = %d, want the page rebuilt from the history", id, code)
	}
	if code, _ := get("/77777777-7777-7777-7777-777777777777"); code != http.StatusNotFound {
		t.Errorf("unknown session = %d, want 404", code)
	}
	if code, _ := get("/" + id + "/raw?offset=0&len=10"); code != http.StatusNotFound {
		t.Errorf("raw JSON of a deleted transcript = %d, want 404", code)
	}
}
