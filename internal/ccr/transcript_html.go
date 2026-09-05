package ccr

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/extension"
	goldmarkhtml "github.com/yuin/goldmark/renderer/html"
)

const chromaStyleName = "github"

var chromaStyle = styles.Get(chromaStyleName)

var markdownRenderer = goldmark.New(
	goldmark.WithExtensions(
		extension.GFM,
		highlighting.NewHighlighting(highlighting.WithStyle(chromaStyleName)),
	),
	goldmark.WithRendererOptions(goldmarkhtml.WithUnsafe()),
)

// renderMarkdown converts Markdown text (assistant prose, human prompts,
// thinking blocks) to HTML. Pseudo-XML wrapper tags that Claude Code
// sometimes injects into user content (e.g. <system-reminder>) pass
// through as unrecognized elements: browsers still render their text
// content, they just don't carry any special styling.
func renderMarkdown(text string) string {
	var buf bytes.Buffer
	if err := markdownRenderer.Convert([]byte(text), &buf); err != nil {
		return "<pre>" + html.EscapeString(text) + "</pre>"
	}
	return buf.String()
}

const collapseLineThreshold = 40

// guessLexer resolves a chroma lexer either by exact name/alias/extension
// (nameOrPath is a lexer name like "bash"/"diff"/"json") or by matching a
// file path's extension, falling back to plaintext.
func guessLexer(nameOrPath string) chroma.Lexer {
	if nameOrPath != "" {
		if l := lexers.Get(nameOrPath); l != nil {
			return l
		}
		if l := lexers.Match(nameOrPath); l != nil {
			return l
		}
	}
	return lexers.Fallback
}

// highlightCode syntax-highlights code using the lexer named
// lexerNameOrPath (either a chroma lexer name like "bash"/"diff"/"json",
// or a file path to guess the language from), returning "" for blank
// input.
func highlightCode(code, lexerNameOrPath string) string {
	if strings.TrimSpace(code) == "" {
		return ""
	}

	lexer := chroma.Coalesce(guessLexer(lexerNameOrPath))
	body := "<pre>" + html.EscapeString(code) + "</pre>"
	if iterator, err := lexer.Tokenise(nil, code); err == nil {
		var buf bytes.Buffer
		if formatErr := chromahtml.New().Format(&buf, chromaStyle, iterator); formatErr == nil {
			body = buf.String()
		}
	}
	return body
}

// renderCodeBlock highlights code, wrapping it in a native <details> (open
// by default) only if it's long enough that collapsing helps readability.
// Used for the "call" side of a tool (commands, input) that's usually
// short and worth seeing at a glance.
func renderCodeBlock(code, lexerNameOrPath string) string {
	body := highlightCode(code, lexerNameOrPath)
	if body == "" {
		return ""
	}
	lines := strings.Count(code, "\n") + 1
	if lines > collapseLineThreshold {
		return collapsibleDetails(fmt.Sprintf("Show %d lines", lines), body)
	}
	return body
}

// renderCollapsedCodeBlock highlights code and always wraps it in a
// closed-by-default <details>/<summary> (click to expand). Used for the
// "result" side of a tool (command output, diffs, file content) which is
// often long and usually skimmed rather than read up front.
func renderCollapsedCodeBlock(code, lexerNameOrPath, label string) string {
	body := highlightCode(code, lexerNameOrPath)
	if body == "" {
		return ""
	}
	return collapsibleDetails(label, body)
}

func collapsibleDetails(summary, body string) string {
	return fmt.Sprintf(`<details><summary>%s</summary>%s</details>`, html.EscapeString(summary), body)
}

var readLineNumberPrefix = regexp.MustCompile(`(?m)^\s*\d+\t`)

// stripLineNumbers removes the "N\t" line-number prefix the Read tool's
// generic tool_result text carries, so the fallback path (used when
// toolUseResult.file.content isn't available) still highlights cleanly.
func stripLineNumbers(text string) string {
	return readLineNumberPrefix.ReplaceAllString(text, "")
}

// extractResultText pulls the human-readable text out of a tool_result
// block's "content" field, which is either a JSON string or an array of
// {"type":"text","text":...} blocks.
func extractResultText(content json.RawMessage) string {
	if len(content) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(content, &s); err == nil {
		return s
	}
	var blocks []rawContentBlock
	if err := json.Unmarshal(content, &blocks); err == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func prettyJSON(raw json.RawMessage) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return string(raw)
	}
	return buf.String()
}

// structuredPatchHunk mirrors the unified-diff hunk shape Claude Code
// already computes for Edit calls (toolUseResult.structuredPatch).
type structuredPatchHunk struct {
	OldStart int      `json:"oldStart"`
	OldLines int      `json:"oldLines"`
	NewStart int      `json:"newStart"`
	NewLines int      `json:"newLines"`
	Lines    []string `json:"lines"`
}

// buildUnifiedDiff renders structuredPatch hunks as unified-diff text
// suitable for the chroma "diff" lexer.
func buildUnifiedDiff(hunks []structuredPatchHunk) string {
	var b strings.Builder
	for _, h := range hunks {
		fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@\n", h.OldStart, h.OldLines, h.NewStart, h.NewLines)
		for _, l := range h.Lines {
			b.WriteString(l)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// fallbackDiffText builds a naive diff (every old line removed, every new
// line added) for the rare case where structuredPatch is unavailable.
func fallbackDiffText(oldStr, newStr string) string {
	var b strings.Builder
	for _, l := range strings.Split(oldStr, "\n") {
		b.WriteString("-" + l + "\n")
	}
	for _, l := range strings.Split(newStr, "\n") {
		b.WriteString("+" + l + "\n")
	}
	return b.String()
}

type bashInput struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

type bashToolUseResult struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
}

type editInput struct {
	FilePath  string `json:"file_path"`
	OldString string `json:"old_string"`
	NewString string `json:"new_string"`
}

type editToolUseResult struct {
	StructuredPatch []structuredPatchHunk `json:"structuredPatch"`
}

type writeInput struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
}

type writeToolUseResult struct {
	Content string `json:"content"`
}

type readInput struct {
	FilePath string `json:"file_path"`
}

type readToolUseResult struct {
	File struct {
		Content string `json:"content"`
	} `json:"file"`
}

// toolIcons gives the tools with a dedicated renderer their own icon; every
// other tool falls back to the generic one.
var toolIcons = map[string]string{
	"Bash":  "🖥️",
	"Edit":  "✏️",
	"Write": "📝",
	"Read":  "📖",
}

const genericToolIcon = "🔧"

// toolFilterName is the name a tool call is grouped under in the filter
// pane: its tool name, or "Tool" for the rare block that carries none.
func toolFilterName(name string) string {
	if name == "" {
		return "Tool"
	}
	return name
}

// toolTitleMaxRunes caps a title built from a command, which can run long.
const toolTitleMaxRunes = 80

// firstLine returns s up to its first newline, trimmed.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// truncateRunes shortens s to at most max runes, marking where it was cut.
func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// toolTitle is the one-line description of a tool call: the title its card
// shows, reused as the summary of the collapsed run it sits in.
func toolTitle(b transcriptBlock) string {
	switch b.toolName {
	case "Bash":
		var in bashInput
		_ = json.Unmarshal(b.input, &in)
		if in.Description != "" {
			return "Bash — " + in.Description
		}
		// no description: the command itself says more than "Bash" does
		if cmd := firstLine(in.Command); cmd != "" {
			return "Bash — " + truncateRunes(cmd, toolTitleMaxRunes)
		}
		return "Bash"
	case "Edit":
		var in editInput
		_ = json.Unmarshal(b.input, &in)
		return in.FilePath
	case "Write":
		var in writeInput
		_ = json.Unmarshal(b.input, &in)
		return in.FilePath
	case "Read":
		var in readInput
		_ = json.Unmarshal(b.input, &in)
		return in.FilePath
	default:
		return toolFilterName(b.toolName)
	}
}

func toolIcon(name string) string {
	if icon, ok := toolIcons[name]; ok {
		return icon
	}
	return genericToolIcon
}

// renderToolUse renders one tool_use block (and its correlated result, if
// any) as a self-contained "tool card".
func renderToolUse(b transcriptBlock) string {
	switch b.toolName {
	case "Bash":
		return renderBashTool(b)
	case "Edit":
		return renderEditTool(b)
	case "Write":
		return renderWriteTool(b)
	case "Read":
		return renderReadTool(b)
	default:
		return renderGenericTool(b)
	}
}

// toolCard renders one tool call. toolName goes into data-tool so the
// filter pane can show or hide every call of that tool.
func toolCard(toolName, title, body string, isError bool) string {
	class := "tool-card"
	if isError {
		class += " tool-card-error"
	}
	return fmt.Sprintf(`<div class="%s" data-tool="%s"><div class="tool-header">%s %s</div><div class="tool-body">%s</div></div>`,
		class, html.EscapeString(toolName), toolIcon(toolName), html.EscapeString(title), body)
}

// renderGenericOutcome renders a tool_result's generic text content
// (fallback for tools without a richer toolUseResult-specific rendering).
func renderGenericOutcome(o toolOutcome) string {
	text := extractResultText(o.resultContent)
	if text == "" {
		return ""
	}
	label := "Result"
	if o.isError {
		label = "Error"
	}
	return renderCollapsedCodeBlock(text, "text", label)
}

func renderBashTool(b transcriptBlock) string {
	var in bashInput
	_ = json.Unmarshal(b.input, &in)

	var body strings.Builder
	body.WriteString(renderCodeBlock(in.Command, "bash"))

	if b.outcome != nil {
		var res bashToolUseResult
		if len(b.outcome.toolUseResult) > 0 && json.Unmarshal(b.outcome.toolUseResult, &res) == nil && (res.Stdout != "" || res.Stderr != "") {
			if strings.TrimSpace(res.Stdout) != "" {
				body.WriteString(renderCollapsedCodeBlock(res.Stdout, "text", "Output"))
			}
			if strings.TrimSpace(res.Stderr) != "" {
				body.WriteString(renderCollapsedCodeBlock(res.Stderr, "text", "stderr"))
			}
		} else {
			body.WriteString(renderGenericOutcome(*b.outcome))
		}
	}

	return toolCard("Bash", toolTitle(b), body.String(), b.outcome != nil && b.outcome.isError)
}

func renderEditTool(b transcriptBlock) string {
	var in editInput
	_ = json.Unmarshal(b.input, &in)

	diffText := ""
	if b.outcome != nil && len(b.outcome.toolUseResult) > 0 {
		var res editToolUseResult
		if json.Unmarshal(b.outcome.toolUseResult, &res) == nil && len(res.StructuredPatch) > 0 {
			diffText = buildUnifiedDiff(res.StructuredPatch)
		}
	}
	if diffText == "" {
		diffText = fallbackDiffText(in.OldString, in.NewString)
	}

	body := renderCollapsedCodeBlock(diffText, "diff", "Diff")
	return toolCard("Edit", toolTitle(b), body, b.outcome != nil && b.outcome.isError)
}

func renderWriteTool(b transcriptBlock) string {
	var in writeInput
	_ = json.Unmarshal(b.input, &in)

	content := in.Content
	if b.outcome != nil && len(b.outcome.toolUseResult) > 0 {
		var res writeToolUseResult
		if json.Unmarshal(b.outcome.toolUseResult, &res) == nil && res.Content != "" {
			content = res.Content
		}
	}

	body := renderCollapsedCodeBlock(content, in.FilePath, "Content")
	return toolCard("Write", toolTitle(b), body, b.outcome != nil && b.outcome.isError)
}

func renderReadTool(b transcriptBlock) string {
	var in readInput
	_ = json.Unmarshal(b.input, &in)

	var content string
	if b.outcome != nil && len(b.outcome.toolUseResult) > 0 {
		var res readToolUseResult
		if json.Unmarshal(b.outcome.toolUseResult, &res) == nil && res.File.Content != "" {
			content = res.File.Content
		}
	}
	if content == "" && b.outcome != nil {
		content = stripLineNumbers(extractResultText(b.outcome.resultContent))
	}

	body := renderCollapsedCodeBlock(content, in.FilePath, "Content")
	return toolCard("Read", toolTitle(b), body, b.outcome != nil && b.outcome.isError)
}

func renderGenericTool(b transcriptBlock) string {
	body := renderCodeBlock(prettyJSON(b.input), "json")
	if b.outcome != nil {
		body += renderGenericOutcome(*b.outcome)
	}
	return toolCard(toolFilterName(b.toolName), toolTitle(b), body, b.outcome != nil && b.outcome.isError)
}

// claudeMarkSVG is Claude's own mark, drawn inline so the page stays
// self-contained: rounded rays bursting from a centre point, in Anthropic's
// coral. currentColor is deliberately not used — the mark keeps its colour
// wherever it sits.
const claudeMarkSVG = `<svg class="kind-icon" viewBox="0 0 24 24" aria-hidden="true"><g stroke="#d97757" stroke-width="2.7" stroke-linecap="round"><path d="M12.00 9.40L12.00 2.40"/><path d="M10.62 9.80L7.65 5.05"/><path d="M9.66 10.86L3.73 7.97"/><path d="M9.41 12.27L4.04 12.84"/><path d="M9.95 13.60L4.59 17.79"/><path d="M11.11 14.44L9.20 19.71"/><path d="M12.54 14.54L14.00 21.39"/><path d="M13.81 13.87L17.56 17.75"/><path d="M14.52 12.63L20.93 14.23"/><path d="M14.47 11.20L19.80 9.47"/><path d="M13.67 10.01L18.04 4.80"/></g></svg>`

// messageKind is one filterable class of message: how its card is labelled
// and styled, and (via its key) the data-kind the filter pane switches on.
type messageKind struct {
	key string
	// icon is markup, not text: an emoji, or the inline SVG of Claude's
	// mark. It is written to the page as-is, so it may only ever come from
	// the constants below.
	icon     string
	name     string
	cssClass string
}

// labelHTML is the kind's icon and name, ready to write into the page.
func (k messageKind) labelHTML() string {
	return k.icon + " " + html.EscapeString(k.name)
}

// messageKinds lists every message kind in the order the filter pane shows
// them.
var messageKinds = []messageKind{
	{"human", "🐰", "Human", "message message-human"},
	{"assistant", claudeMarkSVG, "Claude", "message message-assistant"},
	{"notification", "🔔", "Notification", "message message-notification"},
	{"system", "⚙️", "System", "message message-system"},
}

// thinkingKind is the filter key for assistant thinking blocks. They live
// inside assistant cards rather than being cards of their own, so they are
// filtered separately from the message kinds.
const thinkingKind = "thinking"

// entryKind reports which messageKind an entry belongs to.
func entryKind(e transcriptEntry) string {
	if e.role == "assistant" {
		return "assistant"
	}
	switch {
	case e.isNotification:
		return "notification"
	case e.isMeta:
		return "system"
	default:
		return "human"
	}
}

func lookupMessageKind(key string) messageKind {
	for _, k := range messageKinds {
		if k.key == key {
			return k
		}
	}
	return messageKind{key: key, name: key, cssClass: "message"}
}

// subagentLinker resolves the sub agents a transcript spawned to the URLs
// that render them, so the page can link a tool call (or the report it
// produced) straight to that agent's own transcript. The zero value links
// nothing, which is what a transcript without sub agents needs.
type subagentLinker struct {
	byToolUse map[string]subagentInfo
	byID      map[string]subagentInfo
	urlFor    func(subagentInfo) string
}

func newSubagentLinker(agents []subagentInfo, urlFor func(subagentInfo) string) subagentLinker {
	l := subagentLinker{
		byToolUse: make(map[string]subagentInfo, len(agents)),
		byID:      make(map[string]subagentInfo, len(agents)),
		urlFor:    urlFor,
	}
	for _, a := range agents {
		if a.toolUseID != "" {
			l.byToolUse[a.toolUseID] = a
		}
		l.byID[a.id] = a
	}
	return l
}

// taskIDPattern pulls the agent id out of a task notification, which names
// the agent it is reporting for in a <task-id> tag.
var taskIDPattern = regexp.MustCompile(`<task-id>\s*([A-Za-z0-9._-]+)\s*</task-id>`)

// linkForToolUse returns the sub agent a tool call spawned, if any.
func (l subagentLinker) linkForToolUse(toolUseID string) (subagentInfo, bool) {
	a, ok := l.byToolUse[toolUseID]
	return a, ok
}

// linkForText returns the sub agent a notification is reporting for, found
// by the <task-id> it carries.
func (l subagentLinker) linkForText(text string) (subagentInfo, bool) {
	m := taskIDPattern.FindStringSubmatch(text)
	if m == nil {
		return subagentInfo{}, false
	}
	a, ok := l.byID[m[1]]
	return a, ok
}

// renderSubagentLink renders the "open this sub agent's transcript" link.
func (l subagentLinker) renderSubagentLink(a subagentInfo) string {
	return fmt.Sprintf(`<a class="subagent-link" href="%s">🧵 %s</a>`,
		html.EscapeString(l.urlFor(a)), html.EscapeString(a.label()))
}

// isToolOnlyEntry reports whether an assistant turn is nothing but tool
// calls. Those are what get folded away: a turn that also says something
// keeps its own card, since the prose is the part worth reading.
func isToolOnlyEntry(e transcriptEntry) bool {
	if e.role != "assistant" || len(e.blocks) == 0 {
		return false
	}
	for _, b := range e.blocks {
		if b.kind != "tool_use" {
			return false
		}
	}
	return true
}

// renderMessages renders the message stream, folding each run of
// consecutive tool-only turns into one collapsed group so the prose stays
// scannable. A run of one is rendered as a bare collapsed turn: wrapping it
// in a group would only cost a second click to reach the same thing.
func renderMessages(entries []transcriptEntry, links subagentLinker) string {
	var out strings.Builder
	var run []transcriptEntry

	flush := func() {
		switch len(run) {
		case 0:
		case 1:
			out.WriteString(renderToolRun(run[0], links))
		default:
			out.WriteString(renderToolGroup(run, links))
		}
		run = nil
	}

	for _, e := range entries {
		if isToolOnlyEntry(e) {
			run = append(run, e)
			continue
		}
		flush()
		out.WriteString(renderEntry(e, links))
	}
	flush()
	return out.String()
}

// renderToolGroup wraps a run of tool-only turns in one collapsed
// <details>, each turn collapsed again inside it.
func renderToolGroup(entries []transcriptEntry, links subagentLinker) string {
	var body strings.Builder
	for _, e := range entries {
		body.WriteString(renderToolRun(e, links))
	}
	// data-kind marks these as the assistant turns they are, so the Claude
	// switch in the filter pane keeps covering them once they are folded.
	return fmt.Sprintf(`<details class="tool-group" data-kind="assistant"><summary><span class="tool-group-count">%d</span> <span class="tool-group-noun">tool calls</span></summary><div class="tool-group-body">%s</div></details>`,
		len(entries), body.String())
}

// renderToolRun renders one tool-only turn as a collapsed <details> whose
// summary names the calls it holds, keeping the turn's timestamp and token
// usage visible without opening it.
func renderToolRun(e transcriptEntry, links subagentLinker) string {
	var body strings.Builder
	titles := make([]string, 0, len(e.blocks))
	for _, b := range e.blocks {
		body.WriteString(renderToolUse(b))
		if a, ok := links.linkForToolUse(b.toolUseID); ok {
			body.WriteString(links.renderSubagentLink(a))
		}
		titles = append(titles, toolIcon(b.toolName)+" "+toolTitle(b))
	}

	var aside strings.Builder
	if e.hasUsage {
		aside.WriteString(`<span class="msg-tokens">🎟️ ` + html.EscapeString(formatTokenUsage(e.usage)) + `</span>`)
	}
	if !e.timestamp.IsZero() {
		aside.WriteString(`<span class="msg-time">` + html.EscapeString(e.timestamp.Local().Format("2006-01-02 15:04:05")) + `</span>`)
	}

	class := "tool-run"
	// With a single call the summary already says everything the card's own
	// header would, so the header is dropped and the failure it would have
	// signalled is carried by the summary instead. A run holding several
	// calls keeps its headers: there they are the only way to tell the
	// cards apart.
	if len(e.blocks) == 1 {
		class += " tool-run-single"
	}
	for _, b := range e.blocks {
		if b.outcome != nil && b.outcome.isError {
			class += " tool-run-error"
			break
		}
	}

	return fmt.Sprintf(`<details class="%s" data-kind="assistant"><summary><span class="tool-run-title">%s</span><span class="msg-aside">%s</span></summary><div class="tool-run-body">%s</div></details>`,
		class, html.EscapeString(strings.Join(titles, " · ")), aside.String(), body.String())
}

func renderEntry(e transcriptEntry, links subagentLinker) string {
	switch e.role {
	case "user":
		return renderUserEntry(e, links)
	case "assistant":
		return renderAssistantEntry(e, links)
	default:
		return ""
	}
}

func renderUserEntry(e transcriptEntry, links subagentLinker) string {
	var body strings.Builder
	for _, b := range e.blocks {
		if b.kind == "text" {
			if a, ok := links.linkForText(b.text); ok {
				body.WriteString(links.renderSubagentLink(a))
			}
			body.WriteString(renderMarkdown(b.text))
		}
	}
	return renderMessageCard(messageCard{
		kind: lookupMessageKind(entryKind(e)),
		ts:   e.timestamp,
		body: body.String(),
	})
}

func renderAssistantEntry(e transcriptEntry, links subagentLinker) string {
	var body strings.Builder
	for _, b := range e.blocks {
		switch b.kind {
		case "text":
			body.WriteString(renderMarkdown(b.text))
		case "thinking":
			body.WriteString(`<details class="thinking" data-kind="` + thinkingKind + `"><summary>Thinking</summary><div class="thinking-body">`)
			body.WriteString(renderMarkdown(b.text))
			body.WriteString(`</div></details>`)
		case "tool_use":
			body.WriteString(renderToolUse(b))
			if a, ok := links.linkForToolUse(b.toolUseID); ok {
				body.WriteString(links.renderSubagentLink(a))
			}
		}
	}

	card := messageCard{
		kind: lookupMessageKind("assistant"),
		ts:   e.timestamp,
		body: body.String(),
	}
	if e.hasUsage {
		card.tokens = formatTokenUsage(e.usage)
	}
	return renderMessageCard(card)
}

// messageCard is one rendered message: its kind (which drives the label,
// styling and data-kind), the token usage to show alongside the timestamp
// if the turn reported any, and the already-rendered body HTML.
type messageCard struct {
	kind   messageKind
	tokens string
	ts     time.Time
	body   string
}

func renderMessageCard(c messageCard) string {
	var aside strings.Builder
	if c.tokens != "" {
		aside.WriteString(`<span class="msg-tokens" title="token usage for this turn">🎟️ ` + html.EscapeString(c.tokens) + `</span>`)
	}
	if !c.ts.IsZero() {
		aside.WriteString(`<span class="msg-time">` + html.EscapeString(c.ts.Local().Format("2006-01-02 15:04:05")) + `</span>`)
	}
	return fmt.Sprintf(`<div class="%s" data-kind="%s"><div class="msg-header"><span>%s</span><span class="msg-aside">%s</span></div><div class="msg-body">%s</div></div>`,
		c.kind.cssClass, html.EscapeString(c.kind.key), c.kind.labelHTML(), aside.String(), c.body)
}

// filterRow is one checkbox in the filter pane: what it hides, how it is
// labelled, and how many of them the transcript holds.
type filterRow struct {
	selector string // CSS selector for the elements it controls
	// label is markup, so a kind's icon can be an inline SVG. Anything
	// coming from the transcript (a tool name) must be escaped into it.
	label string
	count int
}

// collectFilterRows walks the entries and returns the filter pane's two
// groups: one row per message kind, and one per tool actually called. Kinds
// absent from this transcript get no row, so the pane never offers a switch
// that does nothing.
func collectFilterRows(entries []transcriptEntry) (kinds, tools []filterRow) {
	kindCounts := map[string]int{}
	toolCounts := map[string]int{}
	for _, e := range entries {
		kindCounts[entryKind(e)]++
		for _, b := range e.blocks {
			switch b.kind {
			case thinkingKind:
				kindCounts[thinkingKind]++
			case "tool_use":
				toolCounts[toolFilterName(b.toolName)]++
			}
		}
	}

	for _, k := range messageKinds {
		if n := kindCounts[k.key]; n > 0 {
			kinds = append(kinds, filterRow{selector: attrSelector("data-kind", k.key), label: k.labelHTML(), count: n})
		}
	}
	if n := kindCounts[thinkingKind]; n > 0 {
		kinds = append(kinds, filterRow{selector: attrSelector("data-kind", thinkingKind), label: "💭 " + html.EscapeString("Thinking"), count: n})
	}

	names := make([]string, 0, len(toolCounts))
	for name := range toolCounts {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		tools = append(tools, filterRow{
			selector: attrSelector("data-tool", name),
			label:    toolIcon(name) + " " + html.EscapeString(name),
			count:    toolCounts[name],
		})
	}
	return kinds, tools
}

// attrSelector builds a CSS attribute selector, escaping the value the way
// a CSS string literal requires.
func attrSelector(attr, value string) string {
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return fmt.Sprintf(`[%s="%s"]`, attr, escaped)
}

// renderFilterPane renders the sidebar: the checkboxes that show and hide
// message kinds and individual tools, followed by links to the transcripts
// of any sub agents this session spawned.
func renderFilterPane(kinds, tools []filterRow, agents []subagentInfo, links subagentLinker) string {
	var b strings.Builder
	b.WriteString(`<aside class="filters">` + "\n")
	b.WriteString(`<div class="filters-inner">` + "\n")
	b.WriteString(`<div class="filters-head"><strong>Filter</strong><span class="filter-actions"><button type="button" data-all="1">All</button><button type="button" data-all="0">None</button></span></div>` + "\n")
	writeFilterGroup(&b, "Messages", kinds)
	writeFilterGroup(&b, "Tools", tools)
	writeSubagentGroup(&b, agents, links)
	b.WriteString("</div>\n</aside>\n")
	return b.String()
}

// writeSubagentGroup lists every sub agent transcript, so one spawned in a
// way the page cannot tie to a specific message is still reachable.
func writeSubagentGroup(b *strings.Builder, agents []subagentInfo, links subagentLinker) {
	if len(agents) == 0 {
		return
	}
	b.WriteString(`<div class="filter-group"><div class="filter-group-title">Sub agents</div>` + "\n")
	for _, a := range agents {
		fmt.Fprintf(b, `<a class="subagent-row" href="%s"><span class="filter-label">🧵 %s</span></a>`+"\n",
			html.EscapeString(links.urlFor(a)), html.EscapeString(a.label()))
	}
	b.WriteString("</div>\n")
}

func writeFilterGroup(b *strings.Builder, title string, rows []filterRow) {
	if len(rows) == 0 {
		return
	}
	b.WriteString(`<div class="filter-group"><div class="filter-group-title">` + html.EscapeString(title) + `</div>` + "\n")
	for _, r := range rows {
		fmt.Fprintf(b, `<label class="filter"><input type="checkbox" checked data-sel="%s"><span class="filter-label">%s</span><span class="filter-count">%d</span></label>`+"\n",
			html.EscapeString(r.selector), r.label, r.count)
	}
	b.WriteString("</div>\n")
}

// sessionHeaderInfo carries the page-header metadata for
// renderTranscriptHTML.
type sessionHeaderInfo struct {
	sessionID string
	cwd       string
	aiTitle   string
	startTime time.Time
	endTime   time.Time
	// subtitle names what this page is when it isn't the session itself
	// (a sub agent's own transcript), and backLink points home from it.
	subtitle string
	backLink string
	// agents are the sub agent transcripts this session spawned, and links
	// resolves them to URLs.
	agents []subagentInfo
	links  subagentLinker
}

// sessionURL and subagentURL are the paths the transcript server serves: a
// session at /<session_id>, and each of its sub agents one level below it.
func sessionURL(sessionID string) string { return "/" + sessionID }

func subagentURL(sessionID, agentID string) string {
	return "/" + sessionID + "/" + subagentDirName + "/" + agentID
}

// buildSessionHTML parses the session's jsonl at path and renders it into
// a self-contained HTML page, linking any sub agents it spawned.
func buildSessionHTML(id, path string) (string, error) {
	entries, err := parseTranscript(path)
	if err != nil {
		return "", err
	}
	cwd, aiTitle, _, startTime, endTime, _, err := parseSessionInfo(path)
	if err != nil {
		return "", err
	}

	agents := findSubagents(path)
	meta := sessionHeaderInfo{
		sessionID: id,
		cwd:       cwd,
		aiTitle:   aiTitle,
		startTime: startTime,
		endTime:   endTime,
		agents:    agents,
		links: newSubagentLinker(agents, func(a subagentInfo) string {
			return subagentURL(id, a.id)
		}),
	}
	return renderTranscriptHTML(meta, entries), nil
}

// buildSubagentHTML renders one sub agent's transcript. Sub agent files
// hold the same lines as a session, so they go through the same pipeline,
// with a header naming the agent and linking back to its session.
func buildSubagentHTML(sessionID string, agent subagentInfo) (string, error) {
	entries, err := parseTranscript(agent.path)
	if err != nil {
		return "", err
	}
	cwd, _, _, startTime, endTime, _, err := parseSessionInfo(agent.path)
	if err != nil {
		return "", err
	}

	subtitle := agent.agentType
	if agent.name != "" {
		subtitle = agent.name
	}
	meta := sessionHeaderInfo{
		sessionID: sessionID,
		cwd:       cwd,
		aiTitle:   agent.label(),
		startTime: startTime,
		endTime:   endTime,
		subtitle:  subtitle,
		backLink:  sessionURL(sessionID),
	}
	return renderTranscriptHTML(meta, entries), nil
}

// renderTranscriptHTML assembles a complete, self-contained HTML page (no
// external CSS/JS/fonts) for the given session.
func renderTranscriptHTML(meta sessionHeaderInfo, entries []transcriptEntry) string {
	title := meta.aiTitle
	if title == "" {
		title = meta.sessionID
	}

	body := renderMessages(entries, meta.links)

	var header strings.Builder
	if meta.backLink != "" {
		header.WriteString(`<a class="back-link" href="` + html.EscapeString(meta.backLink) + `">← back to the session</a>` + "\n")
	}
	header.WriteString("<h1>" + html.EscapeString(title) + "</h1>\n")
	if meta.subtitle != "" {
		header.WriteString(`<div class="subtitle">` + html.EscapeString(meta.subtitle) + "</div>\n")
	}
	header.WriteString(`<div class="meta">` + "\n")
	if meta.cwd != "" {
		header.WriteString("<div><strong>Directory:</strong> " + html.EscapeString(meta.cwd) + "</div>\n")
	}
	header.WriteString("<div><strong>Session:</strong> " + html.EscapeString(meta.sessionID) + "</div>\n")
	if !meta.startTime.IsZero() {
		header.WriteString("<div><strong>Started:</strong> " + html.EscapeString(meta.startTime.Local().Format("2006-01-02 15:04:05")) + "</div>\n")
	}
	if !meta.endTime.IsZero() {
		header.WriteString("<div><strong>Ended:</strong> " + html.EscapeString(meta.endTime.Local().Format("2006-01-02 15:04:05")) + "</div>\n")
	}
	header.WriteString("</div>\n")

	kinds, tools := collectFilterRows(entries)

	var page strings.Builder
	page.WriteString("<!doctype html>\n<html>\n<head>\n<meta charset=\"utf-8\">\n<title>")
	page.WriteString(html.EscapeString(title))
	page.WriteString("</title>\n<style>\n")
	page.WriteString(pageCSS)
	page.WriteString("\n</style>\n</head>\n<body>\n")
	page.WriteString("<div class=\"layout\">\n<div class=\"container\">\n")
	page.WriteString(header.String())
	page.WriteString(`<div class="messages">` + "\n")
	page.WriteString(body)
	page.WriteString("\n</div>\n</div>\n")
	page.WriteString(renderFilterPane(kinds, tools, meta.agents, meta.links))
	page.WriteString("</div>\n")
	// style#filter-style is rewritten by the script as boxes are toggled
	page.WriteString("<style id=\"filter-style\"></style>\n<script>\n")
	page.WriteString(filterScript)
	page.WriteString("\n</script>\n</body>\n</html>\n")
	return page.String()
}

// filterScript drives the filter pane: unchecking a box adds its selector
// to a stylesheet that hides those elements, then any message card left
// with nothing visible in its body is hidden too, so filtering tools does
// not leave a trail of empty cards.
const filterScript = `
(function () {
  var style = document.getElementById('filter-style');
  var boxes = Array.prototype.slice.call(document.querySelectorAll('.filters input[type=checkbox]'));

  function apply() {
    var hidden = boxes.filter(function (b) { return !b.checked; })
                      .map(function (b) { return b.dataset.sel; });
    style.textContent = hidden.length ? hidden.join(',') + '{display:none !important}' : '';

    document.querySelectorAll('.message').forEach(function (card) {
      card.classList.remove('is-empty');
      if (getComputedStyle(card).display === 'none') return;
      var body = card.querySelector('.msg-body');
      if (!body) return;
      var visible = Array.prototype.slice.call(body.children).some(function (el) {
        return getComputedStyle(el).display !== 'none';
      });
      if (!visible) card.classList.add('is-empty');
    });

    // a folded run whose every tool is filtered out has nothing to open,
    // and a group of only such runs has nothing left either
    document.querySelectorAll('.tool-run').forEach(function (run) {
      run.classList.remove('is-empty');
      var tools = Array.prototype.slice.call(run.querySelectorAll('[data-tool]'));
      var visible = tools.some(function (el) { return getComputedStyle(el).display !== 'none'; });
      if (!visible) run.classList.add('is-empty');
    });
    document.querySelectorAll('.tool-group').forEach(function (group) {
      group.classList.remove('is-empty');
      var runs = Array.prototype.slice.call(group.querySelectorAll('.tool-run'));
      var shown = runs.filter(function (r) { return getComputedStyle(r).display !== 'none'; });
      if (!shown.length) {
        group.classList.add('is-empty');
        return;
      }
      // keep the summary honest about how many calls it still holds
      var count = group.querySelector('.tool-group-count');
      var noun = group.querySelector('.tool-group-noun');
      if (count) count.textContent = shown.length;
      if (noun) noun.textContent = shown.length === 1 ? 'tool call' : 'tool calls';
    });
  }

  boxes.forEach(function (b) { b.addEventListener('change', apply); });
  document.querySelectorAll('.filter-actions button').forEach(function (btn) {
    btn.addEventListener('click', function () {
      var on = btn.dataset.all === '1';
      boxes.forEach(function (b) { b.checked = on; });
      apply();
    });
  });
})();
`

const pageCSS = `
:root { color-scheme: light; }
* { box-sizing: border-box; }
body {
  margin: 0;
  padding: 2rem 1rem;
  background: #f3f4f7;
  color: #1f2430;
  font-family: -apple-system, "Segoe UI", Helvetica, Arial, sans-serif;
  line-height: 1.5;
}
.layout {
  display: flex;
  /* No align-items here on purpose: the default "stretch" is what lets the
     filter card stay in view while scrolling. A sticky element can only
     travel inside its parent's box, so .filters has to keep the full height
     of the row -- shrink-wrapping it (align-items: flex-start) leaves the
     card nowhere to move and silently kills the stickiness. */
  gap: 1.5rem;
  max-width: 1280px;
  margin: 0 auto;
}
.container { flex: 1; max-width: 900px; min-width: 0; margin: 0 auto; }
.filters { width: 15rem; flex: none; font-size: 0.85rem; }
.filters-inner {
  position: sticky;
  top: 1rem;
  background: #ffffff;
  border-radius: 10px;
  padding: 0.85rem 1rem;
  box-shadow: 0 1px 2px rgba(16, 24, 40, 0.06);
  max-height: calc(100vh - 2rem);
  overflow-y: auto;
}
.filters-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 0.5rem;
}
.filter-actions { display: flex; gap: 0.25rem; }
.filter-actions button {
  font: inherit;
  font-size: 0.75rem;
  color: #4b5563;
  background: #f3f4f7;
  border: 1px solid #e5e7eb;
  border-radius: 5px;
  padding: 0.1rem 0.4rem;
  cursor: pointer;
}
.filter-actions button:hover { background: #e5e7eb; }
.filter-group { margin-top: 0.75rem; }
.filter-group-title {
  font-size: 0.7rem;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: #9ca3af;
  margin-bottom: 0.25rem;
}
.filter {
  display: flex;
  align-items: center;
  gap: 0.4rem;
  padding: 0.15rem 0;
  cursor: pointer;
}
.filter-label { flex: 1; min-width: 0; overflow-wrap: anywhere; }
.filter-count { color: #9ca3af; font-variant-numeric: tabular-nums; }
@media (max-width: 1100px) {
  .layout { flex-direction: column-reverse; }
  .filters { width: 100%; max-width: 900px; margin: 0 auto; }
  .filters-inner { position: static; max-height: none; }
}
h1 { font-size: 1.5rem; margin-bottom: 0.25rem; }
.meta {
  font-size: 0.85rem;
  color: #6b7280;
  margin-bottom: 2rem;
  display: flex;
  flex-wrap: wrap;
  gap: 1rem;
}
.meta strong { color: #374151; }
.subtitle { font-size: 0.9rem; color: #6b7280; margin-bottom: 0.5rem; }
.back-link {
  display: inline-block;
  font-size: 0.8rem;
  color: #2f6fed;
  text-decoration: none;
  margin-bottom: 0.5rem;
}
.back-link:hover { text-decoration: underline; }
/* link from a tool call (or the report it produced) to that sub agent's
   own transcript */
.subagent-link {
  display: inline-block;
  margin: 0.35rem 0;
  padding: 0.2rem 0.6rem;
  border-radius: 999px;
  background: #ffffff;
  border: 1px solid #c9b7ee;
  color: #6d28d9;
  font-size: 0.8rem;
  text-decoration: none;
}
.subagent-link:hover { background: #f5efff; }
.subagent-row {
  display: block;
  padding: 0.15rem 0;
  color: #6d28d9;
  text-decoration: none;
  overflow-wrap: anywhere;
}
.subagent-row:hover { text-decoration: underline; }
.messages { display: flex; flex-direction: column; gap: 1rem; }
/* Runs of tool calls are folded away by default so the prose reads as a
   conversation; open the group to get the individual calls, and open one
   of those to get its command and output. */
.tool-group, .messages > .tool-run {
  border-radius: 10px;
  background: #ffffff;
  border: 1px solid #e2e4ea;
  box-shadow: 0 1px 2px rgba(16, 24, 40, 0.06);
}
.tool-group > summary, .messages > .tool-run > summary {
  padding: 0.6rem 1rem;
  font-size: 0.85rem;
  color: #4b5563;
  font-weight: 600;
  cursor: pointer;
}
.tool-group-count { font-variant-numeric: tabular-nums; }
.tool-group-body { padding: 0 0.75rem 0.6rem; display: flex; flex-direction: column; gap: 0.4rem; }
.tool-group-body > .tool-run {
  border-radius: 8px;
  background: #f8f9fb;
  border: 1px solid #e2e4ea;
}
.tool-run > summary {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  gap: 0.75rem;
  padding: 0.4rem 0.75rem;
  font-size: 0.82rem;
  color: #33394a;
  cursor: pointer;
}
.tool-run-title { overflow-wrap: anywhere; font-family: "SFMono-Regular", Consolas, Menlo, monospace; }
.tool-run > summary .msg-aside { flex: none; }
.tool-run-body { padding: 0 0.75rem 0.5rem; overflow-x: auto; }
.tool-run-body > .tool-card:first-child { margin-top: 0; }
/* the summary of a single-call run already carries the title, so the card
   repeats nothing; the error colouring it would have shown moves out here */
.tool-run-single .tool-header { display: none; }
.tool-run-error > summary { background: #fdecea; color: #b42318; border-radius: 8px; }
.tool-run-error[open] > summary { border-radius: 8px 8px 0 0; }
.is-empty { display: none !important; }
.message {
  border-radius: 10px;
  padding: 1rem 1.25rem;
  border-left: 4px solid transparent;
  background: #ffffff;
  box-shadow: 0 1px 2px rgba(16, 24, 40, 0.06);
  overflow-x: auto;
}
/* One tint per speaker, so scrolling the page shows at a glance who is
   talking. They stay pale: the tool cards, code blocks and <details> inside
   a message are neutral greys that have to keep reading as neutral on top
   of any of these. */
.message-human { border-left-color: #2f6fed; background: #d8e6ff; }
.message-assistant { border-left-color: #8b5cf6; background: #f2ddfa; }
/* sub agent reports and monitor events: not the human, but real content
   worth reading, so kept at full size unlike .message-system */
.message-notification { border-left-color: #d97706; background: #fcecc4; }
.message-system { border-left-color: #6b7280; background: #d9dde7; opacity: 0.9; font-size: 0.9rem; }
.msg-header {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  font-weight: 600;
  margin-bottom: 0.5rem;
  color: #111827;
}
/* Claude's mark, sized to sit on the text baseline like the emoji the
   other kinds use */
.kind-icon {
  width: 1.1em;
  height: 1.1em;
  vertical-align: -0.2em;
  fill: none;
}
.msg-aside { display: flex; align-items: baseline; gap: 0.6rem; font-weight: 400; }
.msg-time { font-weight: 400; font-size: 0.75rem; color: #9ca3af; }
.msg-tokens {
  font-size: 0.75rem;
  color: #6b7280;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}
.msg-body :first-child { margin-top: 0; }
.msg-body :last-child { margin-bottom: 0; }
.msg-body pre {
  padding: 0.75rem 1rem;
  border-radius: 8px;
  overflow-x: auto;
  font-size: 0.85rem;
  border: 1px solid #e5e7eb;
}
.msg-body code { font-family: "SFMono-Regular", Consolas, Menlo, monospace; }
.msg-body p code, .msg-body li code {
  background: #eef0f4;
  padding: 0.1rem 0.35rem;
  border-radius: 4px;
}
.tool-card {
  margin: 0.75rem 0;
  border-radius: 8px;
  background: #f8f9fb;
  border: 1px solid #e2e4ea;
  overflow: hidden;
}
.tool-card-error { border-color: #f0b4b8; }
.tool-header {
  padding: 0.5rem 0.9rem;
  font-size: 0.85rem;
  font-weight: 600;
  background: #eef0f5;
  color: #33394a;
  font-family: "SFMono-Regular", Consolas, Menlo, monospace;
}
.tool-card-error .tool-header { background: #fdecea; color: #b42318; }
.tool-body { padding: 0.25rem 0.9rem 0.75rem; overflow-x: auto; }
.tool-body pre {
  margin: 0.5rem 0;
  border-radius: 6px;
  overflow-x: auto;
  font-size: 0.82rem;
  padding: 0.75rem 1rem;
  border: 1px solid #e5e7eb;
}
details {
  margin: 0.5rem 0;
  border-radius: 6px;
  background: #f0f1f4;
  border: 1px solid #e2e4ea;
}
details > summary {
  padding: 0.35rem 0.75rem;
  font-size: 0.8rem;
  color: #4b5563;
  font-weight: 600;
  cursor: pointer;
}
details[open] > summary { border-bottom: 1px solid #e2e4ea; }
details > pre { margin: 0; border: none; border-radius: 0 0 6px 6px; }
details.thinking { background: #f5f5fb; border-color: #e4e4f2; font-size: 0.85rem; }
.thinking-body { padding: 0.1rem 0.75rem 0.6rem; color: #4b5563; }
.thinking-body :first-child { margin-top: 0; }
.thinking-body :last-child { margin-bottom: 0; }
`
