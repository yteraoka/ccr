package ccr

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// modalStyle draws the box the opened JSON floats in.
var modalStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	Padding(0, 1)

// jsonlLine is one line of a session file as the viewer lists it: the raw
// bytes, plus the couple of fields worth showing in the index.
type jsonlLine struct {
	number int
	kind   string // the line's "type" field
	time   string // its timestamp in local time, blank when it has none
	raw    []byte
}

// jsonlViewer is the full-screen preview of a session's jsonl. It shows an
// index of the lines, since a single line runs far wider than a terminal,
// and opens the one under the cursor pretty-printed.
type jsonlViewer struct {
	sessionID string
	lines     []jsonlLine
	cursor    int
	top       int

	// detail holds the opened line, pretty-printed and already wrapped to
	// the width it was opened at. It is nil while the index is showing.
	detail     []string
	detailTop  int
	detailLine int

	search incrementalSearch
	// matches are the indices into lines that the filter leaves showing;
	// the cursor and every key walk these, not the whole file.
	matches []int
}

// rows are the lines currently on show: all of them until a filter narrows
// it.
func (v *jsonlViewer) rows() []int {
	if v.matches == nil {
		v.matches = v.search.filter(len(v.lines), v.searchText)
	}
	return v.matches
}

// lineAt returns the line the cursor is on, and whether there is one.
func (v *jsonlViewer) lineAt(cursor int) (jsonlLine, bool) {
	rows := v.rows()
	if cursor < 0 || cursor >= len(rows) {
		return jsonlLine{}, false
	}
	return v.lines[rows[cursor]], true
}

// refilter rebuilds the visible rows after the query changed, putting the
// cursor back at the top: the rows under it are not the ones it was on.
func (v *jsonlViewer) refilter() {
	v.matches = v.search.filter(len(v.lines), v.searchText)
	v.cursor, v.top = 0, 0
}

// searchText is what "/" matches a line against: the whole raw line, so a
// query reaches the type, the timestamp and anything in the payload.
func (v *jsonlViewer) searchText(i int) string { return string(v.lines[i].raw) }

// jsonlProbe is the little that the index needs out of each line. A line
// that fails to parse is still listed: seeing it is the point of a raw
// preview.
type jsonlProbe struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
}

// loadJSONLLines reads a session file into the lines the viewer lists.
func loadJSONLLines(path string) ([]jsonlLine, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var lines []jsonlLine
	for i, chunk := range bytes.Split(data, []byte("\n")) {
		raw := bytes.TrimSpace(chunk)
		if len(raw) == 0 {
			continue
		}
		var probe jsonlProbe
		_ = json.Unmarshal(raw, &probe)

		stamp := ""
		if probe.Timestamp != "" {
			if t, err := time.Parse(time.RFC3339, probe.Timestamp); err == nil {
				stamp = t.Local().Format("15:04:05")
			}
		}
		lines = append(lines, jsonlLine{number: i + 1, kind: probe.Type, time: stamp, raw: raw})
	}
	return lines, nil
}

// newJSONLViewer opens the preview of sessionID's jsonl.
func newJSONLViewer(sessionID string) (*jsonlViewer, error) {
	path, err := findSessionFile(projectsDir(), sessionID)
	if err != nil {
		return nil, err
	}
	lines, err := loadJSONLLines(path)
	if err != nil {
		return nil, err
	}
	return &jsonlViewer{sessionID: sessionID, lines: lines}, nil
}

// openDetail pretty-prints the line under the cursor, wrapped to width.
func (v *jsonlViewer) openDetail(width int) {
	line, ok := v.lineAt(v.cursor)
	if !ok {
		return
	}

	text := string(line.raw)
	var buf bytes.Buffer
	if err := json.Indent(&buf, line.raw, "", "  "); err == nil {
		text = buf.String()
	}

	var out []string
	for _, l := range strings.Split(text, "\n") {
		out = append(out, wrap(l, width)...)
	}
	v.detail, v.detailTop, v.detailLine = out, 0, line.number
}

// update applies a keypress, reporting whether the viewer should close and
// hand the screen back to the picker.
func (v *jsonlViewer) update(key, text string, width, height int) (closed bool) {
	page := height - 2
	if v.detail != nil {
		page = modalContentHeight(height)
	}
	if page < 1 {
		page = 1
	}

	if v.detail != nil {
		switch key {
		case "esc", "q", "i", "left", "h":
			v.detail, v.detailTop = nil, 0
		case "n":
			v.step(1, width, height)
		case "p":
			v.step(-1, width, height)
		case "up", "k":
			v.detailTop--
		case "down", "j":
			v.detailTop++
		case "pgup", "ctrl+u", "b", "backspace":
			v.detailTop -= page
		case "pgdown", "ctrl+d", "space":
			v.detailTop += page
		case "home", "g":
			v.detailTop = 0
		case "end", "G":
			v.detailTop = len(v.detail)
		}
		v.detailTop = clamp(v.detailTop, 0, maxTop(len(v.detail), modalContentHeight(height)))
		return false
	}

	if changed, handled := v.search.key(key, text); handled {
		if changed {
			v.refilter()
		}
		return false
	}

	switch key {
	case "esc", "ctrl+c":
		// esc drops the filter first, and only then leaves the viewer
		if v.search.filtering() {
			v.search.clear()
			v.refilter()
			return false
		}
		return true
	case "q":
		return true
	case "/":
		v.search.begin()
	case "i", "enter", "right", "l":
		v.openDetail(modalContentWidth(width, height))
	// n and p step a line here too, so they mean the same on both screens
	case "up", "k", "p":
		v.cursor--
	case "down", "j", "n":
		v.cursor++
	case "pgup", "ctrl+u", "b", "backspace":
		v.cursor -= page
	case "pgdown", "ctrl+d", "space":
		v.cursor += page
	case "home", "g":
		v.cursor = 0
	case "end", "G":
		v.cursor = len(v.rows()) - 1
	}
	v.cursor = clamp(v.cursor, 0, len(v.rows())-1)
	v.top = scrollTo(v.top, v.cursor, height-2)
	return false
}

// step moves to the line delta away and shows it, so the reader can walk
// the file without closing and reopening the modal at every line.
func (v *jsonlViewer) step(delta, width, height int) {
	next := clamp(v.cursor+delta, 0, len(v.rows())-1)
	if next == v.cursor {
		return
	}
	v.cursor = next
	// keep the list underneath in step, so it is showing the right line when
	// the modal closes
	v.top = scrollTo(v.top, v.cursor, height-2)
	v.openDetail(modalContentWidth(width, height))
}

func clamp(n, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}

// maxTop is the furthest a window of the given height can be scrolled
// through count rows without running off the end.
func maxTop(count, height int) int {
	if count <= height || height < 1 {
		return 0
	}
	return count - height
}

// scrollTo nudges the window so the cursor stays inside it.
func scrollTo(top, cursor, height int) int {
	if height < 1 {
		return cursor
	}
	if cursor < top {
		return cursor
	}
	if cursor >= top+height {
		return cursor - height + 1
	}
	return top
}

const jsonlIndexLegend = "j/k/n/p: move  space/b: page  /: filter  i/enter: JSON  q: back"
const jsonlDetailLegend = "n/p: next/prev   j/k: scroll   space/b: page   q/esc: close"

// view renders the index, with the opened line floating over it as a modal
// when there is one. Keeping the list underneath means opening a line never
// loses your place in the file.
func (v *jsonlViewer) view(width, height int) string {
	index := v.indexView(width, height)
	if v.detail == nil {
		return index
	}
	// Dim the list while the modal is up, so it reads as the background it
	// now is. Its own styling is dropped first: the highlighted row would
	// otherwise keep drawing the eye from behind the box.
	index = legendStyle.Render(ansi.Strip(index))
	box := v.modalBox(width, height)
	boxWidth, boxHeight := lipgloss.Width(box), lipgloss.Height(box)
	return lipgloss.NewCompositor(
		lipgloss.NewLayer(index),
		lipgloss.NewLayer(box).X(max(0, (width-boxWidth)/2)).Y(max(0, (height-boxHeight)/2)).Z(1),
	).Render()
}

// The modal leaves a margin of list visible around it, so it reads as
// something laid over the file rather than another screen.
const (
	modalHMargin  = 4 // columns of list left visible on each side
	modalVMargin  = 2 // rows of it above and below
	modalMaxWidth = 110
	modalChrome   = 4 // the box's own borders and padding
)

// modalContentWidth is how wide the JSON may be inside the modal.
func modalContentWidth(width, height int) int {
	return max(8, modalWidth(width)-modalChrome)
}

func modalWidth(width int) int {
	w := width - modalHMargin*2
	if w > modalMaxWidth {
		w = modalMaxWidth
	}
	return max(12, w)
}

// modalContentHeight is how many rows of JSON the modal shows: its own
// height less the borders, the title and the legend inside it.
func modalContentHeight(height int) int {
	return max(1, height-modalVMargin*2-4)
}

func (v *jsonlViewer) modalBox(width, height int) string {
	rows := modalContentHeight(height)
	end := v.detailTop + rows
	if end > len(v.detail) {
		end = len(v.detail)
	}
	body := make([]string, 0, rows)
	for i := v.detailTop; i < end; i++ {
		body = append(body, v.detail[i])
	}
	for len(body) < rows {
		body = append(body, "")
	}

	inner := modalContentWidth(width, height)
	title := truncate(fmt.Sprintf("line %d of %d", v.detailLine, len(v.lines)), inner)
	if more := len(v.detail) - end; more > 0 {
		title = truncate(fmt.Sprintf("line %d of %d   (%d more rows below)", v.detailLine, len(v.lines), more), inner)
	}

	content := headerRowStyle.Render(title) + "\n" +
		strings.Join(body, "\n") + "\n" +
		legendStyle.Render(truncate(jsonlDetailLegend, inner))

	return modalStyle.Width(modalWidth(width)).Render(content)
}

func (v *jsonlViewer) indexView(width, height int) string {
	header := headerRowStyle.Render(truncate(formatJSONLRow("LINE", "TYPE", "TIME", "CONTENT"), width))
	legend := legendStyle.Render(truncate(jsonlIndexLegend, width))
	rows := v.rows()
	if v.search.showsPrompt() {
		legend = truncate(v.search.prompt(len(rows), len(v.lines)), width)
	}

	visible := height - 2 // header line + legend line
	if visible < 0 {
		visible = 0
	}
	if len(rows) == 0 {
		body := make([]string, visible)
		if visible > 0 {
			message := "(no lines in this session file)"
			if v.search.filtering() {
				message = "(no line matches " + v.search.query + ")"
			}
			body[0] = truncate(message, width)
		}
		return strings.Join(append(append([]string{header}, body...), legend), "\n")
	}

	end := v.top + visible
	if end > len(rows) {
		end = len(rows)
	}
	lines := make([]string, 0, visible)
	for i := v.top; i < end; i++ {
		l := v.lines[rows[i]]
		row := truncate(formatJSONLRow(fmt.Sprintf("%d", l.number), l.kind, l.time, string(l.raw)), width)
		if i == v.cursor {
			row = selectedRowStyle.Render(row)
		}
		lines = append(lines, row)
	}
	for len(lines) < visible {
		lines = append(lines, "")
	}
	return strings.Join(append(append([]string{header}, lines...), legend), "\n")
}

func formatJSONLRow(number, kind, stamp, content string) string {
	return fmt.Sprintf("%6s  %-22s  %8s  %s", number, kind, stamp, content)
}
