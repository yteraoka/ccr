package ccr

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// historyResultLimit is how many matches the history screen asks for.
const historyResultLimit = 100

const historyLegend = "j/k/n/p: move  space/b: page  enter: resume  i: jsonl  v: browser  s or /: search  q: back"

// historyView is the picker's history search screen (s): a query prompt,
// the prompts and answers matching it, and the one under the cursor in
// full. It stays on the picker once opened, so going back to it shows the
// last search again.
type historyView struct {
	open   bool
	typing bool
	query  string
	// global searches every project, as -g does for the session list;
	// otherwise only messages recorded in the current directory.
	global bool

	backend *historyBackend
	// searching is set while a search runs in the background; seq tells
	// its results from those of a search started before it.
	searching bool
	seq       int

	searched bool // a search has finished, so results is an answer
	results  []historyMessage
	terms    [][]rune // what the results' text is highlighted by
	cursor   int
	top      int
	status   string
	// gone caches, per session id, whether its transcript has been deleted
	gone map[string]bool
}

// historyResultsMsg carries a finished search back to the model.
type historyResultsMsg struct {
	seq     int
	query   string
	results []historyMessage
	terms   [][]rune
	mode    string
	warning error
	err     error
}

func newHistoryView(global bool) *historyView {
	return &historyView{open: true, typing: true, global: global}
}

// start begins searching for the typed query in the background, returning
// the command that runs it (nil if there is nothing to search for or a
// search is already running).
func (v *historyView) start() tea.Cmd {
	query := strings.TrimSpace(v.query)
	if query == "" || v.searching {
		return nil
	}
	if v.backend == nil {
		b, err := openHistoryBackend()
		if err != nil {
			v.status = "error: " + err.Error()
			return nil
		}
		v.backend = b
	}
	mode, err := v.backend.resolveMode("")
	if err != nil {
		v.status = "error: " + err.Error()
		return nil
	}
	req := historyRequest{query: query, mode: mode, limit: historyResultLimit}
	if !v.global {
		if req.filter.cwd, err = os.Getwd(); err != nil {
			v.status = "error: " + err.Error()
			return nil
		}
	}

	v.typing, v.searching = false, true
	v.seq++
	v.status = "searching…"
	seq, b := v.seq, v.backend
	return func() tea.Msg {
		// the backend is touched only here while searching is set, and
		// nothing may be printed: the screen belongs to the TUI
		results, warning, err := b.search(context.Background(), req, nil)
		return historyResultsMsg{seq: seq, query: query, results: results, terms: searchTerms(query),
			mode: mode, warning: warning, err: err}
	}
}

// finish takes in a search's results.
func (v *historyView) finish(msg historyResultsMsg) {
	if msg.seq != v.seq {
		return
	}
	v.searching = false
	if msg.err != nil {
		v.status = "error: " + msg.err.Error()
		return
	}
	v.searched = true
	v.results, v.terms = msg.results, msg.terms
	v.cursor, v.top = 0, 0
	switch {
	case msg.warning != nil:
		v.status = fmt.Sprintf("%d results (keyword only: %v)", len(msg.results), msg.warning)
	default:
		v.status = fmt.Sprintf("%d results for %q (%s)", len(msg.results), msg.query, msg.mode)
	}
}

// transcriptGone reports whether r's session no longer has a transcript,
// remembering the answer: the preview asks on every redraw.
func (v *historyView) transcriptGone(r historyMessage) bool {
	gone, ok := v.gone[r.sessionID]
	if !ok {
		gone = !transcriptExists(r.sessionID, r.configDir)
		if v.gone == nil {
			v.gone = make(map[string]bool)
		}
		v.gone[r.sessionID] = gone
	}
	return gone
}

// current is the message under the cursor, if there is one.
func (v *historyView) current() (historyMessage, bool) {
	if v.cursor < 0 || v.cursor >= len(v.results) {
		return historyMessage{}, false
	}
	return v.results[v.cursor], true
}

func (v *historyView) move(delta, rows int) {
	if len(v.results) == 0 {
		return
	}
	v.cursor = clamp(v.cursor+delta, 0, len(v.results)-1)
	v.top = scrollTo(v.top, v.cursor, rows)
}

// historyPaneHeights splits the screen between the result list (header,
// rows and legend) and the full message under it, leaving a line for the
// separator and one for the footer. The message gets the larger share:
// it is what a search is for.
func historyPaneHeights(height int) (list, preview int) {
	list = max(4, (height-2)*2/5)
	return list, max(1, height-2-list)
}

// listRows is how many results the list shows at once.
func historyListRows(height int) int {
	list, _ := historyPaneHeights(height)
	return max(1, list-2)
}

// updateHistory applies a key to the history screen.
func (m pickerModel) updateHistory(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	v := m.history
	key, text := msg.String(), msg.Text
	rows := historyListRows(m.viewHeight())

	if v.typing {
		switch key {
		case "esc", "ctrl+c":
			if !v.searched {
				v.open = false // nothing to go back to
			}
			v.typing = false
			return m, nil
		case "enter":
			return m, v.start()
		case "backspace":
			if q := []rune(v.query); len(q) > 0 {
				v.query = string(q[:len(q)-1])
			}
			return m, nil
		}
		if text != "" {
			v.query += text
			return m, nil
		}
		// keys that type nothing (arrows, page keys) still move
	}

	switch key {
	case "q", "esc":
		v.open = false
	case "ctrl+c":
		return m, tea.Quit
	case "s", "/":
		v.typing = true
	case "up", "k", "p":
		v.move(-1, rows)
	case "down", "j", "n":
		v.move(1, rows)
	case "pgup", "ctrl+u", "b", "backspace":
		v.move(-rows, rows)
	case "pgdown", "ctrl+d", "space":
		v.move(rows, rows)
	case "home", "g":
		v.move(-len(v.results), rows)
	case "end", "G":
		v.move(len(v.results), rows)
	case "enter":
		if r, ok := v.current(); ok {
			// claude cannot resume a session without its transcript, and
			// would only say so after ccr has handed the terminal over
			if v.transcriptGone(r) {
				v.status = "this session's transcript is gone, so it cannot be resumed (v shows what the history kept)"
				return m, nil
			}
			m.selected = sessionEntry{id: r.sessionID, cwd: r.cwd, configDir: r.configDir}
			return m, tea.Quit
		}
	case "i":
		if r, ok := v.current(); ok {
			if v.transcriptGone(r) {
				v.status = "this session's transcript is gone (v shows what the history kept)"
				return m, nil
			}
			viewer, err := newJSONLViewer(r.sessionID)
			if err != nil {
				v.status = "error: " + err.Error()
			} else {
				m.viewer = viewer
			}
		}
	case "v":
		if r, ok := v.current(); ok {
			rememberConfigDir(r.configDir)
			_, v.status = m.openTranscript(r.sessionID)
		}
	}
	return m, nil
}

// view draws the history screen.
func (v *historyView) view(width, height int) string {
	listHeight, previewHeight := historyPaneHeights(height)
	return v.listView(width, listHeight) + "\n" +
		strings.Repeat("─", width) + "\n" +
		v.messageView(width, previewHeight) + "\n" +
		truncate(v.footer(), width)
}

func (v *historyView) footer() string {
	if v.typing {
		return "search history: " + v.query + "▏"
	}
	return v.status
}

func formatHistoryRow(stamp, who, cwd, text string) string {
	return fmt.Sprintf("%-16s  %-6s  %-14s  %s", stamp, who, cwd, text)
}

func (v *historyView) listView(width, height int) string {
	header := headerRowStyle.Render(truncate(formatHistoryRow("TIME", "WHO", "CWD", "MESSAGE"), width))
	legend := legendStyle.Render(truncate(historyLegend, width))
	visible := max(0, height-2)

	lines := make([]string, 0, visible)
	if len(v.results) == 0 && visible > 0 {
		message := `type words to look for and press enter (all must match; "quote" a phrase)`
		if v.searched {
			message = "(no matches)"
		}
		lines = append(lines, truncate(message, width))
	}
	end := min(v.top+visible, len(v.results))
	for i := v.top; i < end; i++ {
		r := v.results[i]
		selected := i == v.cursor
		row := formatHistoryRow(r.createdAt.Local().Format("2006-01-02 15:04"), roleLabel(r.role),
			truncate(cwdBase(r.cwd), 14), snippet(r.text, v.terms, width, !selected))
		row = ansi.Truncate(row, width, "…")
		if selected {
			row = selectedRowStyle.Render(row)
		}
		lines = append(lines, row)
	}
	for len(lines) < visible {
		lines = append(lines, "")
	}
	return strings.Join(append(append([]string{header}, lines...), legend), "\n")
}

// messageView shows the message under the cursor in full, wrapped, and
// starting a line above its first match so the reason it was found is in
// view even in a long answer.
func (v *historyView) messageView(width, height int) string {
	r, ok := v.current()
	if !ok {
		return strings.Repeat("\n", max(0, height-1))
	}
	lines := []string{
		truncate("Session: "+r.sessionID, width),
		truncate("Directory: "+r.cwd, width),
		truncate(fmt.Sprintf("%s  %s", r.createdAt.Local().Format("2006-01-02 15:04:05"), roleLabel(r.role)), width),
	}
	if v.transcriptGone(r) {
		lines = append(lines, truncate("Transcript: gone (v shows what the history kept)", width))
	}
	lines = append(lines, "")

	var body []string
	firstHit := -1
	for _, l := range strings.Split(r.text, "\n") {
		for _, wl := range wrap(l, width) {
			src := []rune(wl)
			hit, first := markTerms(src, v.terms)
			if first >= 0 && firstHit < 0 {
				firstHit = len(body)
			}
			body = append(body, renderHits(src, hit, true))
		}
	}
	room := height - len(lines)
	start := 0
	if firstHit > 0 && len(body) > room {
		start = min(firstHit-1, len(body)-room)
	}
	lines = append(lines, body[start:]...)
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func roleLabel(role string) string {
	if role == "assistant" {
		return "claude"
	}
	return "you"
}

func cwdBase(cwd string) string {
	if cwd == "" {
		return ""
	}
	return filepath.Base(cwd)
}
