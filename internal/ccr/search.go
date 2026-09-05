package ccr

import "strings"

// incrementalSearch is the "/" filter shared by the session list and the
// jsonl index. While a query is set the list shows only the rows matching
// it; while it is being typed the prompt takes the keys.
//
// Typing and filtering are separate: Enter stops typing but keeps the
// filter, so the list can be moved through and acted on with the ordinary
// keys while it is still narrowed. Esc drops the query altogether.
type incrementalSearch struct {
	typing bool
	query  string
}

// begin opens the prompt on a fresh query.
func (s *incrementalSearch) begin() {
	*s = incrementalSearch{typing: true}
}

// clear drops the query and closes the prompt.
func (s *incrementalSearch) clear() {
	*s = incrementalSearch{}
}

// filtering reports whether the list is narrowed to the query.
func (s incrementalSearch) filtering() bool { return s.query != "" }

// showsPrompt reports whether the prompt line belongs on screen: while it
// is being typed, and afterwards to say the list is still filtered.
func (s incrementalSearch) showsPrompt() bool { return s.typing || s.filtering() }

// prompt is the line shown while searching or filtering.
func (s incrementalSearch) prompt(matched, total int) string {
	if !s.showsPrompt() {
		return ""
	}
	line := "/" + s.query
	if !s.typing {
		line += " ⏎"
	}
	switch {
	case !s.filtering():
	case matched == 0:
		line += "   (no match, esc to clear)"
	default:
		line += "   " + itoa(matched) + "/" + itoa(total) + "   (esc to clear)"
	}
	return line
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// key applies one keypress to the search. changed says the query moved, so
// the caller has to rebuild its filtered rows; handled says the search took
// the key, and the caller should not also treat it as a command.
//
// text carries the printable characters of the keypress, which is how the
// query is typed. Keys that type nothing — the arrows, page keys — are left
// unhandled so they still move the cursor through the filtered list.
func (s *incrementalSearch) key(key, text string) (changed, handled bool) {
	if !s.typing {
		return false, false
	}
	switch key {
	case "esc", "ctrl+c":
		had := s.filtering()
		s.clear()
		return had, true
	case "enter":
		// keep the filter, hand the keys back to the list
		s.typing = false
		return false, true
	case "backspace":
		if q := []rune(s.query); len(q) > 0 {
			s.query = string(q[:len(q)-1])
			return true, true
		}
		return false, true
	}
	if text != "" {
		s.query += text
		return true, true
	}
	return false, false
}

// filter returns the rows matching the query, as indices into the full
// list. Everything matches when there is no query.
func (s incrementalSearch) filter(rows int, at func(int) string) []int {
	matches := make([]int, 0, rows)
	needle := strings.ToLower(s.query)
	for i := 0; i < rows; i++ {
		if needle == "" || strings.Contains(strings.ToLower(at(i)), needle) {
			matches = append(matches, i)
		}
	}
	return matches
}
