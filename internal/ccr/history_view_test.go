package ccr

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// historyFixture records msgs in a fresh history database the history
// screen will search.
func historyFixture(t *testing.T, msgs ...historyMessage) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "history.db")
	t.Setenv("CCR_HISTORY_DB", path)
	h, err := openHistoryDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close() //nolint:errcheck
	addMessages(t, h, msgs...)
}

func typeKeys(t *testing.T, m tea.Model, s string) tea.Model {
	t.Helper()
	for _, r := range s {
		m, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return m
}

// runSearch presses enter on the typed query and feeds the background
// search's result back in, as bubbletea would.
func runSearch(t *testing.T, m tea.Model) pickerModel {
	t.Helper()
	m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on a query started no search")
	}
	if got := m.(pickerModel).history.footer(); got != "searching…" {
		t.Errorf("footer while searching = %q", got)
	}
	m, _ = m.Update(cmd())
	return m.(pickerModel)
}

func openHistory(t *testing.T) tea.Model {
	t.Helper()
	m := pickerModel{sessions: []sessionEntry{{id: "s-picker"}}, global: true, width: 120, height: 30}
	updated, _ := m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	pm := updated.(pickerModel)
	if pm.history == nil || !pm.history.open || !pm.history.typing {
		t.Fatalf("s did not open the history prompt: %+v", pm.history)
	}
	return pm
}

// sessionTranscripts lays down a transcript for each of ids in a fresh
// CLAUDE_CONFIG_DIR, so the history screen sees them as still there.
func sessionTranscripts(t *testing.T, ids ...string) {
	t.Helper()
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	for _, id := range ids {
		writeTranscript(t, filepath.Join(cfg, "projects", "-work", id+".jsonl"), tPrompt1)
	}
}

func TestHistoryScreenSearchesAndResumes(t *testing.T) {
	sessionTranscripts(t, "s1", "s2", "s3")
	historyFixture(t,
		historyMessage{sessionID: "s1", role: "user", cwd: "/work/a", configDir: "/cfg-a", text: "全文検索を日本語に対応させたい", createdAt: time.Now().Add(-time.Hour)},
		historyMessage{sessionID: "s2", role: "assistant", cwd: "/work/b", text: "日本語の検索には bigram を使います"},
		historyMessage{sessionID: "s3", role: "user", cwd: "/work/c", text: "テストを直して"},
	)

	m := runSearch(t, typeKeys(t, openHistory(t), "日本語の検索"))
	v := m.history
	if len(v.results) != 2 || v.typing || v.searching {
		t.Fatalf("results = %+v (typing %v, searching %v)", v.results, v.typing, v.searching)
	}
	if !strings.Contains(v.status, "2 results") {
		t.Errorf("status = %q", v.status)
	}

	screen := ansi.Strip(m.View().Content)
	for _, want := range []string{"MESSAGE", "全文検索を日本語に対応させたい", "日本語の検索には", "Session: "} {
		if !strings.Contains(screen, want) {
			t.Errorf("screen lacks %q:\n%s", want, screen)
		}
	}

	// enter resumes the session the message under the cursor came from
	var cmd tea.Cmd
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	updated, _ = updated.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	cur, _ := updated.(pickerModel).history.current()
	updated, cmd = updated.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := updated.(pickerModel).selected
	if want := (sessionEntry{id: cur.sessionID, cwd: cur.cwd, configDir: cur.configDir}); got != want || cmd == nil {
		t.Errorf("selected %+v (cmd %v), want %+v and quit", got, cmd != nil, want)
	}
}

func TestHistoryScreenClosesAndKeepsLastSearch(t *testing.T) {
	historyFixture(t, historyMessage{sessionID: "s1", role: "user", text: "hook の設定"})

	// esc before any search: back to the picker
	updated, _ := openHistory(t).Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if pm := updated.(pickerModel); pm.history.open {
		t.Error("esc on an empty prompt left the history screen open")
	}

	m := runSearch(t, typeKeys(t, openHistory(t), "hook"))
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if updated.(pickerModel).history.open {
		t.Fatal("q left the history screen open")
	}
	if !strings.Contains(ansi.Strip(updated.View().Content), "SESSION ID") {
		t.Error("q did not go back to the session list")
	}

	// s brings the last search back, ready to be edited
	updated, _ = updated.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	v := updated.(pickerModel).history
	if !v.open || !v.typing || v.query != "hook" || len(v.results) != 1 {
		t.Errorf("reopened history = %+v", v)
	}
	// esc now only stops editing: there are results to go back to
	updated, _ = updated.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if v := updated.(pickerModel).history; !v.open || v.typing {
		t.Errorf("esc while editing = open %v typing %v", v.open, v.typing)
	}
}

func TestHistoryScreenIgnoresStaleResults(t *testing.T) {
	v := &historyView{seq: 2, searching: true}
	v.finish(historyResultsMsg{seq: 1, results: []historyMessage{{id: 1}}})
	if !v.searching || v.results != nil {
		t.Errorf("an older search's results replaced the newer one's: %+v", v)
	}
}

func TestHistoryMessageViewStartsNearTheMatch(t *testing.T) {
	text := strings.Repeat("前置きの行\n", 30) + "ここで vec1 を使う\n" + strings.Repeat("後の行\n", 30)
	v := &historyView{results: []historyMessage{{sessionID: "s", text: text}}, terms: searchTerms("vec1")}
	view := v.messageView(60, 10)
	if !strings.Contains(ansi.Strip(view), "ここで vec1 を使う") {
		t.Errorf("message view does not show the match:\n%s", ansi.Strip(view))
	}
	if lines := strings.Count(view, "\n") + 1; lines != 10 {
		t.Errorf("message view is %d lines, want 10", lines)
	}
}

func TestHistoryScreenKnowsDeletedTranscripts(t *testing.T) {
	sessionTranscripts(t) // none of the sessions has its transcript any more
	historyFixture(t, historyMessage{sessionID: "gone", role: "user", text: "hook の設定"})

	m := runSearch(t, typeKeys(t, openHistory(t), "hook"))
	if screen := ansi.Strip(m.View().Content); !strings.Contains(screen, "Transcript: gone") {
		t.Errorf("preview does not say the transcript is gone:\n%s", screen)
	}
	for _, key := range []tea.KeyPressMsg{{Code: tea.KeyEnter}, {Code: 'i', Text: "i"}} {
		updated, cmd := m.Update(key)
		pm := updated.(pickerModel)
		if pm.selected.id != "" || cmd != nil || pm.viewer != nil {
			t.Errorf("%v on a session without a transcript: selected %q, viewer %v", key, pm.selected.id, pm.viewer != nil)
		}
		if !strings.Contains(pm.history.status, "transcript is gone") {
			t.Errorf("%v: status = %q", key, pm.history.status)
		}
	}
}
