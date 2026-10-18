package ccr

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// embeddingMaxRunes caps how much of a text is sent to be embedded. Models
// accept a few thousand tokens at most and some servers reject longer input
// rather than truncating it; the start of a long answer says enough about
// what it is about.
const embeddingMaxRunes = 6000

// defaultEmbeddingTimeout bounds one request to the embeddings endpoint
// unless the config sets its own. It is generous because a local model on
// the CPU (Ruri v3 by default) takes a while over a batch of long answers,
// and a request that times out wastes the work done on it.
const defaultEmbeddingTimeout = 5 * time.Minute

// embeddingBatchSize is how many messages go into one request when
// catching up on messages without a vector, small enough that a batch of
// long answers on a CPU stays well inside the timeout.
const embeddingBatchSize = 8

// The embedding model used when the config file names none: Ruri v3, a
// Japanese embedding model, at the address scripts/ruri-embedding-server.py
// (and llama.cpp's llama-server) listens on by default.
const (
	defaultEmbeddingURL   = "http://127.0.0.1:8080/v1/embeddings"
	defaultEmbeddingModel = "cl-nagoya/ruri-v3-310m"
)

// Ruri v3 is trained with a prefix marking what a text is for, and
// retrieves noticeably worse without it.
const (
	ruriV3QueryPrefix    = "検索クエリ: "
	ruriV3DocumentPrefix = "検索文書: "
)

// isRuriV3 reports whether model names a Ruri v3 embedding model, however
// it is served ("cl-nagoya/ruri-v3-310m", "ruri-v3-130m", a GGUF file...).
func isRuriV3(model string) bool {
	return strings.Contains(strings.ToLower(model), "ruri-v3")
}

// embedder turns texts into vectors through an embeddingConfig's endpoint.
type embedder struct {
	cfg            embeddingConfig
	queryPrefix    string
	documentPrefix string
	client         *http.Client
}

// newEmbedder returns nil (no vector search) when cfg is nil, and otherwise
// fills in what cfg leaves out: Ruri v3 at defaultEmbeddingURL, and the
// model's own prefixes.
func newEmbedder(cfg *embeddingConfig) *embedder {
	if cfg == nil {
		return nil
	}
	timeout := defaultEmbeddingTimeout
	if cfg.Timeout > 0 {
		timeout = time.Duration(cfg.Timeout) * time.Second
	}
	e := &embedder{cfg: *cfg, client: &http.Client{Timeout: timeout}}
	if e.cfg.URL == "" {
		e.cfg.URL = defaultEmbeddingURL
	}
	if e.cfg.Model == "" {
		e.cfg.Model = defaultEmbeddingModel
	}
	if isRuriV3(e.cfg.Model) {
		e.queryPrefix, e.documentPrefix = ruriV3QueryPrefix, ruriV3DocumentPrefix
	}
	if cfg.QueryPrefix != nil {
		e.queryPrefix = *cfg.QueryPrefix
	}
	if cfg.DocumentPrefix != nil {
		e.documentPrefix = *cfg.DocumentPrefix
	}
	return e
}

// key identifies the vectors e produces: vectors from different models,
// sizes or document prefixes are not comparable.
func (e *embedder) key() string {
	return e.cfg.Model + "@" + e.cfg.URL + "#" + strconv.Itoa(e.cfg.Dimensions) + "|" + e.documentPrefix
}

// embedDocuments embeds texts to be stored.
func (e *embedder) embedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	return e.embed(ctx, e.documentPrefix, texts)
}

// embedQuery embeds a search query.
func (e *embedder) embedQuery(ctx context.Context, query string) ([]float32, error) {
	vecs, err := e.embed(ctx, e.queryPrefix, []string{query})
	if err != nil {
		return nil, err
	}
	return vecs[0], nil
}

func (e *embedder) embed(ctx context.Context, prefix string, texts []string) ([][]float32, error) {
	input := make([]string, len(texts))
	for i, t := range texts {
		if r := []rune(t); len(r) > embeddingMaxRunes {
			t = string(r[:embeddingMaxRunes])
		}
		input[i] = prefix + t
	}
	req := struct {
		Model      string   `json:"model"`
		Input      []string `json:"input"`
		Dimensions int      `json:"dimensions,omitempty"`
	}{e.cfg.Model, input, e.cfg.Dimensions}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, e.cfg.URL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if e.cfg.APIKeyEnv != "" {
		key := os.Getenv(e.cfg.APIKeyEnv)
		if key == "" {
			return nil, fmt.Errorf("embedding: %s is not set", e.cfg.APIKeyEnv)
		}
		httpReq.Header.Set("Authorization", "Bearer "+key)
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("embedding: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	data, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
	if err != nil {
		return nil, fmt.Errorf("embedding: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedding: %s: %s", resp.Status, bytes.TrimSpace(data[:min(len(data), 300)]))
	}

	var out struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("embedding: decoding response: %w", err)
	}
	if len(out.Data) != len(texts) {
		return nil, fmt.Errorf("embedding: got %d vectors for %d inputs", len(out.Data), len(texts))
	}
	vecs := make([][]float32, len(texts))
	for i, d := range out.Data {
		idx := d.Index
		if idx < 0 || idx >= len(texts) || vecs[idx] != nil {
			idx = i // a server that leaves index out (all zero) answers in order
		}
		if len(d.Embedding) == 0 {
			return nil, fmt.Errorf("embedding: empty vector in response")
		}
		vecs[idx] = d.Embedding
	}
	return vecs, nil
}

// vectorBlob encodes v the way vec1 stores vectors: little-endian float32s.
func vectorBlob(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(f))
	}
	return b
}
