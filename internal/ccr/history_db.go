package ccr

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/ncruces/go-sqlite3"
	"github.com/ncruces/go-sqlite3/driver"
	"github.com/ncruces/go-sqlite3/ext/fts5"
	"github.com/ncruces/go-sqlite3/ext/vec1"
)

// historyDBPath returns where the history database lives: $CCR_HISTORY_DB,
// else the config file's history.db, else
// ${XDG_DATA_HOME:-$HOME/.local/share}/ccr/history.db.
func historyDBPath(cfg historyConfig) (string, error) {
	if v := os.Getenv("CCR_HISTORY_DB"); v != "" {
		return v, nil
	}
	if cfg.DB != "" {
		p := expandPath(cfg.DB)
		if !filepath.IsAbs(p) {
			return "", fmt.Errorf("history.db must be an absolute path: %q", cfg.DB)
		}
		return p, nil
	}
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "ccr", "history.db"), nil
}

const historySchema = `
CREATE TABLE IF NOT EXISTS messages (
	id         INTEGER PRIMARY KEY,
	session_id TEXT NOT NULL,
	role       TEXT NOT NULL, -- "user" or "assistant"
	text       TEXT NOT NULL,
	cwd        TEXT NOT NULL DEFAULT '',
	config_dir TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL, -- RFC 3339, UTC
	embedded   INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS messages_pending ON messages(id) WHERE embedded = 0;
CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
	text, content='messages', content_rowid='id', tokenize='` + historyTokenizerName + `'
);
CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
`

// historyDB is the prompt/answer history: every message's text, a
// full-text index over it, and (once an embedding model is configured) a
// vec1 table of their vectors keyed by the same id.
type historyDB struct {
	db *sql.DB
}

// openHistoryDB opens (creating it if need be) the database at path.
// WAL and a busy timeout let the hooks of several Claude Code sessions
// write at once.
func openHistoryDB(path string) (*historyDB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	dsn := (&url.URL{Scheme: "file", OmitHost: true, Path: filepath.ToSlash(path), RawQuery: url.Values{
		"_pragma": {"busy_timeout(10000)", "journal_mode(wal)"},
		"_txlock": {"immediate"},
	}.Encode()}).String()
	db, err := driver.Open(dsn, registerHistoryExtensions)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(historySchema); err != nil {
		db.Close() //nolint:errcheck
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &historyDB{db: db}, nil
}

func registerHistoryExtensions(c *sqlite3.Conn) error {
	if err := fts5.RegisterCustom(c, func(api *fts5.API) error {
		return api.CreateTokenizer(historyTokenizerName, func([]string) (fts5.Tokenizer, error) {
			return jaTokenizer{}, nil
		})
	}); err != nil {
		return err
	}
	return vec1.Register(c)
}

func (h *historyDB) Close() error { return h.db.Close() }

// historyMessage is one recorded prompt or answer.
type historyMessage struct {
	id        int64
	sessionID string
	role      string
	text      string
	cwd       string
	configDir string
	createdAt time.Time
}

// add records m and indexes its text, returning its id.
func (h *historyDB) add(m historyMessage) (int64, error) {
	tx, err := h.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck
	res, err := tx.Exec(`INSERT INTO messages(session_id, role, text, cwd, config_dir, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		m.sessionID, m.role, m.text, m.cwd, m.configDir, m.createdAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`INSERT INTO messages_fts(rowid, text) VALUES (?, ?)`, id, m.text); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

const (
	metaEmbeddingModel = "embedding_model"
	// historyVecTable is created on the first vector stored, because vec1
	// takes its dimension from that vector.
	historyVecTable = "messages_vec"
)

// prepareVectors makes sure the stored vectors came from the embedding
// configuration identified by key (see embedder.key), dropping them all
// (to be embedded again) when it changed.
func (h *historyDB) prepareVectors(key string) error {
	var stored string
	err := h.db.QueryRow(`SELECT value FROM meta WHERE key = ?`, metaEmbeddingModel).Scan(&stored)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if stored == key {
		return nil
	}
	tx, err := h.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	for _, q := range []string{
		`DROP TABLE IF EXISTS ` + historyVecTable,
		`UPDATE messages SET embedded = 0 WHERE embedded != 0`,
	} {
		if _, err := tx.Exec(q); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO meta(key, value) VALUES (?, ?)`, metaEmbeddingModel, key); err != nil {
		return err
	}
	return tx.Commit()
}

// hasVectors reports whether the vector table exists yet.
func (h *historyDB) hasVectors() (bool, error) {
	var n int
	err := h.db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE name = ?`, historyVecTable).Scan(&n)
	return n > 0, err
}

// storeVectors saves the vectors of the messages ids, in that order.
func (h *historyDB) storeVectors(ids []int64, vecs [][]float32) error {
	tx, err := h.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	var n int
	if err := tx.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE name = ?`, historyVecTable).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		// flat is an exact, exhaustive search: a personal history is small
		// enough for it, and it needs no training, unlike ivfpq
		for _, q := range []string{
			`CREATE VIRTUAL TABLE ` + historyVecTable + ` USING vec1`,
			`INSERT INTO ` + historyVecTable + `(cmd, vector) VALUES ('rebuild', '{index:"flat", distance:"cos"}')`,
		} {
			if _, err := tx.Exec(q); err != nil {
				return err
			}
		}
	}
	for i, id := range ids {
		// a hook and a search can both pick up the same pending message
		if _, err := tx.Exec(`DELETE FROM `+historyVecTable+` WHERE rowid = ?`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO `+historyVecTable+`(rowid, vector) VALUES (?, ?)`, id, vectorBlob(vecs[i])); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE messages SET embedded = 1 WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// pending returns up to limit messages that have no vector yet.
func (h *historyDB) pending(limit int) ([]historyMessage, error) {
	return h.queryMessages(`SELECT id, session_id, role, text, cwd, config_dir, created_at FROM messages WHERE embedded = 0 ORDER BY id LIMIT ?`, limit)
}

// pendingCount is how many messages have no vector yet.
func (h *historyDB) pendingCount() (int, error) {
	var n int
	err := h.db.QueryRow(`SELECT count(*) FROM messages WHERE embedded = 0`).Scan(&n)
	return n, err
}

// embedPending embeds every message without a vector, batch by batch,
// calling progress after each batch with how many are done of how many.
func (h *historyDB) embedPending(ctx context.Context, e *embedder, progress func(done, total int)) error {
	total, err := h.pendingCount()
	if err != nil || total == 0 {
		return err
	}
	done := 0
	for {
		batch, err := h.pending(32)
		if err != nil {
			return err
		}
		if len(batch) == 0 {
			return nil
		}
		texts := make([]string, len(batch))
		ids := make([]int64, len(batch))
		for i, m := range batch {
			texts[i], ids[i] = m.text, m.id
		}
		vecs, err := e.embedDocuments(ctx, texts)
		if err != nil {
			return err
		}
		if err := h.storeVectors(ids, vecs); err != nil {
			return err
		}
		done += len(batch)
		if progress != nil {
			progress(min(done, total), total)
		}
	}
}

// historyFilter narrows a search. Empty fields match everything.
type historyFilter struct {
	role string
	cwd  string
}

func (f historyFilter) where(alias string) (string, []any) {
	clause, args := "", []any(nil)
	if f.role != "" {
		clause += ` AND ` + alias + `.role = ?`
		args = append(args, f.role)
	}
	if f.cwd != "" {
		clause += ` AND ` + alias + `.cwd = ?`
		args = append(args, f.cwd)
	}
	return clause, args
}

// keywordSearch returns the ids of the best BM25 matches for match (an
// FTS5 expression from ftsQuery), best first.
func (h *historyDB) keywordSearch(match string, f historyFilter, limit int) ([]int64, error) {
	clause, args := f.where("m")
	rows, err := h.db.Query(`SELECT m.id FROM messages_fts JOIN messages m ON m.id = messages_fts.rowid
		WHERE messages_fts MATCH ?`+clause+` ORDER BY bm25(messages_fts) LIMIT ?`,
		append(append([]any{match}, args...), limit)...)
	if err != nil {
		return nil, err
	}
	return scanIDs(rows)
}

// vectorSearch returns the ids of the messages nearest to vec, nearest
// first. vec1 ranks the whole table, so it is asked for more neighbours
// than needed and the filter is applied to those.
func (h *historyDB) vectorSearch(vec []float32, f historyFilter, limit int) ([]int64, error) {
	ok, err := h.hasVectors()
	if err != nil || !ok {
		return nil, err
	}
	k := limit
	if f != (historyFilter{}) {
		k = max(limit*20, 500)
	}
	clause, args := f.where("m")
	// vec1 takes the query vector as its table argument and needs a
	// literal K in the LIMIT
	rows, err := h.db.Query(`SELECT m.id FROM (SELECT rowid, distance FROM `+historyVecTable+`(?) LIMIT `+strconv.Itoa(k)+`) v
		JOIN messages m ON m.id = v.rowid WHERE 1`+clause+` ORDER BY v.distance LIMIT ?`,
		append(append([]any{vectorBlob(vec)}, args...), limit)...)
	if err != nil {
		return nil, err
	}
	return scanIDs(rows)
}

func scanIDs(rows *sql.Rows) ([]int64, error) {
	defer rows.Close() //nolint:errcheck
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// messages loads the messages ids, in the order given.
func (h *historyDB) messages(ids []int64) ([]historyMessage, error) {
	byID := make(map[int64]historyMessage, len(ids))
	for _, id := range ids {
		ms, err := h.queryMessages(`SELECT id, session_id, role, text, cwd, config_dir, created_at FROM messages WHERE id = ?`, id)
		if err != nil {
			return nil, err
		}
		for _, m := range ms {
			byID[m.id] = m
		}
	}
	out := make([]historyMessage, 0, len(ids))
	for _, id := range ids {
		if m, ok := byID[id]; ok {
			out = append(out, m)
		}
	}
	return out, nil
}

func (h *historyDB) queryMessages(q string, args ...any) ([]historyMessage, error) {
	rows, err := h.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck
	var out []historyMessage
	for rows.Next() {
		var m historyMessage
		var created string
		if err := rows.Scan(&m.id, &m.sessionID, &m.role, &m.text, &m.cwd, &m.configDir, &created); err != nil {
			return nil, err
		}
		m.createdAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, m)
	}
	return out, rows.Err()
}

// rrfK damps reciprocal rank fusion so that being first in one ranking
// does not outweigh being near the top of both.
const rrfK = 60

// fuseRankings merges ranked id lists by reciprocal rank fusion: each id
// scores the sum of 1/(rrfK+rank) over the lists it appears in. BM25 scores
// and cosine distances are not on comparable scales, but ranks are.
func fuseRankings(lists ...[]int64) []int64 {
	score := make(map[int64]float64)
	var order []int64
	for _, list := range lists {
		for rank, id := range list {
			if _, ok := score[id]; !ok {
				order = append(order, id)
			}
			score[id] += 1 / float64(rrfK+rank+1)
		}
	}
	sort.SliceStable(order, func(i, j int) bool { return score[order[i]] > score[order[j]] })
	return order
}
