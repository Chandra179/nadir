package reranker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"nadir/internal/adapters/qdrant/documents"
	"nadir/internal/platform/observability"

	"go.uber.org/zap"
)

type rerankRequest struct {
	Query    string   `json:"query"`
	Passages []string `json:"passages"`
}

type rerankResponse struct {
	Scores []float32 `json:"scores"`
}

func (r *dependencies) Rerank(ctx context.Context, query string, chunks []store.ScoredChunk) ([]store.ScoredChunk, error) {
	started := time.Now()
	if len(chunks) == 0 {
		observability.Stage(r.log, "reranking", "empty", started, nil, zap.Int("candidates", 0))
		return chunks, nil
	}

	select {
	case r.sem <- struct{}{}:
		defer func() { <-r.sem }()
	case <-ctx.Done():
		observability.Stage(r.log, "reranking", "canceled", started, ctx.Err(), zap.Int("candidates", len(chunks)))
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
		observability.Stage(r.log, "reranking", "error", started, err, zap.Int("candidates", len(chunks)))
		return nil, fmt.Errorf("reranker build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		observability.Stage(r.log, "reranking", "error", started, err, zap.Int("candidates", len(chunks)))
		return nil, fmt.Errorf("reranker call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("reranker status %d", resp.StatusCode)
		observability.Stage(r.log, "reranking", "error", started, err, zap.Int("candidates", len(chunks)))
		return nil, err
	}

	var rrResp rerankResponse
	if err := json.NewDecoder(resp.Body).Decode(&rrResp); err != nil {
		observability.Stage(r.log, "reranking", "error", started, err, zap.Int("candidates", len(chunks)))
		return nil, fmt.Errorf("reranker decode: %w", err)
	}
	if len(rrResp.Scores) != len(chunks) {
		err := fmt.Errorf("reranker score count mismatch: got %d, want %d", len(rrResp.Scores), len(chunks))
		observability.Stage(r.log, "reranking", "error", started, err, zap.Int("candidates", len(chunks)))
		return nil, err
	}

	reranked := make([]store.ScoredChunk, len(chunks))
	copy(reranked, chunks)
	for i := range reranked {
		reranked[i].Score = rrResp.Scores[i]
	}
	sort.Slice(reranked, func(i, j int) bool { return reranked[i].Score > reranked[j].Score })
	observability.Stage(r.log, "reranking", "success", started, nil, zap.Int("candidates", len(chunks)), zap.Int("results", len(reranked)))
	return reranked, nil
}
