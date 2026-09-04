package ccr

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"regexp"
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

func toolCard(icon, title, body string, isError bool) string {
	class := "tool-card"
	if isError {
		class += " tool-card-error"
	}
	return fmt.Sprintf(`<div class="%s"><div class="tool-header">%s %s</div><div class="tool-body">%s</div></div>`,
		class, icon, html.EscapeString(title), body)
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

	title := "Bash"
	if in.Description != "" {
		title += " — " + in.Description
	}
	return toolCard("🖥️", title, body.String(), b.outcome != nil && b.outcome.isError)
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
	return toolCard("✏️", in.FilePath, body, b.outcome != nil && b.outcome.isError)
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
	return toolCard("📝", in.FilePath, body, b.outcome != nil && b.outcome.isError)
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
	return toolCard("📖", in.FilePath, body, b.outcome != nil && b.outcome.isError)
}

func renderGenericTool(b transcriptBlock) string {
	body := renderCodeBlock(prettyJSON(b.input), "json")
	if b.outcome != nil {
		body += renderGenericOutcome(*b.outcome)
	}
	name := b.toolName
	if name == "" {
		name = "Tool"
	}
	return toolCard("🔧", name, body, b.outcome != nil && b.outcome.isError)
}

func renderEntry(e transcriptEntry) string {
	switch e.role {
	case "user":
		return renderUserEntry(e)
	case "assistant":
		return renderAssistantEntry(e)
	default:
		return ""
	}
}

func renderUserEntry(e transcriptEntry) string {
	label, cardClass := "🧑 Human", "message message-human"
	switch {
	case e.isNotification:
		label, cardClass = "🔔 Notification", "message message-notification"
	case e.isMeta:
		label, cardClass = "⚙️ System", "message message-system"
	}
	var body strings.Builder
	for _, b := range e.blocks {
		if b.kind == "text" {
			body.WriteString(renderMarkdown(b.text))
		}
	}
	return renderMessageCard(cardClass, label, e.timestamp, body.String())
}

func renderAssistantEntry(e transcriptEntry) string {
	var body strings.Builder
	for _, b := range e.blocks {
		switch b.kind {
		case "text":
			body.WriteString(renderMarkdown(b.text))
		case "thinking":
			body.WriteString(`<details class="thinking"><summary>Thinking</summary><div class="thinking-body">`)
			body.WriteString(renderMarkdown(b.text))
			body.WriteString(`</div></details>`)
		case "tool_use":
			body.WriteString(renderToolUse(b))
		}
	}
	return renderMessageCard("message message-assistant", "🤖 Claude", e.timestamp, body.String())
}

func renderMessageCard(cssClass, label string, ts time.Time, bodyHTML string) string {
	timeLabel := ""
	if !ts.IsZero() {
		timeLabel = `<span class="msg-time">` + html.EscapeString(ts.Local().Format("2006-01-02 15:04:05")) + `</span>`
	}
	return fmt.Sprintf(`<div class="%s"><div class="msg-header"><span>%s</span>%s</div><div class="msg-body">%s</div></div>`,
		cssClass, html.EscapeString(label), timeLabel, bodyHTML)
}

// sessionHeaderInfo carries the page-header metadata for
// renderTranscriptHTML.
type sessionHeaderInfo struct {
	sessionID string
	cwd       string
	aiTitle   string
	startTime time.Time
	endTime   time.Time
}

// buildSessionHTML parses the session's jsonl at path and renders it into
// a self-contained HTML page.
func buildSessionHTML(id, path string) (string, error) {
	entries, err := parseTranscript(path)
	if err != nil {
		return "", err
	}
	cwd, aiTitle, _, startTime, endTime, _, err := parseSessionInfo(path)
	if err != nil {
		return "", err
	}
	meta := sessionHeaderInfo{
		sessionID: id,
		cwd:       cwd,
		aiTitle:   aiTitle,
		startTime: startTime,
		endTime:   endTime,
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

	var body strings.Builder
	for _, e := range entries {
		body.WriteString(renderEntry(e))
	}

	var header strings.Builder
	header.WriteString("<h1>" + html.EscapeString(title) + "</h1>\n")
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

	var page strings.Builder
	page.WriteString("<!doctype html>\n<html>\n<head>\n<meta charset=\"utf-8\">\n<title>")
	page.WriteString(html.EscapeString(title))
	page.WriteString("</title>\n<style>\n")
	page.WriteString(pageCSS)
	page.WriteString("\n</style>\n</head>\n<body>\n<div class=\"container\">\n")
	page.WriteString(header.String())
	page.WriteString(`<div class="messages">` + "\n")
	page.WriteString(body.String())
	page.WriteString("\n</div>\n</div>\n</body>\n</html>\n")
	return page.String()
}

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
.container { max-width: 900px; margin: 0 auto; }
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
.messages { display: flex; flex-direction: column; gap: 1rem; }
.message {
  border-radius: 10px;
  padding: 1rem 1.25rem;
  border-left: 4px solid transparent;
  background: #ffffff;
  box-shadow: 0 1px 2px rgba(16, 24, 40, 0.06);
  overflow-x: auto;
}
.message-human { border-left-color: #2f6fed; }
.message-assistant { border-left-color: #8b5cf6; }
.message-system { border-left-color: #9ca3af; opacity: 0.9; font-size: 0.9rem; }
/* sub agent reports and monitor events: not the human, but real content
   worth reading, so kept at full size unlike .message-system */
.message-notification { border-left-color: #d97706; background: #fffbf5; }
.msg-header {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  font-weight: 600;
  margin-bottom: 0.5rem;
  color: #111827;
}
.msg-time { font-weight: 400; font-size: 0.75rem; color: #9ca3af; }
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
