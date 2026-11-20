package ccr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// goneSession lays down a session whose directory does not exist, with the
// <id>/ directory of sub agent transcripts beside it, and returns its jsonl.
func goneSession(t *testing.T, configDir, id string) (path, cwd string) {
	t.Helper()
	cwd = filepath.Join(t.TempDir(), "scratch")
	dir := filepath.Join(configDir, "projects", encodeProjectDir(cwd))
	path = filepath.Join(dir, id+".jsonl")
	writeTranscript(t, path, `{"type":"user","cwd":`+quoteJSON(cwd)+`,"timestamp":"2026-01-01T00:00:00Z","message":{"content":"q"}}`)
	writeTranscript(t, filepath.Join(dir, id, "subagents", "agent-a1.jsonl"), `{"type":"user"}`)
	return path, cwd
}

func TestRelocateSessionMovesTranscriptAndItsDirectory(t *testing.T) {
	cfg := t.TempDir()
	path, _ := goneSession(t, cfg, "s1")
	target := t.TempDir()

	if err := relocateSession(path, target); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(cfg, "projects", encodeProjectDir(target))
	for _, p := range []string{filepath.Join(dest, "s1.jsonl"), filepath.Join(dest, "s1", "subagents", "agent-a1.jsonl")} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Errorf("the emptied project directory is still there: %v", err)
	}
}

func TestRelocateSessionKeepsOtherSessionsAndRefusesToOverwrite(t *testing.T) {
	cfg := t.TempDir()
	path, cwd := goneSession(t, cfg, "s1")
	makeSession(t, cfg, cwd, "other")
	target := t.TempDir()
	makeSession(t, cfg, target, "s1") // the destination already has it

	if err := relocateSession(path, target); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("relocateSession = %v, want an already exists error", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("the transcript moved anyway: %v", err)
	}

	if err := os.Remove(filepath.Join(cfg, "projects", encodeProjectDir(target), "s1.jsonl")); err != nil {
		t.Fatal(err)
	}
	if err := relocateSession(path, target); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "other.jsonl")); err != nil {
		t.Errorf("the project directory's other session: %v", err)
	}
}

func TestSessionCwdFollowsTheProjectDirectory(t *testing.T) {
	cfg := t.TempDir()
	line := func(cwd string) string {
		return `{"type":"user","cwd":` + quoteJSON(cwd) + `,"timestamp":"2026-01-01T00:00:00Z"}`
	}
	cases := []struct {
		name, project string
		cwds          []string
		want          string
	}{
		// moved from /old to /new: the lines claude wrote before are still /old
		{"moved", "/new", []string{"/old", "/old", "/new", "/new"}, "/new"},
		// a session that wandered into a subdirectory still belongs to its own
		{"wandered", "/proj", []string{"/proj", "/proj/sub"}, "/proj"},
		// nothing matches (claude shortens very long names): the first one
		{"no match", "-elsewhere", []string{"/a", "/b"}, "/a"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var lines []string
			for _, cwd := range c.cwds {
				lines = append(lines, line(cwd))
			}
			path := filepath.Join(cfg, "projects", encodeProjectDir(c.project), c.name+".jsonl")
			writeTranscript(t, path, lines...)

			if got, _, _, err := readSessionSummary(path); err != nil || got != c.want {
				t.Errorf("readSessionSummary cwd = %q (%v), want %q", got, err, c.want)
			}
			if info, err := parseSessionInfo(path); err != nil || info.cwd != c.want {
				t.Errorf("parseSessionInfo cwd = %q (%v), want %q", info.cwd, err, c.want)
			}
		})
	}
}

func press(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }

func TestPickerEnterOnGoneDirectoryAsksWhereAndMoves(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	path, cwd := goneSession(t, cfg, "s1")
	target := t.TempDir()

	m := newPickerModel([]sessionEntry{{id: "s1", cwd: cwd, configDir: cfg}})
	m.width, m.height = 200, 30
	if !m.preview.cwdGone || !strings.Contains(ansi.Strip(m.View().Content), "Directory: (gone) "+cwd) {
		t.Errorf("preview does not say the directory is gone:\n%s", ansi.Strip(m.View().Content))
	}

	updated, cmd := m.Update(press(tea.KeyEnter))
	pm := updated.(pickerModel)
	if pm.relocate == nil || cmd != nil || pm.selected.id != "" {
		t.Fatalf("enter on a gone directory: relocate %v, cmd %v, selected %+v", pm.relocate, cmd != nil, pm.selected)
	}
	wd, _ := os.Getwd()
	if pm.relocate.input != wd {
		t.Errorf("prompt starts at %q, want ccr's own directory %q", pm.relocate.input, wd)
	}
	if footer := ansi.Strip(pm.View().Content); !strings.Contains(footer, cwd+" is gone; resume in") {
		t.Errorf("no relocate prompt on screen:\n%s", footer)
	}

	// esc gives up and leaves everything where it was
	updated, _ = pm.Update(press(tea.KeyEscape))
	if updated.(pickerModel).relocate != nil {
		t.Fatal("esc did not close the prompt")
	}
	updated, _ = updated.Update(press(tea.KeyEnter))

	// a folder that does not exist is an error, and the prompt stays
	updated, _ = updated.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	updated = typeKeys(t, updated, filepath.Join(target, "nope"))
	updated, cmd = updated.Update(press(tea.KeyEnter))
	pm = updated.(pickerModel)
	if pm.relocate == nil || pm.relocate.err == nil || cmd != nil {
		t.Fatalf("enter on a missing folder: relocate %+v, cmd %v", pm.relocate, cmd != nil)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the session moved on an error: %v", err)
	}

	updated, _ = updated.Update(tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl})
	updated, cmd = updated.Update(press(tea.KeyEnter))
	pm = updated.(pickerModel)
	if pm.relocate != nil || cmd == nil {
		t.Fatalf("enter on %s: relocate %+v, cmd %v", target, pm.relocate, cmd != nil)
	}
	real, _ := filepath.EvalSymlinks(target)
	if want := (sessionEntry{id: "s1", cwd: real, configDir: cfg}); pm.selected != want {
		t.Errorf("selected %+v, want %+v", pm.selected, want)
	}
	moved := filepath.Join(cfg, "projects", encodeProjectDir(real), "s1.jsonl")
	if _, err := os.Stat(moved); err != nil {
		t.Errorf("the session was not moved: %v", err)
	}
	if got, _, _, _ := readSessionSummary(moved); got != cwd {
		// claude has not written a line in the new place yet; the next
		// resume is what records it, and from then on the new cwd wins
		t.Errorf("moved session's cwd = %q, want the old one %q until claude writes there", got, cwd)
	}
}

func TestPickerRefusesToMoveARunningSession(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	path, cwd := goneSession(t, cfg, "s1")

	m := newPickerModel([]sessionEntry{{id: "s1", cwd: cwd, pid: 4242}})
	updated, _ := m.Update(press(tea.KeyEnter))
	updated, cmd := updated.Update(press(tea.KeyEnter))
	pm := updated.(pickerModel)
	if pm.relocate == nil || pm.relocate.err == nil || !strings.Contains(pm.relocate.err.Error(), "running") || cmd != nil {
		t.Fatalf("relocate %+v, cmd %v", pm.relocate, cmd != nil)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("a running session was moved: %v", err)
	}
}

func TestPickerEnterOnExistingDirectoryResumesStraightAway(t *testing.T) {
	cfg, work := t.TempDir(), t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	makeSession(t, cfg, work, "s1")

	m := newPickerModel([]sessionEntry{{id: "s1", cwd: work}})
	updated, cmd := m.Update(press(tea.KeyEnter))
	if pm := updated.(pickerModel); pm.relocate != nil || cmd == nil || pm.selected.cwd != work {
		t.Errorf("relocate %v, cmd %v, selected %+v", pm.relocate, cmd != nil, pm.selected)
	}
}

func TestCompleteDir(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"alpha", "alpine", "beta", ".hidden", "ほげ", "ほら"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "bravo.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "beta"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	sep := string(filepath.Separator)
	for in, want := range map[string]string{
		root + sep + "al":   root + sep + "alp",
		root + sep + "alph": root + sep + "alpha" + sep,
		root + sep + "b":    root + sep + "beta" + sep, // the file is not offered
		root + sep + "li":   root + sep + "link" + sep,
		root + sep + ".h":   root + sep + ".hidden" + sep,
		root + sep + "ほ":    root + sep + "ほ",
		root + sep + "zz":   root + sep + "zz",
	} {
		if got := completeDir(in); got != want {
			t.Errorf("completeDir(%q) = %q, want %q", in, got, want)
		}
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.Mkdir(filepath.Join(home, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := completeDir("~/pro"); got != "~/projects"+sep {
		t.Errorf("completeDir(~/pro) = %q, want the ~ kept", got)
	}
}

func TestDropLastPathElement(t *testing.T) {
	for in, want := range map[string]string{
		"/a/b/c":  "/a/b/",
		"/a/b/c/": "/a/b/",
		"/a":      "/",
		"abc":     "",
	} {
		if got := dropLastPathElement(in); got != want {
			t.Errorf("dropLastPathElement(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRelocatePromptKeepsTheEndOfALongPath(t *testing.T) {
	r := &relocatePrompt{entry: sessionEntry{cwd: "/tmp/old"}, input: "/very/long/path/that/does/not/fit/the/line/target"}
	got := r.prompt(40)
	if w := ansi.StringWidth(got); w > 40 {
		t.Errorf("prompt is %d columns wide: %q", w, got)
	}
	if !strings.HasSuffix(got, "target▏") || !strings.HasPrefix(got, "resume in: …") {
		t.Errorf("prompt = %q, want the start of the path dropped", got)
	}
	if got := r.prompt(200); !strings.HasPrefix(got, "/tmp/old is gone") {
		t.Errorf("wide prompt = %q", got)
	}
}

func TestHistoryScreenEnterOnGoneDirectoryAsksWhere(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	_, cwd := goneSession(t, cfg, "s1")
	historyFixture(t, historyMessage{sessionID: "s1", role: "user", cwd: cwd, text: "scratch work"})

	m := runSearch(t, typeKeys(t, openHistory(t), "scratch"))
	updated, cmd := m.Update(press(tea.KeyEnter))
	pm := updated.(pickerModel)
	if pm.relocate == nil || cmd != nil {
		t.Fatalf("enter: relocate %v, cmd %v", pm.relocate, cmd != nil)
	}
	screen := ansi.Strip(pm.View().Content)
	if lines := strings.Split(screen, "\n"); !strings.Contains(lines[len(lines)-1], "resume in") {
		t.Errorf("history screen's last line is not the prompt:\n%s", screen)
	}

	target := t.TempDir()
	updated, _ = updated.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	updated = typeKeys(t, updated, target)
	updated, cmd = updated.Update(press(tea.KeyEnter))
	real, _ := filepath.EvalSymlinks(target)
	if pm := updated.(pickerModel); cmd == nil || pm.selected.id != "s1" || pm.selected.cwd != real {
		t.Errorf("selected %+v, cmd %v", pm.selected, cmd != nil)
	}
}

func TestCompleteDirWithAVariableAsTheLastElement(t *testing.T) {
	root := t.TempDir()
	long := filepath.Join(root, "a-much-longer-name")
	for _, d := range []string{long, filepath.Join(long, "inner")} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("D", long)
	t.Setenv("S", "a-much")
	sep := string(filepath.Separator)
	for in, want := range map[string]string{
		// the element is longer expanded than typed: this used to panic
		"$D": long + sep,
		// the element is shorter expanded than typed: this used to cut $S
		root + sep + "$S": long + sep,
		// a $VAR before the last element stays as typed
		"$D" + sep + "in": "$D" + sep + "inner" + sep,
	} {
		if got := completeDir(in); got != want {
			t.Errorf("completeDir(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRelocatePromptAddsNoEllipsisWhenTheShortLabelFits(t *testing.T) {
	r := &relocatePrompt{entry: sessionEntry{cwd: "/a/rather/long/directory/that/is/gone"}, input: "/tmp/new"}
	got := r.prompt(40)
	if got != "resume in: /tmp/new▏" {
		t.Errorf("prompt = %q, want the whole path with nothing marked as cut", got)
	}
	r.input = "/very/long/path/that/does/not/fit/the/line/target"
	if got := r.prompt(40); ansi.StringWidth(got) != 40 {
		t.Errorf("cut prompt = %q is %d columns, want the line filled to 40", got, ansi.StringWidth(got))
	}
}

func TestHistoryScreenRefusesToMoveARunningSession(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	path, cwd := goneSession(t, cfg, "s1")
	writeTranscript(t, filepath.Join(cfg, "sessions", "111.json"), `{"pid":111,"sessionId":"s1"}`)
	orig := isClaudeProcess
	isClaudeProcess = func(pid int) bool { return pid == 111 }
	t.Cleanup(func() { isClaudeProcess = orig })
	historyFixture(t, historyMessage{sessionID: "s1", role: "user", cwd: cwd, text: "scratch work"})

	m := runSearch(t, typeKeys(t, openHistory(t), "scratch"))
	updated, _ := m.Update(press(tea.KeyEnter))
	if pm := updated.(pickerModel); pm.relocate == nil || pm.relocate.entry.pid != 111 {
		t.Fatalf("relocate = %+v, want the running pid carried over", pm.relocate)
	}
	updated, cmd := updated.Update(press(tea.KeyEnter))
	pm := updated.(pickerModel)
	if pm.relocate == nil || pm.relocate.err == nil || !strings.Contains(pm.relocate.err.Error(), "running") || cmd != nil {
		t.Fatalf("relocate %+v, cmd %v", pm.relocate, cmd != nil)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("a running session was moved: %v", err)
	}
}
