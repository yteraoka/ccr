package ccr

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestPreviewViewServingURLAfterEnded(t *testing.T) {
	end := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	got := previewView(previewData{
		sessionID:  "session-x",
		cwd:        "/tmp/proj",
		size:       123,
		end:        end,
		servingURL: "http://localhost:8000/abc",
	}, 20, 100)

	lines := strings.Split(got, "\n")
	endIdx, servingIdx := -1, -1
	for i, l := range lines {
		if strings.HasPrefix(l, "Ended:") {
			endIdx = i
		}
		if strings.HasPrefix(l, "Serving at:") {
			servingIdx = i
		}
	}
	if endIdx == -1 {
		t.Fatalf("no Ended: line found in %q", got)
	}
	if servingIdx != endIdx+1 {
		t.Errorf("Serving at: line at %d, want immediately after Ended: at %d\noutput:\n%s", servingIdx, endIdx, got)
	}
	if !strings.Contains(got, "Serving at: http://localhost:8000/abc") {
		t.Errorf("expected exact URL in output: %s", got)
	}
}

func TestPreviewViewNoServingURL(t *testing.T) {
	got := previewView(previewData{sessionID: "session-x", cwd: "/tmp/proj", size: 123}, 20, 100)
	if strings.Contains(got, "Serving at:") {
		t.Errorf("did not expect a Serving at: line when servingURL is empty: %s", got)
	}
}

func TestPreviewViewError(t *testing.T) {
	got := previewView(previewData{sessionID: "session-x", err: errors.New("boom")}, 20, 40)
	if !strings.HasPrefix(got, "error: ") {
		t.Errorf("previewView(err) = %q, want it to start with \"error: \"", got)
	}
}

func TestPreviewViewWithAiTitleAndStart(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	got := previewView(previewData{
		sessionID: "session-x",
		cwd:       "/tmp/proj",
		size:      100,
		aiTitle:   "My Title",
		start:     start,
	}, 20, 100)
	if !strings.Contains(got, "Title: My Title") {
		t.Errorf("previewView = %q, want Title: My Title", got)
	}
	if !strings.Contains(got, "Started:") {
		t.Errorf("previewView = %q, want Started: line", got)
	}
	if strings.Contains(got, "Ended:") {
		t.Errorf("previewView = %q, did not expect Ended: line for zero end time", got)
	}
}

func TestPreviewViewWithPromptsWraps(t *testing.T) {
	prompts := []string{"first prompt", "a much longer second prompt that needs wrapping across lines"}
	got := previewView(previewData{sessionID: "session-x", cwd: "/tmp/proj", size: 10, prompts: prompts}, 40, 20)
	if !strings.Contains(got, "Prompts:") {
		t.Errorf("previewView = %q, want a Prompts: section", got)
	}
	if !strings.Contains(got, "·first prompt") {
		t.Errorf("previewView = %q, want the first prompt bulleted", got)
	}
	lineCount := strings.Count(got, "\n") + 1
	if lineCount > 40 {
		t.Errorf("previewView produced %d lines, want at most the height budget (40)", lineCount)
	}
}

func TestPreviewViewHeightTruncation(t *testing.T) {
	prompts := []string{"p1", "p2", "p3"}
	got := previewView(previewData{
		sessionID: "session-x",
		cwd:       "/tmp/proj",
		size:      10,
		aiTitle:   "title",
		prompts:   prompts,
	}, 3, 40)
	lines := strings.Split(got, "\n")
	if len(lines) != 3 {
		t.Errorf("previewView height-truncated output has %d lines, want 3: %q", len(lines), got)
	}
}

func TestTruncateNonPositiveWidthReturnsInput(t *testing.T) {
	if got := truncate("hello", 0); got != "hello" {
		t.Errorf("truncate(width<=0) = %q, want input unchanged", got)
	}
}

func TestTruncateShortStringUnchanged(t *testing.T) {
	if got := truncate("hi", 10); got != "hi" {
		t.Errorf("truncate(fits) = %q, want unchanged", got)
	}
}

func TestTruncateLongStringCutWithEllipsis(t *testing.T) {
	got := truncate("hello world", 5)
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncate(too long) = %q, want it to end with an ellipsis", got)
	}
}

func TestViewProducesListAndPreview(t *testing.T) {
	setupPreviewFixture(t, "session-a", "/tmp/proj-a")
	m := newPickerModel([]sessionEntry{{id: "session-a"}})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(pickerModel)

	got := ansi.Strip(m.View().Content)
	if !strings.Contains(got, "SESSION ID") {
		t.Errorf("View() = %q, want the list header", got)
	}
	if !strings.Contains(got, "Directory: /tmp/proj-a") {
		t.Errorf("View() = %q, want the preview pane", got)
	}
}

func TestHumanSize(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
	}
	for _, c := range cases {
		if got := humanSize(c.in); got != c.want {
			t.Errorf("humanSize(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestHumanCount(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1.0K"},
		{1500, "1.5K"},
		{1000000, "1.0M"},
		// just below a unit boundary: %.1f rounds up, so these must roll
		// over to the next unit instead of printing "1000.0K"
		{999999, "1.0M"},
		{999999999, "1.0G"},
	}
	for _, c := range cases {
		if got := humanCount(c.in); got != c.want {
			t.Errorf("humanCount(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPreviewViewShowsTokenBreakdown(t *testing.T) {
	p := previewData{
		sessionID: "session-x",
		cwd:       "/tmp/proj",
		size:      123,
		tokens:    tokenUsage{input: 1200, output: 3400, cacheCreation: 56000, cacheRead: 780000},
	}
	got := previewView(p, 20, 200)

	want := "Tokens: 840.6K (in 1.2K / out 3.4K / cache write 56.0K / cache read 780.0K)"
	if !strings.Contains(got, want) {
		t.Errorf("previewView = %q, want a line containing %q", got, want)
	}
	// the whole line has to survive an 80-column terminal uncut
	narrow := previewView(p, 20, 80)
	if !strings.Contains(narrow, want) {
		t.Errorf("previewView(width=80) = %q, want the token line to fit uncut", narrow)
	}
}

func TestViewShowsFullSessionIDWhenListShortensIt(t *testing.T) {
	id := "0123abcd-4567-89ef-0123-456789abcdef"
	setupPreviewFixture(t, id, "/tmp/proj-a")

	m := newPickerModel([]sessionEntry{{id: id}})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	m = updated.(pickerModel)

	got := ansi.Strip(m.View().Content)
	if !strings.Contains(got, "Session: "+id) {
		t.Errorf("View() = %q, want the preview to carry the full session id", got)
	}
}

func TestListViewShowsTokensColumn(t *testing.T) {
	sessions := []sessionEntry{{id: "abc", tokens: 1500}}
	got := ansi.Strip(listView(sessions, 0, 10, 100))

	if !strings.Contains(got, "TOKENS") {
		t.Errorf("listView header = %q, want a TOKENS column", got)
	}
	if !strings.Contains(got, "1.5K") {
		t.Errorf("listView = %q, want the session's token count formatted as 1.5K", got)
	}
}

func TestListIDWidthShortensOnNarrowTerminals(t *testing.T) {
	wide := rowPrefixWidth(fullIDWidth) + minCwdWidth
	if got := listIDWidth(wide); got != fullIDWidth {
		t.Errorf("listIDWidth(%d) = %d, want the full id width %d", wide, got, fullIDWidth)
	}
	if got := listIDWidth(wide - 1); got != shortIDWidth {
		t.Errorf("listIDWidth(%d) = %d, want the short id width %d", wide-1, got, shortIDWidth)
	}
	if got := listIDWidth(80); got != shortIDWidth {
		t.Errorf("listIDWidth(80) = %d, want the short id width %d", got, shortIDWidth)
	}
}

func TestListViewKeepsCwdVisibleOnNarrowTerminal(t *testing.T) {
	id := "0123abcd-4567-89ef-0123-456789abcdef"
	sessions := []sessionEntry{{id: id, cwd: "/home/me/projects/my-project", tokens: 1500}}

	narrow := ansi.Strip(listView(sessions, 0, 10, 80))
	if strings.Contains(narrow, id) {
		t.Errorf("listView(80) = %q, want the session id shortened", narrow)
	}
	if !strings.Contains(narrow, id[:shortIDWidth]) {
		t.Errorf("listView(80) = %q, want the id's first %d characters", narrow, shortIDWidth)
	}
	if !strings.Contains(narrow, "my-project") {
		t.Errorf("listView(80) = %q, want the full CWD basename to still fit", narrow)
	}
	if !strings.Contains(narrow, "ID") {
		t.Errorf("listView(80) = %q, want a shortened id header", narrow)
	}

	wide := ansi.Strip(listView(sessions, 0, 10, 120))
	if !strings.Contains(wide, id) {
		t.Errorf("listView(120) = %q, want the full session id", wide)
	}
	if !strings.Contains(wide, "SESSION ID") {
		t.Errorf("listView(120) = %q, want the full id header", wide)
	}
	if !strings.Contains(wide, "my-project") {
		t.Errorf("listView(120) = %q, want the CWD basename", wide)
	}
}

func TestWrapNonPositiveWidthReturnsUnsplit(t *testing.T) {
	got := wrap("hello world", 0)
	if len(got) != 1 || got[0] != "hello world" {
		t.Errorf("wrap(width<=0) = %v, want unsplit input", got)
	}
}

func TestWrapSplitsLongLine(t *testing.T) {
	got := wrap("hello world foo bar", 8)
	if len(got) < 2 {
		t.Fatalf("wrap = %v, want multiple lines", got)
	}
	for _, l := range got {
		if len(l) > 8 {
			t.Errorf("line %q exceeds width 8", l)
		}
	}
}

func TestInitReturnsNilCmd(t *testing.T) {
	var m pickerModel
	if cmd := m.Init(); cmd != nil {
		t.Errorf("Init() = %v, want nil", cmd)
	}
}

func TestUpdateWindowSizeMsg(t *testing.T) {
	m := pickerModel{sessions: []sessionEntry{{id: "a"}}}
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	pm := updated.(pickerModel)
	if pm.width != 120 || pm.height != 40 {
		t.Errorf("got width=%d height=%d, want 120x40", pm.width, pm.height)
	}
}

func TestUpdateCursorMovement(t *testing.T) {
	m := pickerModel{sessions: []sessionEntry{{id: "a"}, {id: "b"}, {id: "c"}}}

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	pm := updated.(pickerModel)
	if pm.cursor != 1 {
		t.Fatalf("cursor after down = %d, want 1", pm.cursor)
	}

	updated, _ = pm.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	pm = updated.(pickerModel)
	if pm.cursor != 0 {
		t.Fatalf("cursor after up = %d, want 0", pm.cursor)
	}

	// up at the top stays at 0
	updated, _ = pm.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	pm = updated.(pickerModel)
	if pm.cursor != 0 {
		t.Errorf("cursor after up at top = %d, want 0", pm.cursor)
	}

	// down past the bottom stays at len-1
	for i := 0; i < 5; i++ {
		updated, _ = pm.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		pm = updated.(pickerModel)
	}
	if pm.cursor != 2 {
		t.Errorf("cursor after repeated down = %d, want 2 (clamped)", pm.cursor)
	}
}

func TestUpdateEnterSelectsAndQuits(t *testing.T) {
	m := pickerModel{sessions: []sessionEntry{{id: "a"}, {id: "b"}}, cursor: 1}
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	pm := updated.(pickerModel)
	if pm.selected.id != "b" {
		t.Errorf("selected.id = %q, want %q", pm.selected.id, "b")
	}
	if cmd == nil {
		t.Fatal("expected a quit command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("cmd() = %T, want tea.QuitMsg", cmd())
	}
}

func TestUpdateQuitKeys(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{
		{Code: 'c', Mod: tea.ModCtrl},
		{Code: tea.KeyEsc},
		{Code: 'q'},
	} {
		m := pickerModel{sessions: []sessionEntry{{id: "a"}}}
		_, cmd := m.Update(key)
		if cmd == nil {
			t.Fatalf("key %q: expected a quit command", key.String())
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Errorf("key %q: cmd() = %T, want tea.QuitMsg", key.String(), cmd())
		}
	}
}

func TestResetPreviewClearsState(t *testing.T) {
	m := pickerModel{preview: previewData{
		sessionID: "session-x", cwd: "/tmp/x", size: 42, aiTitle: "t",
		prompts: []string{"p"}, start: time.Now(), end: time.Now(),
		tokens: tokenUsage{input: 1}, servingURL: "http://localhost:8000/session-x",
	}}
	m.resetPreview(nil)

	if m.preview.cwd != "" || m.preview.size != 0 || m.preview.aiTitle != "" || m.preview.prompts != nil {
		t.Errorf("resetPreview did not clear preview fields: %+v", m.preview)
	}
	if !m.preview.start.IsZero() || !m.preview.end.IsZero() {
		t.Errorf("resetPreview did not clear start/end times: %+v", m.preview)
	}
	if m.preview.tokens != (tokenUsage{}) {
		t.Errorf("resetPreview did not clear token usage: %+v", m.preview)
	}
	// the id and serving URL describe the session itself, not what was read
	// out of it, so they survive
	if m.preview.sessionID != "session-x" {
		t.Errorf("resetPreview cleared sessionID: %+v", m.preview)
	}
	if m.preview.servingURL != "http://localhost:8000/session-x" {
		t.Errorf("resetPreview cleared servingURL: %+v", m.preview)
	}
}

// setupPreviewFixture creates a fake ${CLAUDE_CONFIG_DIR}/projects/.../<id>.jsonl
// so loadPreview (via findSessionFile/parseSessionInfo) can resolve it.
func setupPreviewFixture(t *testing.T, sessionID, cwd string) {
	t.Helper()
	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)

	projDir := filepath.Join(configDir, "projects", encodeProjectDir(cwd))
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"type":"user","cwd":"` + cwd + `","timestamp":"2026-01-01T00:00:00Z"}` + "\n"
	if err := os.WriteFile(filepath.Join(projDir, sessionID+".jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestNewPickerModelLoadsInitialPreview(t *testing.T) {
	setupPreviewFixture(t, "session-a", "/tmp/proj-a")

	m := newPickerModel([]sessionEntry{{id: "session-a"}})
	if m.preview.cwd != "/tmp/proj-a" {
		t.Errorf("previewCwd = %q, want /tmp/proj-a", m.preview.cwd)
	}
	if m.preview.err != nil {
		t.Errorf("previewErr = %v, want nil", m.preview.err)
	}
}

func TestLoadPreviewUpdatesOnCursorMove(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)

	for id, cwd := range map[string]string{"session-a": "/tmp/proj-a", "session-b": "/tmp/proj-b"} {
		projDir := filepath.Join(configDir, "projects", encodeProjectDir(cwd))
		if err := os.MkdirAll(projDir, 0o755); err != nil {
			t.Fatal(err)
		}
		content := `{"type":"user","cwd":"` + cwd + `","timestamp":"2026-01-01T00:00:00Z"}` + "\n"
		if err := os.WriteFile(filepath.Join(projDir, id+".jsonl"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	m := newPickerModel([]sessionEntry{{id: "session-a"}, {id: "session-b"}})
	if m.preview.cwd != "/tmp/proj-a" {
		t.Fatalf("initial previewCwd = %q, want /tmp/proj-a", m.preview.cwd)
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	pm := updated.(pickerModel)
	if pm.preview.cwd != "/tmp/proj-b" {
		t.Errorf("previewCwd after moving cursor = %q, want /tmp/proj-b", pm.preview.cwd)
	}
}

func TestLoadPreviewMissingSessionFileSetsErr(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	if err := os.MkdirAll(filepath.Join(configDir, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}

	m := newPickerModel([]sessionEntry{{id: "does-not-exist"}})
	if m.preview.err == nil {
		t.Error("expected previewErr to be set for a missing session file")
	}
}

func TestListViewKeyLegendIsLastLine(t *testing.T) {
	sessions := []sessionEntry{{id: "abc"}, {id: "def"}}
	got := listView(sessions, 0, 10, 100)

	lines := strings.Split(got, "\n")
	if len(lines) != 10 {
		t.Fatalf("got %d lines, want 10 (height budget): %q", len(lines), got)
	}
	last := lines[len(lines)-1]
	if !strings.Contains(last, "move") || !strings.Contains(last, "quit") {
		t.Errorf("last line = %q, want it to contain the key legend", last)
	}
}

func TestPickerOpensAndClosesTheJSONLViewer(t *testing.T) {
	setupPreviewFixture(t, "session-a", "/tmp/proj-a")
	m := newPickerModel([]sessionEntry{{id: "session-a"}})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = updated.(pickerModel)

	updated, _ = m.Update(tea.KeyPressMsg{Code: 'i'})
	m = updated.(pickerModel)
	if m.viewer == nil {
		t.Fatal("i did not open the jsonl viewer")
	}
	got := ansi.Strip(m.View().Content)
	if !strings.Contains(got, "LINE") || strings.Contains(got, "SESSION ID") {
		t.Errorf("View() = %q, want the viewer instead of the picker", got)
	}

	// while it is open the picker's own keys are the viewer's
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'q'})
	m = updated.(pickerModel)
	if cmd != nil {
		t.Error("q inside the viewer should return to the picker, not quit ccr")
	}
	if m.viewer != nil {
		t.Error("q did not close the viewer")
	}
	if got := ansi.Strip(m.View().Content); !strings.Contains(got, "SESSION ID") {
		t.Errorf("View() = %q, want the picker back", got)
	}
}

func TestPickerJSONLViewerMissingSessionShowsStatus(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	if err := os.MkdirAll(filepath.Join(configDir, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}

	m := pickerModel{sessions: []sessionEntry{{id: "does-not-exist"}}}
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'i'})
	pm := updated.(pickerModel)

	if pm.viewer != nil {
		t.Error("no viewer should open for a session that cannot be read")
	}
	if !strings.HasPrefix(pm.statusMsg, "error: ") {
		t.Errorf("statusMsg = %q, want the failure reported", pm.statusMsg)
	}
}
