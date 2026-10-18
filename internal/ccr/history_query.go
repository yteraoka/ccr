package ccr

import (
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/ikawaha/kagome-dict/ipa"
	"github.com/ikawaha/kagome/v2/tokenizer"
)

// queryAnalyzer is the morphological analyzer queryTerms splits Japanese
// with. Loading its dictionary takes about a second, so it is built only
// for the first query that has Japanese in it.
var queryAnalyzer = sync.OnceValues(func() (*tokenizer.Tokenizer, error) {
	return tokenizer.New(ipa.Dict(), tokenizer.OmitBosEos())
})

// queryTerms splits what a person typed into the terms keyword search
// requires, each to be matched as a phrase (a substring, given the bigram
// index):
//
//   - text in double quotes is one term, as written;
//   - other whitespace-separated words without Japanese are terms as they
//     are;
//   - Japanese is cut into words by kagome, keeping only the ones that
//     carry meaning, so 日本語の検索 asks for 日本語 and 検索 rather than
//     for that exact string. See contentTerms.
func queryTerms(q string) []string {
	var terms []string
	for i, part := range strings.Split(q, `"`) {
		if i%2 == 1 { // inside quotes
			if strings.TrimSpace(part) != "" {
				terms = append(terms, part)
			}
			continue
		}
		for _, word := range strings.Fields(part) {
			if !hasCJK(word) {
				terms = append(terms, word)
				continue
			}
			terms = append(terms, contentTerms(word)...)
		}
	}
	return terms
}

func hasCJK(s string) bool {
	for _, r := range s {
		if classifyRune(normalizeRune(r)) == classCJK {
			return true
		}
	}
	return false
}

// lightVerbs are verbs that say next to nothing on their own: 対応させたい
// is about 対応, not about する.
var lightVerbs = map[string]bool{"する": true, "できる": true, "ある": true, "いる": true, "なる": true, "おる": true}

// contentTerms analyzes word (Japanese, no spaces) and returns its content
// words. Particles, auxiliaries, pronouns and the like are dropped; runs of
// nouns (and prefixes) stay together as one term, so 全文検索 or 東京都 is
// still looked for as a whole. A verb or adjective becomes its stem (変えたい
// → 変え), the part every conjugation of it shares, except where it is the
// first half of a compound noun (埋め込みモデル); one whose stem is a single
// character (行きたい → 行) is dropped, as that would match far too much. If
// nothing is left, or kagome cannot be loaded, word itself is the term.
func contentTerms(word string) []string {
	t, err := queryAnalyzer()
	if err != nil {
		return []string{word}
	}
	tokens := t.Tokenize(word)

	var terms []string
	var span strings.Builder
	flush := func() {
		if span.Len() > 0 {
			terms = append(terms, span.String())
			span.Reset()
		}
	}
	for i, tok := range tokens {
		f := tok.Features()
		switch {
		case isNounLike(f):
			span.WriteString(tok.Surface)
		case isPredicate(f):
			if feature(f, 5) == "連用形" && i+1 < len(tokens) && isNounLike(tokens[i+1].Features()) {
				span.WriteString(tok.Surface) // 埋め込み + モデル
				continue
			}
			flush()
			if s := stem(tok.Surface, feature(f, 6)); utf8.RuneCountInString(s) > 1 {
				terms = append(terms, s)
			}
		default:
			flush()
		}
	}
	flush()
	if len(terms) == 0 {
		return []string{word}
	}
	return terms
}

// isNounLike reports whether IPA features f describe a noun worth searching
// for, or a prefix to one. Words kagome does not know (English, code,
// symbols inside identifiers) come out as nouns too, which keeps foo.bar
// or ccr_ja whole.
func isNounLike(f []string) bool {
	switch feature(f, 0) {
	case "名詞":
		sub := feature(f, 1)
		return sub != "代名詞" && sub != "非自立"
	case "接頭詞":
		return true
	}
	return false
}

// isPredicate reports whether IPA features f describe a verb or adjective
// that means something by itself.
func isPredicate(f []string) bool {
	switch feature(f, 0) {
	case "動詞", "形容詞":
		return feature(f, 1) == "自立" && !lightVerbs[feature(f, 6)]
	}
	return false
}

func feature(f []string, i int) string {
	if i < len(f) {
		return f[i]
	}
	return ""
}

// stem returns the part of a conjugated word that its dictionary form
// shares: 変え/変える → 変え, 早く/早い → 早. A word already in dictionary
// form loses its last kana (書く → 書), which is what changes when it
// conjugates.
func stem(surface, base string) string {
	if base == "" || base == "*" {
		return surface
	}
	s, b := []rune(surface), []rune(base)
	n := 0
	for n < len(s) && n < len(b) && s[n] == b[n] {
		n++
	}
	if n == len(s) && n == len(b) && n > 1 {
		n--
	}
	if n == 0 {
		return surface
	}
	return string(s[:n])
}

// ftsQuery turns what a person typed into an FTS5 MATCH expression: every
// term of queryTerms must appear (as a phrase, so a Japanese term matches
// as a substring), and a term that is a single CJK character matches as a
// prefix (see tokenizeJA). Terms with nothing searchable in them (bare
// punctuation) are dropped; "" means there is nothing to search.
func ftsQuery(q string) string {
	var parts []string
	for _, term := range queryTerms(q) {
		var tokens []string
		_ = tokenizeJA(term, true, func(token string, _, _ int) error {
			tokens = append(tokens, token)
			return nil
		})
		if len(tokens) == 0 {
			continue
		}
		part := `"` + strings.ReplaceAll(term, `"`, `""`) + `"`
		if len(tokens) == 1 && utf8.RuneCountInString(tokens[0]) == 1 && classifyRune([]rune(tokens[0])[0]) == classCJK {
			part += "*"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, " ")
}
