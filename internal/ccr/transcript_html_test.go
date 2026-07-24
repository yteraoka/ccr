package ccr

import (
	"strings"
	"testing"
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
