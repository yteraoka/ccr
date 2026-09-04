package ccr

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/mattn/go-runewidth"
)

var (
	selectedRowStyle = lipgloss.NewStyle().Reverse(true)
	headerRowStyle   = lipgloss.NewStyle().Bold(true).Underline(true)
	legendStyle      = lipgloss.NewStyle().Faint(true)
)

const promptBullet = "·" // U+00B7 MIDDLE DOT, representable in Latin-1

const keyLegend = "up/down/j/k: move   enter: resume   v: view transcript   q/esc/ctrl-c: quit"

// previewData is everything the bottom pane shows about the highlighted
// session. sessionID doubles as loadPreview's cache key: it is filled in
// even when reading the session failed, so a broken session is not re-read
// on every keystroke.
type previewData struct {
	sessionID  string
	cwd        string
	size       int64
	aiTitle    string
	prompts    []string
	start      time.Time
	end        time.Time
	tokens     tokenUsage
	servingURL string
	err        error
}

// pickerModel is the bubbletea model backing the interactive session
// picker: the top pane lists sessions sorted by recency, the bottom pane
// previews the cwd and recent messages of whichever session is highlighted.
type pickerModel struct {
	sessions []sessionEntry
	cursor   int
	width    int
	height   int

	preview previewData

	statusMsg string

	selected sessionEntry
}

func newPickerModel(sessions []sessionEntry) pickerModel {
	m := pickerModel{sessions: sessions}
	m.loadPreview()
	return m
}

func (m pickerModel) Init() tea.Cmd {
	return nil
}

func (m pickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				m.loadPreview()
			}
		case "down", "j":
			if m.cursor < len(m.sessions)-1 {
				m.cursor++
				m.loadPreview()
			}
		case "enter":
			m.selected = m.sessions[m.cursor]
			return m, tea.Quit
		case "v":
			url, err := serveAndOpenTranscript(m.sessions[m.cursor].id)
			m.preview.servingURL = url
			if err != nil {
				m.statusMsg = "error: " + err.Error()
			} else {
				m.statusMsg = ""
			}
		}
	}
	return m, nil
}

// loadPreview parses the currently highlighted session's file so the
// preview pane can show its directory, file size, and recent prompts.
func (m *pickerModel) loadPreview() {
	id := m.sessions[m.cursor].id
	if url, ok := runningTranscriptServerURL(id); ok {
		m.preview.servingURL = url
	} else {
		m.preview.servingURL = ""
	}
	if id == m.preview.sessionID {
		return
	}
	m.preview.sessionID = id

	path, err := findSessionFile(projectsDir(), id)
	if err != nil {
		m.resetPreview(err)
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		m.resetPreview(err)
		return
	}
	cwd, aiTitle, prompts, startTime, endTime, usage, err := parseSessionInfo(path)
	if err != nil {
		m.resetPreview(err)
		return
	}
	m.preview = previewData{
		sessionID:  id,
		cwd:        cwd,
		size:       info.Size(),
		aiTitle:    aiTitle,
		prompts:    prompts,
		start:      startTime,
		end:        endTime,
		tokens:     usage,
		servingURL: m.preview.servingURL,
	}
}

// resetPreview drops everything the preview pane shows about the current
// session except the id it was read from and its serving URL, recording err
// (nil for "no error, just nothing to show yet").
func (m *pickerModel) resetPreview(err error) {
	m.preview = previewData{
		sessionID:  m.preview.sessionID,
		servingURL: m.preview.servingURL,
		err:        err,
	}
}

func (m pickerModel) View() tea.View {
	width, height := m.width, m.height
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}

	previewHeight := height / 3
	if previewHeight < 8 {
		previewHeight = 8
	}
	listHeight := height - previewHeight - 2 // separator line + status footer line
	if listHeight < 2 {
		listHeight = 2
	}

	content := listView(m.sessions, m.cursor, listHeight, width) + "\n" +
		strings.Repeat("─", width) + "\n" +
		previewView(m.preview, previewHeight, width) + "\n" +
		truncate(m.statusMsg, width)

	v := tea.NewView(content)
	v.AltScreen = true
	return v
}

const (
	// fullIDWidth is the length of a session id (a UUID); shortIDWidth is
	// what it collapses to, git-style, on a terminal too narrow to show
	// both the full id and a usable CWD column.
	fullIDWidth  = 36
	shortIDWidth = 8
	// minCwdWidth is how many columns CWD must keep before the session id
	// is shortened to make room for it.
	minCwdWidth = 20
)

// rowPrefixWidth is how many columns a row spends before CWD when the
// session id column is idWidth wide (the other columns plus their gutters).
func rowPrefixWidth(idWidth int) int {
	return 16 + 2 + idWidth + 2 + 7 + 2 + 8 + 2
}

// listIDWidth returns the width of the SESSION ID column: the full id when
// the terminal can still give CWD a usable column, otherwise a short prefix.
// Session ids are unique enough in their first few characters to tell rows
// apart, while CWD is often the only thing distinguishing sessions from
// different projects under `ccr -g`.
func listIDWidth(width int) int {
	if width >= rowPrefixWidth(fullIDWidth)+minCwdWidth {
		return fullIDWidth
	}
	return shortIDWidth
}

func formatRow(idWidth int, timestamp, id, pid, tokens, cwdBasename string) string {
	return fmt.Sprintf("%-16s  %-*s  %7s  %8s  %s", timestamp, idWidth, runewidth.Truncate(id, idWidth, ""), pid, tokens, cwdBasename)
}

func listView(sessions []sessionEntry, cursor, height, width int) string {
	idWidth := listIDWidth(width)
	idHeader := "SESSION ID"
	if idWidth < len(idHeader) {
		idHeader = "ID"
	}
	header := headerRowStyle.Render(truncate(formatRow(idWidth, "TIMESTAMP", idHeader, "PID", "TOKENS", "CWD"), width))
	legend := legendStyle.Render(truncate(keyLegend, width))

	rowHeight := height - 2 // header line + key legend line
	if rowHeight < 0 {
		rowHeight = 0
	}

	start := 0
	if cursor >= rowHeight {
		start = cursor - rowHeight + 1
	}
	end := start + rowHeight
	if end > len(sessions) {
		end = len(sessions)
	}

	lines := make([]string, 0, rowHeight)
	for i := start; i < end; i++ {
		s := sessions[i]
		basename := ""
		if s.cwd != "" {
			basename = filepath.Base(s.cwd)
		}
		pid := ""
		if s.pid != 0 {
			pid = strconv.Itoa(s.pid)
		}
		line := truncate(formatRow(idWidth, s.timestamp.Local().Format("2006-01-02 15:04"), s.id, pid, humanCount(s.tokens), basename), width)
		if i == cursor {
			line = selectedRowStyle.Render(line)
		}
		lines = append(lines, line)
	}
	for len(lines) < rowHeight {
		lines = append(lines, "")
	}
	return header + "\n" + strings.Join(lines, "\n") + "\n" + legend
}

func previewView(p previewData, height, width int) string {
	if p.err != nil {
		return truncate("error: "+p.err.Error(), width)
	}

	// The list shortens the session id on narrow terminals, so the preview
	// always carries the full one.
	lines := []string{
		truncate("Session: "+p.sessionID, width),
		truncate("Directory: "+p.cwd, width),
	}
	if p.aiTitle != "" {
		lines = append(lines, truncate("Title: "+p.aiTitle, width))
	}
	lines = append(lines, truncate("Size: "+humanSize(p.size), width))
	lines = append(lines, truncate("Tokens: "+formatTokenUsage(p.tokens), width))
	if !p.start.IsZero() {
		lines = append(lines, truncate("Started: "+p.start.Local().Format("2006-01-02 15:04:05"), width))
	}
	if !p.end.IsZero() {
		lines = append(lines, truncate("Ended:   "+p.end.Local().Format("2006-01-02 15:04:05"), width))
	}
	if p.servingURL != "" {
		lines = append(lines, truncate("Serving at: "+p.servingURL, width))
	}
	if len(p.prompts) > 0 {
		lines = append(lines, "", truncate("Prompts:", width))
		bulletWidth := runewidth.StringWidth(promptBullet)
		promptWidth := width - bulletWidth
		if promptWidth < 1 {
			promptWidth = 1
		}
		indent := strings.Repeat(" ", bulletWidth)
		for _, prompt := range p.prompts {
			first := true
			for _, l := range strings.Split(prompt, "\n") {
				for _, wl := range wrap(l, promptWidth) {
					prefix := indent
					if first {
						prefix = promptBullet
						first = false
					}
					lines = append(lines, prefix+wl)
				}
			}
		}
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}

// humanSize formats n bytes as a human-readable size using binary (1024)
// units, e.g. "12.3 KB", "1.2 MB".
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for n2 := n / unit; n2 >= unit; n2 /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// formatTokenUsage renders a session's cumulative token usage as its total
// followed by the per-kind breakdown, e.g.
// "1.2M (in 4.0K / out 24.1K / cache write 132.0K / cache read 1.1M)".
// "input"/"output" are abbreviated to keep the whole line inside 80 columns.
func formatTokenUsage(u tokenUsage) string {
	return fmt.Sprintf("%s (in %s / out %s / cache write %s / cache read %s)",
		humanCount(u.total()), humanCount(u.input), humanCount(u.output),
		humanCount(u.cacheCreation), humanCount(u.cacheRead))
}

const countUnits = "KMGTPE"

// humanCount formats n using decimal (1000) units with one decimal place
// once it reaches four digits, e.g. "999", "1.5K", "2.3M".
func humanCount(n int) string {
	const unit = 1000
	if n < unit {
		return strconv.Itoa(n)
	}
	div, exp := int64(unit), 0
	for n2 := int64(n) / unit; n2 >= unit; n2 /= unit {
		div *= unit
		exp++
	}
	// The unit is picked before %.1f rounds, so a value just shy of the
	// next one (999_999) would print as "1000.0K"; roll it over instead.
	v := float64(n) / float64(div)
	if v >= 999.95 && exp < len(countUnits)-1 {
		v /= unit
		exp++
	}
	return fmt.Sprintf("%.1f%c", v, countUnits[exp])
}

// wrap splits s into lines that each fit within width display columns
// (wide East Asian characters count as 2), so long prompt text wraps onto
// multiple lines instead of overflowing or being cut off.
func wrap(s string, width int) []string {
	if width <= 0 {
		return []string{s}
	}
	return strings.Split(runewidth.Wrap(s, width), "\n")
}

// truncate cuts s to at most width display columns, appending "…" if it
// had to cut.
func truncate(s string, width int) string {
	if width <= 0 {
		return s
	}
	return runewidth.Truncate(s, width, "…")
}
