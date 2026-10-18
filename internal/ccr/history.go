package ccr

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/term"
)

var searchHitStyle = lipgloss.NewStyle().Bold(true).Reverse(true)

// hookInput is the part of a Claude Code hook's stdin JSON that `ccr hook`
// reads, for the UserPromptSubmit and Stop events.
type hookInput struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	HookEventName  string `json:"hook_event_name"`
	// Prompt is the text the person submitted (UserPromptSubmit).
	Prompt string `json:"prompt"`
	// LastAssistantMessage is the final assistant text of the turn (Stop).
	LastAssistantMessage string `json:"last_assistant_message"`
}

// RunHook implements `ccr hook`: registered for the UserPromptSubmit and
// Stop hooks, it records the person's prompt or Claude's answer in the
// history database, and embeds it straight away when an embedding model
// is configured (if that fails the message is embedded by a later search).
//
// It never writes to stdout: what a UserPromptSubmit hook prints is added
// to the conversation.
func RunHook(stdin io.Reader) error {
	var in hookInput
	if err := json.NewDecoder(stdin).Decode(&in); err != nil {
		return fmt.Errorf("hook: reading input: %w", err)
	}

	var role, text string
	switch in.HookEventName {
	case "UserPromptSubmit":
		role, text = "user", in.Prompt
	case "Stop":
		role, text = "assistant", in.LastAssistantMessage
		if text == "" && in.TranscriptPath != "" {
			// older Claude Code does not send the answer; dig it out
			text = lastAssistantText(in.TranscriptPath)
		}
	default:
		return nil // registered for an event it has nothing to record from
	}
	if strings.TrimSpace(text) == "" || in.SessionID == "" {
		return nil
	}

	cfg, err := loadFileConfig(ccrConfigPath())
	if err != nil {
		return err
	}
	path, err := historyDBPath(cfg.History)
	if err != nil {
		return err
	}
	h, err := openHistoryDB(path)
	if err != nil {
		return err
	}
	defer h.Close() //nolint:errcheck

	id, err := h.add(historyMessage{
		sessionID: in.SessionID,
		role:      role,
		text:      text,
		cwd:       in.Cwd,
		configDir: configDir(),
		createdAt: time.Now(),
	})
	if err != nil {
		return err
	}

	e := newEmbedder(cfg.History.Embedding)
	if e == nil {
		return nil
	}
	if err := h.prepareVectors(e.key()); err != nil {
		return err
	}
	vecs, err := e.embedDocuments(context.Background(), []string{text})
	if err != nil {
		return err
	}
	return h.storeVectors([]int64{id}, vecs)
}

// lastAssistantText returns the text of the last assistant message after
// the last prompt the person typed in the transcript at path, or "" if
// there is none.
func lastAssistantText(path string) string {
	entries, err := parseTranscript(path)
	if err != nil {
		return ""
	}
	last := ""
	for _, e := range entries {
		if e.role == "user" && !e.isMeta && !e.isNotification {
			last = ""
			continue
		}
		if e.role != "assistant" {
			continue
		}
		var texts []string
		for _, b := range e.blocks {
			if b.kind == "text" {
				texts = append(texts, b.text)
			}
		}
		if len(texts) > 0 {
			last = strings.Join(texts, "\n\n")
		}
	}
	return last
}

// search modes for `ccr search -mode`.
const (
	searchHybrid  = "hybrid"
	searchKeyword = "keyword"
	searchVector  = "vector"
)

// RunSearch implements `ccr search`: finds recorded prompts and answers by
// keyword (BM25), by meaning (vector), or both fused, and prints them.
func RunSearch(args []string) error {
	fs := flag.NewFlagSet("ccr search", flag.ExitOnError)
	global := fs.Bool("g", false, "search every project, not just the current directory")
	limit := fs.Int("l", 10, "number of results")
	role := fs.String("role", "", `only "user" prompts or "assistant" answers`)
	mode := fs.String("mode", "", `"hybrid" (default with an embedding model), "keyword" or "vector"`)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: ccr search [-g] [-l n] [-role user|assistant] [-mode hybrid|keyword|vector] <query>")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	query := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if query == "" {
		fs.Usage()
		return errors.New("no query given")
	}
	switch *role {
	case "", "user", "assistant":
	default:
		return fmt.Errorf("-role must be user or assistant, not %q", *role)
	}

	cfg, err := loadFileConfig(ccrConfigPath())
	if err != nil {
		return err
	}
	e := newEmbedder(cfg.History.Embedding)
	switch *mode {
	case "":
		*mode = searchKeyword
		if e != nil {
			*mode = searchHybrid
		}
	case searchKeyword:
	case searchHybrid, searchVector:
		if e == nil {
			return fmt.Errorf("-mode %s needs history.embedding in %s", *mode, ccrConfigPath())
		}
	default:
		return fmt.Errorf("-mode must be hybrid, keyword or vector, not %q", *mode)
	}

	path, err := historyDBPath(cfg.History)
	if err != nil {
		return err
	}
	h, err := openHistoryDB(path)
	if err != nil {
		return err
	}
	defer h.Close() //nolint:errcheck

	filter := historyFilter{role: *role}
	if !*global {
		if filter.cwd, err = os.Getwd(); err != nil {
			return err
		}
	}

	ctx := context.Background()
	candidates := max(*limit*5, 50)
	var rankings [][]int64
	if *mode != searchKeyword {
		ids, err := vectorRanking(ctx, h, e, query, filter, candidates)
		if err != nil {
			if *mode == searchVector {
				return err
			}
			fmt.Fprintf(os.Stderr, "ccr: vector search skipped: %v\n", err)
		}
		rankings = append(rankings, ids)
	}
	if *mode != searchVector {
		if match := ftsQuery(query); match != "" {
			ids, err := h.keywordSearch(match, filter, candidates)
			if err != nil {
				return err
			}
			rankings = append(rankings, ids)
		}
	}

	ids := fuseRankings(rankings...)
	if len(ids) > *limit {
		ids = ids[:*limit]
	}
	results, err := h.messages(ids)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		fmt.Fprintln(os.Stderr, "no matches")
		return nil
	}
	_, err = io.WriteString(os.Stdout, formatSearchResults(results, query, *global, term.IsTerminal(os.Stdout.Fd())))
	return err
}

// vectorRanking embeds whatever is still waiting for a vector, then the
// query, and returns the nearest messages.
func vectorRanking(ctx context.Context, h *historyDB, e *embedder, query string, f historyFilter, limit int) ([]int64, error) {
	if err := h.prepareVectors(e.key()); err != nil {
		return nil, err
	}
	err := h.embedPending(ctx, e, func(done, total int) {
		fmt.Fprintf(os.Stderr, "\rembedding %d/%d messages...", done, total)
		if done == total {
			fmt.Fprintln(os.Stderr)
		}
	})
	if err != nil {
		return nil, err
	}
	vec, err := e.embedQuery(ctx, query)
	if err != nil {
		return nil, err
	}
	return h.vectorSearch(vec, f, limit)
}

// snippetRunes is how much of a message a search result shows.
const snippetRunes = 160

// formatSearchResults renders one header line and one snippet line per
// result. The header carries the full session id, to hand to
// `claude --resume`.
func formatSearchResults(results []historyMessage, query string, showCwd, color bool) string {
	var b strings.Builder
	terms := searchTerms(query)
	for i, m := range results {
		if i > 0 {
			b.WriteString("\n")
		}
		who := "you"
		if m.role == "assistant" {
			who = "claude"
		}
		header := fmt.Sprintf("%s  %-6s  %s", m.createdAt.Local().Format("2006-01-02 15:04"), who, m.sessionID)
		if showCwd && m.cwd != "" {
			header += "  " + filepath.Base(m.cwd)
		}
		if color {
			header = legendStyle.Render(header)
		}
		b.WriteString(header + "\n")
		b.WriteString("  " + snippet(m.text, terms, snippetRunes, color) + "\n")
	}
	return b.String()
}

// searchTerms are the query's terms (see queryTerms), folded the way the
// index folds text, for snippet to look for.
func searchTerms(query string) [][]rune {
	var terms [][]rune
	for _, t := range queryTerms(query) {
		folded := foldRunes(t)
		if len(folded) > 0 {
			terms = append(terms, folded)
		}
	}
	return terms
}

func foldRunes(s string) []rune {
	r := []rune(s)
	for i := range r {
		r[i] = normalizeRune(r[i])
	}
	return r
}

// snippet returns about width runes of text on one line, centred on the
// first occurrence of any of terms (the start of text if none occurs, as
// for a match on meaning alone), with the occurrences highlighted when
// color is set.
func snippet(text string, terms [][]rune, width int, color bool) string {
	src := []rune(strings.Join(strings.FieldsFunc(text, unicode.IsSpace), " "))
	folded := make([]rune, len(src))
	for i, r := range src {
		folded[i] = normalizeRune(r)
	}

	// mark every rune covered by a term
	hit := make([]bool, len(src))
	first := -1
	for _, t := range terms {
		for i := 0; i+len(t) <= len(folded); i++ {
			if string(folded[i:i+len(t)]) != string(t) {
				continue
			}
			for j := i; j < i+len(t); j++ {
				hit[j] = true
			}
			if first < 0 || i < first {
				first = i
			}
		}
	}

	start := 0
	if first > width/3 {
		start = first - width/3
	}
	end := min(start+width, len(src))
	if end-start < width {
		start = max(0, end-width)
	}

	var b strings.Builder
	if start > 0 {
		b.WriteString("…")
	}
	for i := start; i < end; {
		j := i
		for j < end && hit[j] == hit[i] {
			j++
		}
		part := string(src[i:j])
		if hit[i] && color {
			part = searchHitStyle.Render(part)
		}
		b.WriteString(part)
		i = j
	}
	if end < len(src) {
		b.WriteString("…")
	}
	return b.String()
}
