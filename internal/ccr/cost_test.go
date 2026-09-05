package ccr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const costLine = `{"type":"cost-state","sessionId":"s1","totalCostUSD":0.7504959999999999,` +
	`"totalAPIDuration":56645,"totalAPIDurationWithoutRetries":56624,"totalToolDuration":22542,` +
	`"totalLinesAdded":12,"totalLinesRemoved":3,"totalDuration":195296,"startTime":1787991647111,` +
	`"modelUsage":{"claude-opus-5":{"inputTokens":116,"outputTokens":3565,"cacheReadInputTokens":433858,` +
	`"cacheCreationInputTokens":44183,"webSearchRequests":0,"costUSD":0.7484639999999999},` +
	`"claude-haiku-4-5-20251001":{"inputTokens":1852,"outputTokens":36,"cacheReadInputTokens":0,` +
	`"cacheCreationInputTokens":0,"webSearchRequests":0,"costUSD":0.002032}},"hasUnknownModelCost":false}`

// A session file carries several cost-state lines as it goes, each
// superseding the last, so the tally shown has to be the final one.
func TestParseSessionInfoTakesTheLastCostState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	earlier := strings.Replace(costLine, `"totalCostUSD":0.7504959999999999`, `"totalCostUSD":0.1`, 1)
	content := `{"type":"user","cwd":"/tmp/proj"}` + "\n" + earlier + "\n" + costLine + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := parseSessionInfo(path)
	if err != nil {
		t.Fatalf("parseSessionInfo: %v", err)
	}
	if got.cost == nil {
		t.Fatal("no cost-state was picked up")
	}
	if got.cost.TotalCostUSD != 0.7504959999999999 {
		t.Errorf("totalCostUSD = %v, want the last line's value", got.cost.TotalCostUSD)
	}
	if got.cost.TotalLinesAdded != 12 || got.cost.TotalLinesRemoved != 3 {
		t.Errorf("lines = +%d/-%d, want +12/-3", got.cost.TotalLinesAdded, got.cost.TotalLinesRemoved)
	}
	if len(got.cost.ModelUsage) != 2 {
		t.Errorf("modelUsage has %d models, want 2", len(got.cost.ModelUsage))
	}
	if u := got.cost.ModelUsage["claude-opus-5"]; u.total() != 116+3565+433858+44183 {
		t.Errorf("opus total = %d, want every kind of token summed", u.total())
	}
	// models are ordered, so the page renders the same way every time
	if names := got.cost.modelNames(); names[0] != "claude-haiku-4-5-20251001" || names[1] != "claude-opus-5" {
		t.Errorf("modelNames = %v, want them sorted", names)
	}
}

func TestParseSessionInfoWithoutCostState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"user","cwd":"/tmp/proj"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := parseSessionInfo(path)
	if err != nil {
		t.Fatalf("parseSessionInfo: %v", err)
	}
	if got.cost != nil {
		t.Errorf("cost = %+v, want none for a file that records none", got.cost)
	}
}

func TestFormatUSD(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "$0.00"},
		{0.7504959999999999, "$0.75"},
		{2.7207304999999997, "$2.72"},
		// amounts that would round away to nothing keep their places
		{0.002032, "$0.0020"},
		{0.0001, "$0.0001"},
	}
	for _, c := range cases {
		if got := formatUSD(c.in); got != c.want {
			t.Errorf("formatUSD(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestHumanDuration(t *testing.T) {
	cases := []struct {
		ms   int64
		want string
	}{
		{0, "0s"},
		{-1, "0s"},
		{1500, "1.5s"},
		{56645, "56.6s"},
		{195296, "3m 15s"},
		{19615296, "5h 26m"},
	}
	for _, c := range cases {
		if got := humanDuration(c.ms); got != c.want {
			t.Errorf("humanDuration(%d) = %q, want %q", c.ms, got, c.want)
		}
	}
}

func TestRenderCostState(t *testing.T) {
	if got := renderCostState(nil); got != "" {
		t.Errorf("renderCostState(nil) = %q, want nothing rendered", got)
	}

	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(costLine+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	parsed, err := parseSessionInfo(path)
	if err != nil {
		t.Fatal(err)
	}

	got := renderCostState(parsed.cost)
	for _, want := range []string{
		"$0.75",                     // the total
		"claude-opus-5",             // each model
		"claude-haiku-4-5-20251001", //
		"$0.0020",                   // a small per-model cost, not rounded to $0.00
		"433.9K",                    // its tokens, in the page's own units
		"3m 15s",                    // the session's duration
		"56.6s",                     // the API time
		"+12 / -3",                  // lines touched
	} {
		if !strings.Contains(got, want) {
			t.Errorf("renderCostState missing %q in:\n%s", want, got)
		}
	}
	// it is collapsed: this describes the session, not the conversation
	if !strings.Contains(got, "<details class=\"cost\">") || strings.Contains(got, "<details class=\"cost\" open") {
		t.Errorf("renderCostState should be a closed <details>:\n%s", got)
	}
}

func TestRenderCostStateFlagsUnknownModelCost(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	line := strings.Replace(costLine, `"hasUnknownModelCost":false`, `"hasUnknownModelCost":true`, 1)
	if err := os.WriteFile(path, []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	parsed, err := parseSessionInfo(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(renderCostState(parsed.cost), "lower bound") {
		t.Error("a total that is missing a model's price should say so")
	}
}

func TestTranscriptPageShowsCostWhenTheFileHasIt(t *testing.T) {
	dir := t.TempDir()
	withCost := filepath.Join(dir, "a.jsonl")
	if err := os.WriteFile(withCost, []byte(`{"type":"user","message":{"content":"hi"}}`+"\n"+costLine+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	page, err := buildSessionHTML("a", withCost)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(page, "<strong>Cost:</strong> $0.75") {
		t.Error("the header should carry the cost alongside the other session facts")
	}
	if !strings.Contains(page, `class="cost"`) {
		t.Error("the breakdown should be on the page")
	}

	// and a file with no cost-state renders no cost at all
	without := filepath.Join(dir, "b.jsonl")
	if err := os.WriteFile(without, []byte(`{"type":"user","message":{"content":"hi"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	page, err = buildSessionHTML("b", without)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(page, "<strong>Cost:</strong>") || strings.Contains(page, `<details class="cost"`) {
		t.Error("a file with no cost-state should render no cost")
	}
}
