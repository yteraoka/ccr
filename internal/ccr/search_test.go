package ccr

import "testing"

// rowsAt stands in for a list being filtered.
func rowsAt(items ...string) (int, func(int) string) {
	return len(items), func(i int) string { return items[i] }
}

func (s incrementalSearch) matched(items ...string) []string {
	n, at := rowsAt(items...)
	var out []string
	for _, i := range s.filter(n, at) {
		out = append(out, items[i])
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSearchNarrowsToMatchesAsItIsTyped(t *testing.T) {
	items := []string{"alpha", "bravo", "charlie", "chart"}
	var s incrementalSearch
	s.begin()

	for _, c := range []string{"c", "h"} {
		if changed, handled := s.key(c, c); !changed || !handled {
			t.Fatalf("typing %q: changed=%v handled=%v, want both true", c, changed, handled)
		}
	}
	if got, want := s.matched(items...), []string{"charlie", "chart"}; !equal(got, want) {
		t.Errorf("matches = %v, want %v", got, want)
	}

	// backspace widens it again
	s.key("backspace", "")
	if got, want := s.matched(items...), []string{"charlie", "chart"}; !equal(got, want) {
		t.Errorf("after backspace = %v, want everything matching %q", got, want)
	}
	s.key("backspace", "")
	if got := s.matched(items...); len(got) != len(items) {
		t.Errorf("an empty query matched %d rows, want all %d", len(got), len(items))
	}
}

func TestSearchIsCaseInsensitive(t *testing.T) {
	var s incrementalSearch
	s.begin()
	s.key("b", "b")
	if got, want := s.matched("Alpha", "BRAVO"), []string{"BRAVO"}; !equal(got, want) {
		t.Errorf("matches = %v, want %v", got, want)
	}
}

func TestSearchEnterKeepsTheFilterAndEscapeDropsIt(t *testing.T) {
	var accept incrementalSearch
	accept.begin()
	accept.key("a", "a")
	changed, handled := accept.key("enter", "")
	if !handled || changed {
		t.Errorf("enter: changed=%v handled=%v, want handled without changing the query", changed, handled)
	}
	if accept.typing {
		t.Error("enter should stop the typing")
	}
	// the list stays narrowed, so the ordinary keys work on the matches
	if !accept.filtering() || !accept.showsPrompt() {
		t.Error("enter should keep the filter and its prompt")
	}
	// and the keys go back to the list rather than the query
	if _, handled := accept.key("j", "j"); handled {
		t.Error("keys after enter belong to the list, not the query")
	}

	var cancel incrementalSearch
	cancel.begin()
	cancel.key("a", "a")
	if changed, handled := cancel.key("esc", ""); !handled || !changed {
		t.Errorf("esc: changed=%v handled=%v, want both true", changed, handled)
	}
	if cancel.filtering() || cancel.showsPrompt() || cancel.typing {
		t.Errorf("esc should drop the search entirely: %+v", cancel)
	}
}

func TestSearchPromptReportsWhatIsShowing(t *testing.T) {
	var s incrementalSearch
	if s.prompt(0, 0) != "" {
		t.Error("there is no prompt when no search is running")
	}

	s.begin()
	s.key("a", "a")
	if got, want := s.prompt(3, 10), "/a   3/10   (esc to clear)"; got != want {
		t.Errorf("prompt = %q, want %q", got, want)
	}
	if got, want := s.prompt(0, 10), "/a   (no match, esc to clear)"; got != want {
		t.Errorf("prompt with no matches = %q, want %q", got, want)
	}

	// once accepted it says so, so a narrowed list never looks like the
	// whole one
	s.key("enter", "")
	if got := s.prompt(3, 10); got != "/a ⏎   3/10   (esc to clear)" {
		t.Errorf("prompt after enter = %q, want it marked as applied", got)
	}
}

// Keys that type nothing are left for the list, so the cursor still moves
// while a query is being typed.
func TestSearchLeavesMovementKeysToTheList(t *testing.T) {
	var s incrementalSearch
	s.begin()
	for _, key := range []string{"up", "down", "pgup", "pgdown", "home", "end"} {
		if changed, handled := s.key(key, ""); handled || changed {
			t.Errorf("%q: changed=%v handled=%v, want it left to the list", key, changed, handled)
		}
	}
	if s.query != "" {
		t.Errorf("query = %q, want the movement keys kept out of it", s.query)
	}

	// but the keys that are movement on the list are text while typing
	for _, c := range []string{"n", "p", "q", "i"} {
		if _, handled := s.key(c, c); !handled {
			t.Errorf("%q should be typed into the query while searching", c)
		}
	}
	if s.query != "npqi" {
		t.Errorf("query = %q, want %q", s.query, "npqi")
	}
	// space is typed text too
	s.key("space", " ")
	if s.query != "npqi " {
		t.Errorf("query = %q, want the space that was typed", s.query)
	}
}

func TestSearchWithNoQueryMatchesEverything(t *testing.T) {
	var s incrementalSearch
	if got := s.matched("a", "b", "c"); len(got) != 3 {
		t.Errorf("matched %d rows with no query, want all 3", len(got))
	}
	if s.filtering() {
		t.Error("an empty query is not a filter")
	}
}
