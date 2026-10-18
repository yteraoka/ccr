package ccr

import (
	"unicode"

	"github.com/ncruces/go-sqlite3/ext/fts5"
	"golang.org/x/text/width"
)

// historyTokenizerName is the FTS5 tokenizer the history index is built
// with. It is registered on every connection openHistoryDB makes; a
// connection without it (the sqlite3 shell, say) cannot query the index.
const historyTokenizerName = "ccr_ja"

// jaTokenizer is an FTS5 tokenizer that copes with Japanese, which has no
// spaces between words for the stock unicode61 tokenizer to split on (it
// would index a whole sentence as one token). Runs of CJK characters are
// indexed as overlapping bigrams instead, so any substring of two or more
// characters is found as a phrase of consecutive bigrams; other letters
// and digits are indexed as lowercase words, as unicode61 would.
//
// Bigrams rather than trigram (FTS5's built-in alternative) because a great
// many Japanese words are two characters long, which trigram cannot find.
type jaTokenizer struct{}

func (jaTokenizer) Tokenize(flags fts5.TokenizeFlag, text, _ string, emit fts5.TokenCallback) error {
	return tokenizeJA(text, flags&fts5.TOKENIZE_QUERY != 0, func(token string, start, end int) error {
		return emit(0, token, start, end)
	})
}

type charClass int

const (
	classOther charClass = iota
	classWord
	classCJK
)

// classifyRune says how r takes part in a token. r is expected already
// folded by normalizeRune.
func classifyRune(r rune) charClass {
	switch {
	case unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul),
		r == 'ー', r == '々', r == '〆', r == 'ヶ':
		return classCJK
	case unicode.IsLetter(r), unicode.IsDigit(r), unicode.IsMark(r), r == '_':
		return classWord
	}
	return classOther
}

// normalizeRune folds the variants a search should not tell apart:
// full-width ASCII to plain ASCII, half-width katakana to full-width, and
// case. It maps one rune to one rune, so offsets into the folded text line
// up with the original's.
func normalizeRune(r rune) rune {
	if f := width.LookupRune(r).Folded(); f != 0 {
		r = f
	}
	return unicode.ToLower(r)
}

// tokenizeJA splits text into the tokens jaTokenizer indexes, calling emit
// with each token and its byte range in text.
//
// A CJK run of one character is indexed as that character. A longer run is
// indexed as its bigrams, and in a document (query false) also its last
// character, so a single-character prefix query ("検"*) finds every
// occurrence, the run's last character included. A query leaves that last
// character out: "検索" must become the single bigram, or the phrase would
// demand a lone 索 right after it.
func tokenizeJA(text string, query bool, emit func(token string, start, end int) error) error {
	var (
		runes []rune // the folded run being collected
		offs  []int  // byte offset in text of each rune in runes
		kind  charClass
		// runEnd is where the run being flushed stops in text, set right
		// before every flush.
		runEnd int
	)
	end := func(i int) int { // byte offset just past runes[i]
		if i+1 < len(offs) {
			return offs[i+1]
		}
		return runEnd
	}
	flush := func() error {
		defer func() { runes, offs = runes[:0], offs[:0] }()
		switch {
		case len(runes) == 0:
			return nil
		case kind == classWord:
			return emit(string(runes), offs[0], end(len(runes)-1))
		case len(runes) == 1:
			return emit(string(runes), offs[0], end(0))
		}
		for i := 0; i+1 < len(runes); i++ {
			if err := emit(string(runes[i:i+2]), offs[i], end(i+1)); err != nil {
				return err
			}
		}
		if !query {
			last := len(runes) - 1
			return emit(string(runes[last]), offs[last], end(last))
		}
		return nil
	}

	for i, r := range text {
		n := normalizeRune(r)
		c := classifyRune(n)
		if c != kind || c == classOther {
			runEnd = i
			if err := flush(); err != nil {
				return err
			}
			kind = c
		}
		if c != classOther {
			runes = append(runes, n)
			offs = append(offs, i)
		}
	}
	runEnd = len(text)
	return flush()
}
