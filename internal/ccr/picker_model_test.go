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
	got := previewView("/tmp/proj", 123, "", nil, time.Time{}, end, "http://localhost:8000/abc", nil, 20, 100)

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
	got := previewView("/tmp/proj", 123, "", nil, time.Time{}, time.Time{}, "", nil, 20, 100)
	if strings.Contains(got, "Serving at:") {
		t.Errorf("did not expect a Serving at: line when servingURL is empty: %s", got)
	}
}

func TestPreviewViewError(t *testing.T) {
	got := previewView("", 0, "", nil, time.Time{}, time.Time{}, "", errors.New("boom"), 20, 40)
	if !strings.HasPrefix(got, "error: ") {
		t.Errorf("previewView(err) = %q, want it to start with \"error: \"", got)
	}
}

func TestPreviewViewWithAiTitleAndStart(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	got := previewView("/tmp/proj", 100, "My Title", nil, start, time.Time{}, "", nil, 20, 100)
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
	got := previewView("/tmp/proj", 10, "", prompts, time.Time{}, time.Time{}, "", nil, 40, 20)
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
	got := previewView("/tmp/proj", 10, "title", prompts, time.Time{}, time.Time{}, "", nil, 3, 40)
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
	m := pickerModel{
		previewCwd: "/tmp/x", previewSize: 42, previewAiTitle: "t",
		previewPrompts: []string{"p"}, previewStart: time.Now(), previewEnd: time.Now(),
	}
	m.resetPreview(nil)
	if m.previewCwd != "" || m.previewSize != 0 || m.previewAiTitle != "" || m.previewPrompts != nil {
		t.Errorf("resetPreview did not clear preview fields: %+v", m)
	}
	if !m.previewStart.IsZero() || !m.previewEnd.IsZero() {
		t.Errorf("resetPreview did not clear start/end times: %+v", m)
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
	if m.previewCwd != "/tmp/proj-a" {
		t.Errorf("previewCwd = %q, want /tmp/proj-a", m.previewCwd)
	}
	if m.previewErr != nil {
		t.Errorf("previewErr = %v, want nil", m.previewErr)
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
	if m.previewCwd != "/tmp/proj-a" {
		t.Fatalf("initial previewCwd = %q, want /tmp/proj-a", m.previewCwd)
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	pm := updated.(pickerModel)
	if pm.previewCwd != "/tmp/proj-b" {
		t.Errorf("previewCwd after moving cursor = %q, want /tmp/proj-b", pm.previewCwd)
	}
}

func TestLoadPreviewMissingSessionFileSetsErr(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	if err := os.MkdirAll(filepath.Join(configDir, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}

	m := newPickerModel([]sessionEntry{{id: "does-not-exist"}})
	if m.previewErr == nil {
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
