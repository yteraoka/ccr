package ccr

import (
	"fmt"
	"sort"
	"time"
)

// costState mirrors the "cost-state" line Claude Code writes: a running
// tally of what the session has cost and where its time went. A session
// file carries several as it goes, each superseding the last, so the one
// worth showing is the final one.
type costState struct {
	TotalCostUSD                 float64               `json:"totalCostUSD"`
	TotalAPIDuration             int64                 `json:"totalAPIDuration"`
	TotalAPIDurationWithoutRetry int64                 `json:"totalAPIDurationWithoutRetries"`
	TotalToolDuration            int64                 `json:"totalToolDuration"`
	TotalDuration                int64                 `json:"totalDuration"`
	TotalLinesAdded              int                   `json:"totalLinesAdded"`
	TotalLinesRemoved            int                   `json:"totalLinesRemoved"`
	ModelUsage                   map[string]modelUsage `json:"modelUsage"`
	HasUnknownModelCost          bool                  `json:"hasUnknownModelCost"`
}

// modelUsage is what one model was asked to do, and what that cost.
type modelUsage struct {
	InputTokens              int     `json:"inputTokens"`
	OutputTokens             int     `json:"outputTokens"`
	CacheReadInputTokens     int     `json:"cacheReadInputTokens"`
	CacheCreationInputTokens int     `json:"cacheCreationInputTokens"`
	WebSearchRequests        int     `json:"webSearchRequests"`
	CostUSD                  float64 `json:"costUSD"`
}

// total is the tokens this model accounted for, across every kind.
func (u modelUsage) total() int {
	return u.InputTokens + u.OutputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
}

// modelNames returns the models in a stable order, so the page renders the
// same way every time.
func (c costState) modelNames() []string {
	names := make([]string, 0, len(c.ModelUsage))
	for name := range c.ModelUsage {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// formatUSD writes a dollar amount at a precision that keeps it readable:
// cents for the amounts that have any, and more places for the ones that
// would otherwise round away to nothing.
func formatUSD(v float64) string {
	if v != 0 && v < 0.01 {
		return fmt.Sprintf("$%.4f", v)
	}
	return fmt.Sprintf("$%.2f", v)
}

// humanDuration formats a millisecond count the way the page reads best:
// seconds below a minute, minutes and seconds below an hour, and hours and
// minutes beyond that.
func humanDuration(ms int64) string {
	if ms <= 0 {
		return "0s"
	}
	d := time.Duration(ms) * time.Millisecond
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	case d < time.Hour:
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	default:
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	}
}
