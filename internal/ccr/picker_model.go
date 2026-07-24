package ccr

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

var (
	selectedRowStyle = lipgloss.NewStyle().Reverse(true)
	headerRowStyle   = lipgloss.NewStyle().Bold(true).Underline(true)
	legendStyle      = lipgloss.NewStyle().Faint(true)
)

const promptBullet = "·" // U+00B7 MIDDLE DOT, representable in Latin-1

const keyLegend = "up/down/j/k: move   enter: resume   v: view transcript   q/esc/ctrl-c: quit"

// pickerModel is the bubbletea model backing the interactive session
// picker: the top pane lists sessions sorted by recency, the bottom pane
// previews the cwd and recent messages of whichever session is highlighted.
type pickerModel struct {
	sessions []sessionEntry
	cursor   int
	width    int
	height   int

	previewID         string
	previewCwd        string
	previewSize       int64
	previewAiTitle    string
	previewPrompts    []string
	previewStart      time.Time
	previewEnd        time.Time
	previewServingURL string
	previewErr        error

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

	case tea.KeyMsg:
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
			m.previewServingURL = url
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
		m.previewServingURL = url
	} else {
		m.previewServingURL = ""
	}
	if id == m.previewID {
		return
	}
	m.previewID = id

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
	cwd, aiTitle, prompts, startTime, endTime, err := parseSessionInfo(path)
	if err != nil {
		m.resetPreview(err)
		return
	}
	m.previewErr, m.previewCwd, m.previewSize, m.previewAiTitle, m.previewPrompts = nil, cwd, info.Size(), aiTitle, prompts
	m.previewStart, m.previewEnd = startTime, endTime
}

// resetPreview clears the preview pane state and records err (nil for "no
// error, just nothing to show yet").
func (m *pickerModel) resetPreview(err error) {
	m.previewErr, m.previewCwd, m.previewSize, m.previewAiTitle, m.previewPrompts = err, "", 0, "", nil
	m.previewStart, m.previewEnd = time.Time{}, time.Time{}
}

func (m pickerModel) View() string {
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

	return listView(m.sessions, m.cursor, listHeight, width) + "\n" +
		strings.Repeat("─", width) + "\n" +
		previewView(m.previewCwd, m.previewSize, m.previewAiTitle, m.previewPrompts, m.previewStart, m.previewEnd, m.previewServingURL, m.previewErr, previewHeight, width) + "\n" +
		truncate(m.statusMsg, width)
}

func formatRow(timestamp, id, pid, cwdBasename string) string {
	return fmt.Sprintf("%-16s  %-36s  %7s  %s", timestamp, id, pid, cwdBasename)
}

func listView(sessions []sessionEntry, cursor, height, width int) string {
	header := headerRowStyle.Render(truncate(formatRow("TIMESTAMP", "SESSION ID", "PID", "CWD"), width))
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
		line := truncate(formatRow(s.timestamp.Local().Format("2006-01-02 15:04"), s.id, pid, basename), width)
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

func previewView(cwd string, size int64, aiTitle string, prompts []string, start, end time.Time, servingURL string, err error, height, width int) string {
	if err != nil {
		return truncate("error: "+err.Error(), width)
	}

	lines := []string{
		truncate("Directory: "+cwd, width),
	}
	if aiTitle != "" {
		lines = append(lines, truncate("Title: "+aiTitle, width))
	}
	lines = append(lines, truncate("Size: "+humanSize(size), width))
	if !start.IsZero() {
		lines = append(lines, truncate("Started: "+start.Local().Format("2006-01-02 15:04:05"), width))
	}
	if !end.IsZero() {
		lines = append(lines, truncate("Ended:   "+end.Local().Format("2006-01-02 15:04:05"), width))
	}
	if servingURL != "" {
		lines = append(lines, truncate("Serving at: "+servingURL, width))
	}
	if len(prompts) > 0 {
		lines = append(lines, "", truncate("Prompts:", width))
		bulletWidth := runewidth.StringWidth(promptBullet)
		promptWidth := width - bulletWidth
		if promptWidth < 1 {
			promptWidth = 1
		}
		indent := strings.Repeat(" ", bulletWidth)
		for _, p := range prompts {
			first := true
			for _, l := range strings.Split(p, "\n") {
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
