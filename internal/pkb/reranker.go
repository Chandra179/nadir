package pkb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"
)

// Reranker scores (query, passage) pairs and returns chunks sorted by relevance.
// Implementations: HTTPReranker (cross-encoder sidecar), nil (disabled).
type Reranker interface {
	Rerank(ctx context.Context, query string, chunks []ScoredChunk) ([]ScoredChunk, error)
}

// HTTPReranker calls a cross-encoder sidecar at POST /rerank.
// Expected sidecar: cmd/reranker/main.py (cross-encoder/ms-marco-MiniLM-L-6-v2).
// Protocol:
//
//	request:  {"query": "...", "passages": ["text1", ...]}
//	response: {"scores": [0.95, -2.3, ...]}  — parallel to passages, higher = more relevant
type HTTPReranker struct {
	addr   string
	client *http.Client
}

type rerankRequest struct {
	Query    string   `json:"query"`
	Passages []string `json:"passages"`
}

type rerankResponse struct {
	Scores []float32 `json:"scores"`
}

func NewHTTPReranker(addr string) *HTTPReranker {
	return &HTTPReranker{
		addr: addr,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (r *HTTPReranker) Rerank(ctx context.Context, query string, chunks []ScoredChunk) ([]ScoredChunk, error) {
	if len(chunks) == 0 {
		return chunks, nil
	}

	passages := make([]string, len(chunks))
	for i, c := range chunks {
		text := c.WindowText
		if text == "" {
			text = c.Text
		}
		passages[i] = text
	}

	body, _ := json.Marshal(rerankRequest{Query: query, Passages: passages})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.addr+"/rerank", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("reranker build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reranker call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("reranker status %d", resp.StatusCode)
	}

	var rrResp rerankResponse
	if err := json.NewDecoder(resp.Body).Decode(&rrResp); err != nil {
		return nil, fmt.Errorf("reranker decode: %w", err)
	}
	if len(rrResp.Scores) != len(chunks) {
		return nil, fmt.Errorf("reranker score count mismatch: got %d, want %d", len(rrResp.Scores), len(chunks))
	}

	reranked := make([]ScoredChunk, len(chunks))
	copy(reranked, chunks)
	for i := range reranked {
		reranked[i].Score = rrResp.Scores[i]
	}
	sort.Slice(reranked, func(i, j int) bool { return reranked[i].Score > reranked[j].Score })
	return reranked, nil
}
