package ccr

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestBuildUnifiedDiff(t *testing.T) {
	hunks := []structuredPatchHunk{
		{
			OldStart: 11, OldLines: 3, NewStart: 11, NewLines: 4,
			Lines: []string{" a", "+b", " c"},
		},
	}
	got := buildUnifiedDiff(hunks)
	want := "@@ -11,3 +11,4 @@\n a\n+b\n c\n"
	if got != want {
		t.Errorf("buildUnifiedDiff = %q, want %q", got, want)
	}
}

func TestStripLineNumbers(t *testing.T) {
	in := "1\tpackage main\n2\t\n3\tfunc main() {}\n"
	want := "package main\n\nfunc main() {}\n"
	if got := stripLineNumbers(in); got != want {
		t.Errorf("stripLineNumbers = %q, want %q", got, want)
	}
}

func TestExtractResultTextString(t *testing.T) {
	got := extractResultText([]byte(`"hello world"`))
	if got != "hello world" {
		t.Errorf("extractResultText = %q, want %q", got, "hello world")
	}
}

func TestExtractResultTextBlocks(t *testing.T) {
	raw := []byte(`[{"type":"text","text":"line one"},{"type":"text","text":"line two"}]`)
	got := extractResultText(raw)
	want := "line one\nline two"
	if got != want {
		t.Errorf("extractResultText = %q, want %q", got, want)
	}
}

func TestFallbackDiffText(t *testing.T) {
	got := fallbackDiffText("old", "new")
	want := "-old\n+new\n"
	if got != want {
		t.Errorf("fallbackDiffText = %q, want %q", got, want)
	}
}

func TestRenderCodeBlockCollapsesLongContent(t *testing.T) {
	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, "line")
	}
	code := strings.Join(lines, "\n")

	got := renderCodeBlock(code, "text")
	if !strings.Contains(got, "<details>") {
		t.Error("expected long content to be wrapped in <details>")
	}
}

func TestRenderCodeBlockShortContentNotCollapsed(t *testing.T) {
	got := renderCodeBlock("echo hi", "bash")
	if strings.Contains(got, "<details>") {
		t.Error("short content should not be collapsed")
	}
	if got == "" {
		t.Error("expected non-empty output")
	}
}

func TestRenderCodeBlockEmpty(t *testing.T) {
	if got := renderCodeBlock("", "text"); got != "" {
		t.Errorf("renderCodeBlock(empty) = %q, want empty string", got)
	}
}

func TestRenderCollapsedCodeBlockAlwaysCollapsesShortContent(t *testing.T) {
	got := renderCollapsedCodeBlock("hi", "text", "Output")
	if !strings.Contains(got, "<details>") {
		t.Error("renderCollapsedCodeBlock should always wrap in <details>, even for short content")
	}
	if !strings.Contains(got, "<summary>Output</summary>") {
		t.Errorf("expected summary label %q in output: %s", "Output", got)
	}
	if strings.Contains(got, " open") {
		t.Error("expected <details> to be closed by default")
	}
}

func TestRenderCollapsedCodeBlockEmpty(t *testing.T) {
	if got := renderCollapsedCodeBlock("", "text", "Output"); got != "" {
		t.Errorf("renderCollapsedCodeBlock(empty) = %q, want empty string", got)
	}
}

func TestRenderMarkdownBasic(t *testing.T) {
	got := renderMarkdown("**bold**")
	if !strings.Contains(got, "<strong>bold</strong>") {
		t.Errorf("renderMarkdown = %q, expected <strong>bold</strong>", got)
	}
}

func TestPrettyJSON(t *testing.T) {
	got := prettyJSON([]byte(`{"a":1}`))
	want := "{\n  \"a\": 1\n}"
	if got != want {
		t.Errorf("prettyJSON = %q, want %q", got, want)
	}
}

func TestPrettyJSONInvalidReturnsRaw(t *testing.T) {
	got := prettyJSON([]byte(`not json`))
	if got != "not json" {
		t.Errorf("prettyJSON(invalid) = %q, want raw input echoed back", got)
	}
}

func TestGuessLexerByName(t *testing.T) {
	if l := guessLexer("bash"); l == nil || l.Config().Name != "Bash" {
		t.Errorf("guessLexer(bash) = %+v, want Bash lexer", l)
	}
}

func TestGuessLexerByPathExtension(t *testing.T) {
	if l := guessLexer("main.go"); l == nil || l.Config().Name != "Go" {
		t.Errorf("guessLexer(main.go) = %+v, want Go lexer", l)
	}
}

func TestGuessLexerEmptyName(t *testing.T) {
	if l := guessLexer(""); l == nil {
		t.Error("guessLexer(\"\") should fall back to a non-nil lexer")
	}
}

func TestGuessLexerFallback(t *testing.T) {
	if l := guessLexer("no-such-thing.zzz"); l == nil {
		t.Error("guessLexer should fall back to a non-nil lexer for unknown input")
	}
}

func TestHighlightCodeEmpty(t *testing.T) {
	if got := highlightCode("   ", "bash"); got != "" {
		t.Errorf("highlightCode(blank) = %q, want empty string", got)
	}
}

func TestHighlightCodeNonEmpty(t *testing.T) {
	got := highlightCode("echo hi", "bash")
	if !strings.Contains(got, "echo") {
		t.Errorf("highlightCode = %q, want it to contain the code", got)
	}
}

func TestToolCard(t *testing.T) {
	got := toolCard("Bash", "Bash", "<pre>body</pre>", false)
	if !strings.Contains(got, "tool-card") || strings.Contains(got, "tool-card-error") {
		t.Errorf("toolCard(isError=false) = %q, want tool-card without error class", got)
	}
	if !strings.Contains(got, "Bash") || !strings.Contains(got, "<pre>body</pre>") {
		t.Errorf("toolCard = %q, want title and body present", got)
	}
}

func TestToolCardError(t *testing.T) {
	got := toolCard("Bash", "Bash", "body", true)
	if !strings.Contains(got, "tool-card-error") {
		t.Errorf("toolCard(isError=true) = %q, want tool-card-error class", got)
	}
}

func TestRenderGenericOutcomeEmpty(t *testing.T) {
	if got := renderGenericOutcome(toolOutcome{}); got != "" {
		t.Errorf("renderGenericOutcome(empty) = %q, want empty string", got)
	}
}

func TestRenderGenericOutcomeError(t *testing.T) {
	got := renderGenericOutcome(toolOutcome{resultContent: []byte(`"boom"`), isError: true})
	if !strings.Contains(got, "<summary>Error</summary>") {
		t.Errorf("renderGenericOutcome(isError) = %q, want Error summary", got)
	}
	if !strings.Contains(got, "boom") {
		t.Errorf("renderGenericOutcome = %q, want it to contain the result text", got)
	}
}

func TestRenderToolUseBash(t *testing.T) {
	b := transcriptBlock{
		kind:     "tool_use",
		toolName: "Bash",
		input:    []byte(`{"command":"echo hi","description":"say hi"}`),
		outcome: &toolOutcome{
			toolUseResult: []byte(`{"stdout":"hi\n","stderr":""}`),
		},
	}
	got := renderToolUse(b)
	if !strings.Contains(got, "Bash — say hi") {
		t.Errorf("renderToolUse(Bash) = %q, want title with description", got)
	}
	if !strings.Contains(got, "echo") || !strings.Contains(got, "hi") {
		t.Errorf("renderToolUse(Bash) = %q, want command rendered", got)
	}
	if !strings.Contains(got, "Output") {
		t.Errorf("renderToolUse(Bash) = %q, want Output section for stdout", got)
	}
}

func TestRenderToolUseBashFallsBackToGenericOutcome(t *testing.T) {
	b := transcriptBlock{
		kind:     "tool_use",
		toolName: "Bash",
		input:    []byte(`{"command":"false"}`),
		outcome: &toolOutcome{
			resultContent: []byte(`"boom"`),
			isError:       true,
		},
	}
	got := renderToolUse(b)
	if !strings.Contains(got, "tool-card-error") {
		t.Errorf("renderToolUse(Bash error) = %q, want tool-card-error", got)
	}
	if !strings.Contains(got, "boom") {
		t.Errorf("renderToolUse(Bash error) = %q, want fallback result text", got)
	}
}

func TestRenderToolUseEditWithStructuredPatch(t *testing.T) {
	b := transcriptBlock{
		kind:     "tool_use",
		toolName: "Edit",
		input:    []byte(`{"file_path":"/tmp/foo.go","old_string":"a","new_string":"b"}`),
		outcome: &toolOutcome{
			toolUseResult: []byte(`{"structuredPatch":[{"oldStart":1,"oldLines":1,"newStart":1,"newLines":1,"lines":["-a","+b"]}]}`),
		},
	}
	got := renderToolUse(b)
	if !strings.Contains(got, "/tmp/foo.go") {
		t.Errorf("renderToolUse(Edit) = %q, want file path in title", got)
	}
	if !strings.Contains(got, "@@ -1,1 +1,1 @@") {
		t.Errorf("renderToolUse(Edit) = %q, want structured patch hunk header", got)
	}
}

func TestRenderToolUseEditFallsBackToNaiveDiff(t *testing.T) {
	b := transcriptBlock{
		kind:     "tool_use",
		toolName: "Edit",
		input:    []byte(`{"file_path":"/tmp/foo.go","old_string":"old","new_string":"new"}`),
	}
	got := renderToolUse(b)
	if !strings.Contains(got, "-old") || !strings.Contains(got, "+new") {
		t.Errorf("renderToolUse(Edit, no outcome) = %q, want naive diff", got)
	}
}

func TestRenderToolUseWritePrefersOutcomeContent(t *testing.T) {
	b := transcriptBlock{
		kind:     "tool_use",
		toolName: "Write",
		input:    []byte(`{"file_path":"/tmp/new.txt","content":"input content"}`),
		outcome: &toolOutcome{
			toolUseResult: []byte(`{"content":"result content"}`),
		},
	}
	got := renderToolUse(b)
	if !strings.Contains(got, "result content") {
		t.Errorf("renderToolUse(Write) = %q, want outcome content preferred over input", got)
	}
}

func TestRenderToolUseReadPrefersStructuredResult(t *testing.T) {
	b := transcriptBlock{
		kind:     "tool_use",
		toolName: "Read",
		input:    []byte(`{"file_path":"/tmp/existing.go"}`),
		outcome: &toolOutcome{
			toolUseResult: []byte(`{"file":{"content":"package main"}}`),
		},
	}
	got := renderToolUse(b)
	if !strings.Contains(got, "package") || !strings.Contains(got, "main") {
		t.Errorf("renderToolUse(Read) = %q, want file content rendered", got)
	}
}

func TestRenderToolUseReadFallsBackToGenericResultText(t *testing.T) {
	b := transcriptBlock{
		kind:     "tool_use",
		toolName: "Read",
		input:    []byte(`{"file_path":"/tmp/existing.go"}`),
		outcome: &toolOutcome{
			resultContent: []byte(`"1\tpackage main\n"`),
		},
	}
	got := renderToolUse(b)
	if !strings.Contains(got, "package") || !strings.Contains(got, "main") {
		t.Errorf("renderToolUse(Read fallback) = %q, want file content rendered", got)
	}
	if strings.Contains(got, "1\tpackage") {
		t.Errorf("renderToolUse(Read fallback) = %q, want line numbers stripped", got)
	}
}

func TestRenderToolUseGenericTool(t *testing.T) {
	b := transcriptBlock{
		kind:     "tool_use",
		toolName: "Glob",
		input:    []byte(`{"pattern":"*.go"}`),
	}
	got := renderToolUse(b)
	if !strings.Contains(got, "Glob") {
		t.Errorf("renderToolUse(Glob) = %q, want tool name as title", got)
	}
	if !strings.Contains(got, "*.go") {
		t.Errorf("renderToolUse(Glob) = %q, want pretty-printed input", got)
	}
}

func TestRenderToolUseGenericToolNoName(t *testing.T) {
	b := transcriptBlock{kind: "tool_use", input: []byte(`{}`)}
	got := renderToolUse(b)
	if !strings.Contains(got, "Tool") {
		t.Errorf("renderToolUse(no name) = %q, want fallback title \"Tool\"", got)
	}
}

func TestRenderEntryUnknownRole(t *testing.T) {
	if got := renderEntry(transcriptEntry{role: "system"}); got != "" {
		t.Errorf("renderEntry(unknown role) = %q, want empty string", got)
	}
}

func TestRenderEntryDispatchesByRole(t *testing.T) {
	userEntry := transcriptEntry{role: "user", blocks: []transcriptBlock{{kind: "text", text: "hi"}}}
	if got := renderEntry(userEntry); !strings.Contains(got, "🧑 Human") {
		t.Errorf("renderEntry(user) = %q, want it to dispatch to renderUserEntry", got)
	}

	assistantEntry := transcriptEntry{role: "assistant", blocks: []transcriptBlock{{kind: "text", text: "hi"}}}
	if got := renderEntry(assistantEntry); !strings.Contains(got, "🤖 Claude") {
		t.Errorf("renderEntry(assistant) = %q, want it to dispatch to renderAssistantEntry", got)
	}
}

func TestBuildSessionHTMLMissingFile(t *testing.T) {
	if _, err := buildSessionHTML("id", filepath.Join(t.TempDir(), "missing.jsonl")); err == nil {
		t.Fatal("expected an error for a missing session file")
	}
}

func TestBuildSessionHTMLValid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	content := `{"type":"user","cwd":"/tmp/proj","timestamp":"2026-01-01T00:00:00Z","message":{"content":"hello"}}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := buildSessionHTML("session-1", path)
	if err != nil {
		t.Fatalf("buildSessionHTML: %v", err)
	}
	if !strings.Contains(got, "hello") || !strings.Contains(got, "/tmp/proj") {
		t.Errorf("buildSessionHTML output missing expected content: %q", got)
	}
}

func TestRenderUserEntryHuman(t *testing.T) {
	e := transcriptEntry{role: "user", blocks: []transcriptBlock{{kind: "text", text: "hi there"}}}
	got := renderUserEntry(e)
	if !strings.Contains(got, "🧑 Human") {
		t.Errorf("renderUserEntry = %q, want Human label", got)
	}
	if !strings.Contains(got, "hi there") {
		t.Errorf("renderUserEntry = %q, want message text", got)
	}
}

func TestRenderUserEntryMeta(t *testing.T) {
	e := transcriptEntry{role: "user", isMeta: true, blocks: []transcriptBlock{{kind: "text", text: "note"}}}
	got := renderUserEntry(e)
	if !strings.Contains(got, "⚙️ System") {
		t.Errorf("renderUserEntry(isMeta) = %q, want System label", got)
	}
}

func TestRenderUserEntryNotification(t *testing.T) {
	e := transcriptEntry{
		role:           "user",
		isNotification: true,
		blocks:         []transcriptBlock{{kind: "text", text: "agent finished"}},
	}
	got := renderUserEntry(e)

	if !strings.Contains(got, "🔔 Notification") {
		t.Errorf("renderUserEntry(isNotification) = %q, want Notification label", got)
	}
	if strings.Contains(got, "🧑 Human") {
		t.Errorf("renderUserEntry(isNotification) = %q, must not be attributed to the human", got)
	}
	if !strings.Contains(got, "message-notification") {
		t.Errorf("renderUserEntry(isNotification) = %q, want the notification card class", got)
	}
}

func TestRenderAssistantEntryTextThinkingAndTool(t *testing.T) {
	e := transcriptEntry{
		role: "assistant",
		blocks: []transcriptBlock{
			{kind: "text", text: "hello"},
			{kind: "thinking", text: "pondering"},
			{kind: "tool_use", toolName: "Glob", input: []byte(`{}`)},
		},
	}
	got := renderAssistantEntry(e)
	if !strings.Contains(got, "🤖 Claude") {
		t.Errorf("renderAssistantEntry = %q, want Claude label", got)
	}
	if !strings.Contains(got, "hello") {
		t.Errorf("renderAssistantEntry = %q, want text block rendered", got)
	}
	if !strings.Contains(got, `class="thinking"`) || !strings.Contains(got, "pondering") {
		t.Errorf("renderAssistantEntry = %q, want thinking block rendered", got)
	}
	if !strings.Contains(got, "Glob") {
		t.Errorf("renderAssistantEntry = %q, want tool_use rendered", got)
	}
}

func TestRenderMessageCardWithTimestamp(t *testing.T) {
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	got := renderMessageCard(messageCard{kind: lookupMessageKind("human"), ts: ts, body: "<p>hi</p>"})
	if !strings.Contains(got, "msg-time") {
		t.Errorf("renderMessageCard(non-zero ts) = %q, want a msg-time span", got)
	}
}

func TestRenderMessageCardWithoutTimestamp(t *testing.T) {
	got := renderMessageCard(messageCard{kind: lookupMessageKind("human"), body: "<p>hi</p>"})
	if strings.Contains(got, "msg-time") {
		t.Errorf("renderMessageCard(zero ts) = %q, want no msg-time span", got)
	}
}

func TestRenderTranscriptHTMLIncludesHeaderFields(t *testing.T) {
	meta := sessionHeaderInfo{
		sessionID: "session-1",
		cwd:       "/tmp/proj",
		aiTitle:   "My Session",
		startTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		endTime:   time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC),
	}
	entries := []transcriptEntry{
		{role: "user", blocks: []transcriptBlock{{kind: "text", text: "hi"}}},
	}
	got := renderTranscriptHTML(meta, entries)
	for _, want := range []string{"My Session", "/tmp/proj", "session-1", "Started:", "Ended:", "hi"} {
		if !strings.Contains(got, want) {
			t.Errorf("renderTranscriptHTML missing %q in output", want)
		}
	}
}

func TestRenderTranscriptHTMLTitleFallsBackToSessionID(t *testing.T) {
	meta := sessionHeaderInfo{sessionID: "session-1"}
	got := renderTranscriptHTML(meta, nil)
	if !strings.Contains(got, "<title>session-1</title>") {
		t.Errorf("renderTranscriptHTML = %q, want session id as title", got)
	}
}

func TestCollectFilterRowsCountsOnlyWhatIsPresent(t *testing.T) {
	entries := []transcriptEntry{
		{role: "user", blocks: []transcriptBlock{{kind: "text", text: "hi"}}},
		{role: "assistant", blocks: []transcriptBlock{
			{kind: "thinking", text: "hmm"},
			{kind: "tool_use", toolName: "Bash"},
			{kind: "tool_use", toolName: "Read"},
		}},
		{role: "assistant", blocks: []transcriptBlock{{kind: "tool_use", toolName: "Bash"}}},
	}

	kinds, tools := collectFilterRows(entries)

	kindCounts := map[string]int{}
	for _, r := range kinds {
		kindCounts[r.label] = r.count
	}
	if kindCounts["🧑 Human"] != 1 || kindCounts["🤖 Claude"] != 2 || kindCounts["💭 Thinking"] != 1 {
		t.Errorf("kind rows = %+v, want 1 human, 2 assistant, 1 thinking", kinds)
	}
	// nothing in this transcript is a notification or a system note, so no
	// switch should be offered for them
	if _, ok := kindCounts["🔔 Notification"]; ok {
		t.Errorf("kind rows = %+v, want no Notification row when there are none", kinds)
	}
	if _, ok := kindCounts["⚙️ System"]; ok {
		t.Errorf("kind rows = %+v, want no System row when there are none", kinds)
	}

	if len(tools) != 2 {
		t.Fatalf("tool rows = %+v, want Bash and Read", tools)
	}
	// sorted by name, with each tool's call count
	if tools[0].label != "🖥️ Bash" || tools[0].count != 2 {
		t.Errorf("tools[0] = %+v, want Bash x2", tools[0])
	}
	if tools[1].label != "📖 Read" || tools[1].count != 1 {
		t.Errorf("tools[1] = %+v, want Read x1", tools[1])
	}
	if tools[0].selector != `[data-tool="Bash"]` {
		t.Errorf("tools[0].selector = %q, want a data-tool selector", tools[0].selector)
	}
}

func TestAttrSelectorEscapesValue(t *testing.T) {
	got := attrSelector("data-tool", `we"ird\name`)
	want := `[data-tool="we\"ird\\name"]`
	if got != want {
		t.Errorf("attrSelector = %q, want %q", got, want)
	}
}

func TestRenderTranscriptHTMLIncludesFilterPane(t *testing.T) {
	entries := []transcriptEntry{
		{role: "user", blocks: []transcriptBlock{{kind: "text", text: "hi"}}},
		{role: "assistant", blocks: []transcriptBlock{{kind: "tool_use", toolName: "Bash"}}},
	}
	got := renderTranscriptHTML(sessionHeaderInfo{sessionID: "s1"}, entries)

	for _, want := range []string{
		`class="filters"`,
		`data-sel="[data-kind=&#34;human&#34;]"`,
		`data-sel="[data-tool=&#34;Bash&#34;]"`,
		`data-kind="assistant"`,
		`data-tool="Bash"`,
		`id="filter-style"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("renderTranscriptHTML missing %q", want)
		}
	}
}

func TestRenderAssistantEntryShowsTokenUsage(t *testing.T) {
	e := transcriptEntry{
		role:     "assistant",
		blocks:   []transcriptBlock{{kind: "text", text: "hello"}},
		usage:    tokenUsage{input: 2, output: 144, cacheCreation: 29552},
		hasUsage: true,
	}
	got := renderAssistantEntry(e)

	if !strings.Contains(got, "msg-tokens") {
		t.Errorf("renderAssistantEntry = %q, want a token badge", got)
	}
	if !strings.Contains(got, "29.7K (in 2 / out 144 / cache write 29.6K / cache read 0)") {
		t.Errorf("renderAssistantEntry = %q, want the usage breakdown", got)
	}
}

func TestRenderAssistantEntryWithoutUsageHasNoTokenBadge(t *testing.T) {
	e := transcriptEntry{role: "assistant", blocks: []transcriptBlock{{kind: "text", text: "hello"}}}
	if got := renderAssistantEntry(e); strings.Contains(got, "msg-tokens") {
		t.Errorf("renderAssistantEntry(no usage) = %q, want no token badge", got)
	}
}

// The four message kinds have to be tellable apart at a glance, so each
// carries its own card background rather than sharing the default white.
func TestPageCSSGivesEachMessageKindItsOwnBackground(t *testing.T) {
	backgrounds := map[string]string{}
	for _, k := range messageKinds {
		class := strings.TrimPrefix(k.cssClass, "message ")
		re := regexp.MustCompile(`\.` + regexp.QuoteMeta(class) + ` \{[^}]*background: (#[0-9a-fA-F]{6})`)
		m := re.FindStringSubmatch(pageCSS)
		if m == nil {
			t.Errorf("pageCSS has no background for .%s", class)
			continue
		}
		if prev, ok := backgrounds[m[1]]; ok {
			t.Errorf(".%s reuses %s, already used by .%s", class, m[1], prev)
		}
		backgrounds[m[1]] = class
	}
	if len(backgrounds) != len(messageKinds) {
		t.Errorf("got %d distinct card backgrounds, want one per kind (%d)", len(backgrounds), len(messageKinds))
	}
}

// The filter card is sticky, and a sticky element can only travel inside
// its parent's box. .filters therefore has to keep the full height of the
// flex row: constraining .layout's cross-axis alignment shrink-wraps the
// aside and silently stops the card from following the scroll.
func TestPageCSSLayoutDoesNotShrinkWrapFilterPane(t *testing.T) {
	layout := regexp.MustCompile(`(?s)\n\.layout \{(.*?)\n\}`).FindStringSubmatch(pageCSS)
	if layout == nil {
		t.Fatal("pageCSS has no .layout rule")
	}
	// strip comments before looking for the declaration
	body := regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(layout[1], "")
	if strings.Contains(body, "align-items") {
		t.Errorf(".layout declares align-items (%q); that shrink-wraps .filters and breaks the sticky filter card", body)
	}

	inner := regexp.MustCompile(`(?s)\n\.filters-inner \{(.*?)\n\}`).FindStringSubmatch(pageCSS)
	if inner == nil {
		t.Fatal("pageCSS has no .filters-inner rule")
	}
	if !strings.Contains(inner[1], "position: sticky") {
		t.Errorf(".filters-inner = %q, want position: sticky so the pane follows the scroll", inner[1])
	}
}
