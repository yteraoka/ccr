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
	"time"
)

// embeddingMaxRunes caps how much of a text is sent to be embedded. Models
// accept a few thousand tokens at most and some servers reject longer input
// rather than truncating it; the start of a long answer says enough about
// what it is about.
const embeddingMaxRunes = 6000

// embeddingTimeout bounds one request to the embeddings endpoint.
const embeddingTimeout = 60 * time.Second

// embedder turns texts into vectors through an embeddingConfig's endpoint.
type embedder struct {
	cfg    embeddingConfig
	client *http.Client
}

func newEmbedder(cfg *embeddingConfig) (*embedder, error) {
	if cfg == nil {
		return nil, nil
	}
	if cfg.URL == "" || cfg.Model == "" {
		return nil, fmt.Errorf("history.embedding needs both url and model")
	}
	return &embedder{cfg: *cfg, client: &http.Client{Timeout: embeddingTimeout}}, nil
}

// embedDocuments embeds texts to be stored.
func (e *embedder) embedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	return e.embed(ctx, e.cfg.DocumentPrefix, texts)
}

// embedQuery embeds a search query.
func (e *embedder) embedQuery(ctx context.Context, query string) ([]float32, error) {
	vecs, err := e.embed(ctx, e.cfg.QueryPrefix, []string{query})
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
