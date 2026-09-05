package ccr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func writeJSONLFixture(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadJSONLLinesKeepsEveryLineIncludingUnparseableOnes(t *testing.T) {
	path := writeJSONLFixture(t,
		`{"type":"user","timestamp":"2026-07-24T13:32:51Z","message":{"content":"hi"}}`,
		``,
		`not json at all`,
		`{"type":"assistant"}`,
	)

	lines, err := loadJSONLLines(path)
	if err != nil {
		t.Fatalf("loadJSONLLines: %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3 (blank dropped, unparseable kept): %+v", len(lines), lines)
	}

	// the line numbers are the file's, so they still point at the real line
	if lines[0].number != 1 || lines[1].number != 3 || lines[2].number != 4 {
		t.Errorf("line numbers = %d/%d/%d, want 1/3/4", lines[0].number, lines[1].number, lines[2].number)
	}
	if lines[0].kind != "user" || lines[0].time == "" {
		t.Errorf("first line = %+v, want its type and timestamp", lines[0])
	}
	// a line that is not JSON is still listed: seeing it is the point
	if lines[1].kind != "" || string(lines[1].raw) != "not json at all" {
		t.Errorf("unparseable line = %+v, want it listed verbatim", lines[1])
	}
	// no timestamp means no time shown, rather than a zero one
	if lines[2].time != "" {
		t.Errorf("line without a timestamp shows %q, want blank", lines[2].time)
	}
}

func TestJSONLViewerIndexListsLinesAndMovesTheCursor(t *testing.T) {
	path := writeJSONLFixture(t,
		`{"type":"user","message":{"content":"one"}}`,
		`{"type":"assistant","message":{"content":"two"}}`,
	)
	lines, err := loadJSONLLines(path)
	if err != nil {
		t.Fatal(err)
	}
	v := &jsonlViewer{sessionID: "s1", lines: lines}

	got := ansi.Strip(v.view(100, 10))
	for _, want := range []string{"LINE", "TYPE", "CONTENT", "user", "assistant", "i/enter: JSON", "space/b: page", "j/k/n/p: move", "/: filter"} {
		if !strings.Contains(got, want) {
			t.Errorf("index view missing %q:\n%s", want, got)
		}
	}

	if v.update("down", "", 100, 10); v.cursor != 1 {
		t.Errorf("cursor after down = %d, want 1", v.cursor)
	}
	// and it stops at the ends rather than running off them
	v.update("down", "", 100, 10)
	if v.cursor != 1 {
		t.Errorf("cursor past the last line = %d, want it clamped to 1", v.cursor)
	}
	v.update("home", "", 100, 10)
	if v.cursor != 0 {
		t.Errorf("cursor after home = %d, want 0", v.cursor)
	}
}

func TestJSONLViewerOpensTheLinePrettyPrinted(t *testing.T) {
	path := writeJSONLFixture(t, `{"type":"user","message":{"content":"hi"}}`)
	lines, err := loadJSONLLines(path)
	if err != nil {
		t.Fatal(err)
	}
	v := &jsonlViewer{sessionID: "s1", lines: lines}

	v.update("i", "", 100, 20)
	if v.detail == nil {
		t.Fatal("i did not open the line")
	}
	got := ansi.Strip(v.view(100, 20))
	if !strings.Contains(got, `"type": "user"`) {
		t.Errorf("the modal is not pretty-printed:\n%s", got)
	}
	if !strings.Contains(got, "line 1 of 1") {
		t.Errorf("the modal does not say which line it is:\n%s", got)
	}
	// it floats over the list rather than replacing it: the box is drawn,
	// and the index is still there around it
	if !strings.Contains(got, "╭") || !strings.Contains(got, "╰") {
		t.Errorf("the modal has no box drawn around it:\n%s", got)
	}
	if !strings.Contains(got, "LINE") {
		t.Errorf("the list should stay visible behind the modal:\n%s", got)
	}

	// esc closes the modal, and only a second one leaves the viewer
	if closed := v.update("esc", "", 100, 20); closed {
		t.Error("esc should close the modal, not the viewer")
	}
	if v.detail != nil {
		t.Error("esc did not close the modal")
	}
	if closed := v.update("esc", "", 100, 20); !closed {
		t.Error("esc in the index should close the viewer")
	}

	// and the key that opened it closes it again
	v.update("i", "", 100, 20)
	v.update("i", "", 100, 20)
	if v.detail != nil {
		t.Error("i should toggle the modal shut again")
	}
}

func TestJSONLViewerShowsUnparseableLineVerbatim(t *testing.T) {
	path := writeJSONLFixture(t, `{ broken`)
	lines, err := loadJSONLLines(path)
	if err != nil {
		t.Fatal(err)
	}
	v := &jsonlViewer{sessionID: "s1", lines: lines}
	v.update("i", "", 100, 20)

	got := ansi.Strip(v.view(100, 20))
	if !strings.Contains(got, "{ broken") {
		t.Errorf("a line that is not JSON should still be shown as it is:\n%s", got)
	}
}

func TestJSONLViewerEmptyFile(t *testing.T) {
	path := writeJSONLFixture(t, ``)
	lines, err := loadJSONLLines(path)
	if err != nil {
		t.Fatal(err)
	}
	v := &jsonlViewer{sessionID: "s1", lines: lines}

	got := ansi.Strip(v.view(60, 8))
	if !strings.Contains(got, "no lines") {
		t.Errorf("empty file view = %q, want it to say so", got)
	}
	// and it must not panic on the keys that move a cursor there is none of
	v.update("down", "", 60, 8)
	v.update("i", "", 60, 8)
	if v.detail != nil {
		t.Error("there is no line to open in an empty file")
	}
}

func TestScrollToKeepsTheCursorInView(t *testing.T) {
	cases := []struct{ top, cursor, height, want int }{
		{top: 0, cursor: 0, height: 5, want: 0},
		{top: 0, cursor: 4, height: 5, want: 0},  // still the last visible row
		{top: 0, cursor: 5, height: 5, want: 1},  // one past it scrolls by one
		{top: 10, cursor: 3, height: 5, want: 3}, // jumping back scrolls up to it
	}
	for _, c := range cases {
		if got := scrollTo(c.top, c.cursor, c.height); got != c.want {
			t.Errorf("scrollTo(top=%d, cursor=%d, height=%d) = %d, want %d", c.top, c.cursor, c.height, got, c.want)
		}
	}
}

func TestMaxTopStopsAtTheEnd(t *testing.T) {
	if got := maxTop(3, 10); got != 0 {
		t.Errorf("maxTop(fits) = %d, want 0", got)
	}
	if got := maxTop(30, 10); got != 20 {
		t.Errorf("maxTop(30, 10) = %d, want 20", got)
	}
}

func TestModalLeavesTheListVisibleAroundIt(t *testing.T) {
	const width, height = 100, 24
	if got := modalWidth(width); got >= width {
		t.Errorf("modalWidth(%d) = %d, want it narrower than the terminal", width, got)
	}
	// the JSON gets the box less its borders and padding
	if got, want := modalContentWidth(width, height), modalWidth(width)-modalChrome; got != want {
		t.Errorf("modalContentWidth = %d, want %d", got, want)
	}
	// and the box less its borders, title and legend
	if got := modalContentHeight(height); got >= height {
		t.Errorf("modalContentHeight(%d) = %d, want it shorter than the terminal", height, got)
	}

	// a terminal too small for the margins still yields a usable box
	if got := modalWidth(6); got < 12 {
		t.Errorf("modalWidth(6) = %d, want a minimum usable width", got)
	}
	if got := modalContentHeight(3); got < 1 {
		t.Errorf("modalContentHeight(3) = %d, want at least one row", got)
	}
}

func TestModalScrollsWithinItsOwnHeight(t *testing.T) {
	path := writeJSONLFixture(t, `{"type":"user","message":{"content":"hi"},"a":1,"b":2,"c":3,"d":4,"e":5,"f":6}`)
	lines, err := loadJSONLLines(path)
	if err != nil {
		t.Fatal(err)
	}
	v := &jsonlViewer{sessionID: "s1", lines: lines}
	v.update("i", "", 60, 12)
	if len(v.detail) == 0 {
		t.Fatal("the modal has no content")
	}

	v.update("end", "", 60, 12)
	if v.detailTop != maxTop(len(v.detail), modalContentHeight(12)) {
		t.Errorf("detailTop after end = %d, want the last page of the modal", v.detailTop)
	}
	v.update("down", "", 60, 12)
	if v.detailTop != maxTop(len(v.detail), modalContentHeight(12)) {
		t.Error("scrolling past the end should stop there")
	}
	v.update("home", "", 60, 12)
	if v.detailTop != 0 {
		t.Errorf("detailTop after home = %d, want 0", v.detailTop)
	}
}

// Enter resumes a session on the picker's list; inside the viewer it is
// free, and opens the JSON alongside i.
func TestJSONLViewerEnterOpensButNeverQuits(t *testing.T) {
	path := writeJSONLFixture(t, `{"type":"user","message":{"content":"hi"}}`)
	lines, err := loadJSONLLines(path)
	if err != nil {
		t.Fatal(err)
	}
	v := &jsonlViewer{sessionID: "s1", lines: lines}

	if closed := v.update("enter", "", 100, 20); closed {
		t.Error("enter should never close the viewer")
	}
	if v.detail == nil {
		t.Error("enter should open the JSON, like i")
	}

	// and it does not put it away again: closing is q/esc/i
	if closed := v.update("enter", "", 100, 20); closed {
		t.Error("enter should never close the viewer")
	}
	if v.detail == nil {
		t.Error("enter should not close the modal; q, esc and i do that")
	}
}

// Walking the file from inside the modal saves closing and reopening it at
// every line.
func TestModalStepsThroughLinesWithNAndP(t *testing.T) {
	path := writeJSONLFixture(t,
		`{"type":"user","message":{"content":"one"}}`,
		`{"type":"assistant","message":{"content":"two"}}`,
		`{"type":"user","message":{"content":"three"}}`,
	)
	lines, err := loadJSONLLines(path)
	if err != nil {
		t.Fatal(err)
	}
	v := &jsonlViewer{sessionID: "s1", lines: lines}
	v.update("i", "", 100, 20)

	shows := func(want string) bool { return strings.Contains(ansi.Strip(v.view(100, 20)), want) }
	if !shows(`"content": "one"`) {
		t.Fatalf("the modal did not open on the first line:\n%s", ansi.Strip(v.view(100, 20)))
	}

	v.update("n", "", 100, 20)
	if v.cursor != 1 || !shows(`"content": "two"`) {
		t.Errorf("n did not move to the next line (cursor=%d)", v.cursor)
	}
	if v.detail == nil {
		t.Error("n should keep the modal open")
	}
	if !shows("line 2 of 3") {
		t.Error("the modal title should follow the line it is showing")
	}

	v.update("p", "", 100, 20)
	if v.cursor != 0 || !shows(`"content": "one"`) {
		t.Errorf("p did not move back (cursor=%d)", v.cursor)
	}

	// and they stop at the ends rather than wrapping or panicking
	v.update("p", "", 100, 20)
	if v.cursor != 0 {
		t.Errorf("p at the first line = %d, want it to stay", v.cursor)
	}
	for i := 0; i < 5; i++ {
		v.update("n", "", 100, 20)
	}
	if v.cursor != 2 {
		t.Errorf("n past the last line = %d, want it clamped", v.cursor)
	}
}

// Stepping scrolls the list underneath, so it is showing the right line
// when the modal closes.
func TestModalStepKeepsTheListInSync(t *testing.T) {
	var raw []string
	for i := 0; i < 40; i++ {
		raw = append(raw, `{"type":"user","message":{"content":"line"}}`)
	}
	lines, err := loadJSONLLines(writeJSONLFixture(t, raw...))
	if err != nil {
		t.Fatal(err)
	}
	v := &jsonlViewer{sessionID: "s1", lines: lines}
	v.update("i", "", 100, 12)
	for i := 0; i < 30; i++ {
		v.update("n", "", 100, 12)
	}
	v.update("esc", "", 100, 12)

	if v.cursor != 30 {
		t.Fatalf("cursor = %d, want 30", v.cursor)
	}
	rows := 12 - 2
	if v.top > v.cursor || v.cursor >= v.top+rows {
		t.Errorf("list window top=%d does not contain the cursor at %d", v.top, v.cursor)
	}
}

func TestModalPagingKeys(t *testing.T) {
	var raw []string
	for i := 0; i < 3; i++ {
		raw = append(raw, `{"type":"user","a":1,"b":2,"c":3,"d":4,"e":5,"f":6,"g":7,"h":8,"i":9,"j":10}`)
	}
	lines, err := loadJSONLLines(writeJSONLFixture(t, raw...))
	if err != nil {
		t.Fatal(err)
	}
	const width, height = 80, 14
	page := modalContentHeight(height)

	v := &jsonlViewer{sessionID: "s1", lines: lines}
	v.update("i", "", width, height)
	if len(v.detail) <= page {
		t.Fatalf("the fixture is not long enough to page: %d rows in a %d-row modal", len(v.detail), page)
	}

	// space pages down; b and backspace page back up
	v.update("space", "", width, height)
	if v.detailTop != page {
		t.Errorf("detailTop after space = %d, want %d", v.detailTop, page)
	}
	v.update("b", "", width, height)
	if v.detailTop != 0 {
		t.Errorf("detailTop after b = %d, want 0", v.detailTop)
	}
	v.update("space", "", width, height)
	v.update("backspace", "", width, height)
	if v.detailTop != 0 {
		t.Errorf("detailTop after backspace = %d, want 0", v.detailTop)
	}
	// backspace pages rather than closing the modal
	if v.detail == nil {
		t.Error("backspace should page up, not close the modal")
	}

	// they agree with the pgup/pgdown keys they stand in for
	v.update("pgdown", "", width, height)
	afterPgDown := v.detailTop
	v.update("pgup", "", width, height)
	v.update("space", "", width, height)
	if v.detailTop != afterPgDown {
		t.Errorf("space moved to %d but pgdown moved to %d", v.detailTop, afterPgDown)
	}
}

// The index pages with the same keys as the modal, so moving between the
// two screens does not change what space does.
//
// The key name for the space bar is "space"; " " never arrives, so a
// binding written that way would silently do nothing.
func TestIndexPagingKeys(t *testing.T) {
	var raw []string
	for i := 0; i < 60; i++ {
		raw = append(raw, `{"type":"user","message":{"content":"hi"}}`)
	}
	lines, err := loadJSONLLines(writeJSONLFixture(t, raw...))
	if err != nil {
		t.Fatal(err)
	}
	const width, height = 80, 14
	page := height - 2

	v := &jsonlViewer{sessionID: "s1", lines: lines}
	v.update("space", "", width, height)
	if v.cursor != page {
		t.Errorf("cursor after space = %d, want a page down (%d)", v.cursor, page)
	}
	// and it pages rather than opening the JSON, which i and enter do
	if v.detail != nil {
		t.Error("space should page the list, not open the modal")
	}

	v.update("b", "", width, height)
	if v.cursor != 0 {
		t.Errorf("cursor after b = %d, want back at the top", v.cursor)
	}
	v.update("space", "", width, height)
	v.update("backspace", "", width, height)
	if v.cursor != 0 {
		t.Errorf("cursor after backspace = %d, want back at the top", v.cursor)
	}

	// they agree with the pgup/pgdown keys they stand in for
	v.update("pgdown", "", width, height)
	afterPgDown := v.cursor
	v.update("pgup", "", width, height)
	v.update("space", "", width, height)
	if v.cursor != afterPgDown {
		t.Errorf("space moved to %d but pgdown moved to %d", v.cursor, afterPgDown)
	}
}

// n and p step a line in the modal, so they do the same on the list it
// opens from rather than meaning something else there.
func TestIndexStepsWithNAndP(t *testing.T) {
	var raw []string
	for i := 0; i < 5; i++ {
		raw = append(raw, `{"type":"user","message":{"content":"hi"}}`)
	}
	lines, err := loadJSONLLines(writeJSONLFixture(t, raw...))
	if err != nil {
		t.Fatal(err)
	}
	v := &jsonlViewer{sessionID: "s1", lines: lines}

	v.update("n", "", 80, 20)
	if v.cursor != 1 {
		t.Errorf("cursor after n = %d, want 1 (the same as j)", v.cursor)
	}
	v.update("n", "", 80, 20)
	v.update("p", "", 80, 20)
	if v.cursor != 1 {
		t.Errorf("cursor after p = %d, want 1 (the same as k)", v.cursor)
	}

	// they move exactly as far as j and k do
	withNP := &jsonlViewer{sessionID: "s1", lines: lines}
	withJK := &jsonlViewer{sessionID: "s1", lines: lines}
	for _, k := range []string{"n", "n", "n", "p"} {
		withNP.update(k, "", 80, 20)
	}
	for _, k := range []string{"j", "j", "j", "k"} {
		withJK.update(k, "", 80, 20)
	}
	if withNP.cursor != withJK.cursor {
		t.Errorf("n/p reached %d but j/k reached %d", withNP.cursor, withJK.cursor)
	}

	// and neither opens the JSON: i and enter do that
	if withNP.detail != nil {
		t.Error("n/p should move the cursor, not open the modal")
	}
}

func TestJSONLIndexFiltersToMatches(t *testing.T) {
	lines, err := loadJSONLLines(writeJSONLFixture(t,
		`{"type":"user","message":{"content":"hello"}}`,
		`{"type":"assistant","message":{"content":"world"}}`,
		`{"type":"assistant","message":{"content":"needle in here"}}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	v := &jsonlViewer{sessionID: "s1", lines: lines}

	v.update("/", "/", 100, 20)
	for _, r := range "needle" {
		v.update(string(r), string(r), 100, 20)
	}

	// only the matching line is left, and the cursor is on it
	if got := len(v.rows()); got != 1 {
		t.Errorf("%d rows showing, want just the match", got)
	}
	line, ok := v.lineAt(v.cursor)
	if !ok || line.number != 3 {
		t.Errorf("cursor is on line %+v, want the third line of the file", line)
	}

	got := ansi.Strip(v.view(100, 20))
	if !strings.Contains(got, "needle") {
		t.Errorf("view = %q, want the matching line", got)
	}
	if strings.Contains(got, "world") || strings.Contains(got, "hello") {
		t.Errorf("view = %q, want the lines that do not match hidden", got)
	}
	if !strings.Contains(got, "/needle") || !strings.Contains(got, "1/3") {
		t.Errorf("view = %q, want the prompt to say how much is showing", got)
	}

	// enter keeps the list narrowed and hands the keys back
	v.update("enter", "", 100, 20)
	if v.search.typing || !v.search.filtering() {
		t.Error("enter should stop the typing but keep the filter")
	}
	if got := len(v.rows()); got != 1 {
		t.Errorf("%d rows after enter, want the filter still on", got)
	}
	// opening the JSON now works on the filtered line
	v.update("i", "", 100, 20)
	if v.detail == nil {
		t.Error("i should open the JSON once the query has been accepted")
	}
	v.update("i", "", 100, 20)

	// esc drops the filter rather than leaving the viewer
	if closed := v.update("esc", "", 100, 20); closed {
		t.Error("esc should clear the filter first, not close the viewer")
	}
	if v.search.filtering() || len(v.rows()) != 3 {
		t.Errorf("esc left %d rows and filtering=%v, want the whole file back", len(v.rows()), v.search.filtering())
	}
	// and a second esc does leave
	if closed := v.update("esc", "", 100, 20); !closed {
		t.Error("esc with no filter should close the viewer")
	}
}

func TestJSONLIndexSaysWhenNothingMatches(t *testing.T) {
	lines, err := loadJSONLLines(writeJSONLFixture(t, `{"type":"user","message":{"content":"hi"}}`))
	if err != nil {
		t.Fatal(err)
	}
	v := &jsonlViewer{sessionID: "s1", lines: lines}
	v.update("/", "/", 100, 20)
	for _, r := range "zzz" {
		v.update(string(r), string(r), 100, 20)
	}

	if got := len(v.rows()); got != 0 {
		t.Fatalf("%d rows showing, want none", got)
	}
	got := ansi.Strip(v.view(100, 20))
	if !strings.Contains(got, "no line matches zzz") {
		t.Errorf("view = %q, want it to say nothing matched", got)
	}
	// and nothing to open, without panicking on the way
	v.update("i", "", 100, 20)
	if v.detail != nil {
		t.Error("there is no line to open when nothing matches")
	}
}

func TestJSONLIndexSearchTakesTypedKeysNotCommands(t *testing.T) {
	lines, err := loadJSONLLines(writeJSONLFixture(t, `{"type":"user","message":{"content":"quiet"}}`))
	if err != nil {
		t.Fatal(err)
	}
	v := &jsonlViewer{sessionID: "s1", lines: lines}
	v.update("/", "/", 100, 20)

	// i, q and n are commands on the list, but text while typing a query
	for _, r := range "qui" {
		v.update(string(r), string(r), 100, 20)
	}
	if v.detail != nil {
		t.Error("i should be typed into the query, not open the modal")
	}
	if v.search.query != "qui" {
		t.Errorf("query = %q, want %q", v.search.query, "qui")
	}
	if closed := v.update("e", "e", 100, 20); closed {
		t.Error("typing must not close the viewer")
	}
	if got := len(v.rows()); got != 1 {
		t.Errorf("%d rows showing, want the line matching %q", got, v.search.query)
	}
}
