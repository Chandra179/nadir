//go:build js && wasm

package browser

import (
	"context"
	"fmt"
	"sort"
	"syscall/js"

	"nadir/internal/core/retrieval/search"
)

// Reranker scores passages with a host-page cross-encoder.
type Reranker struct {
	rerank js.Value
}

// NewReranker wraps rerank(query, passages) => Promise<number[]>, one relevance
// score per passage in input order.
func NewReranker(rerank js.Value) (*Reranker, error) {
	if rerank.Type() != js.TypeFunction {
		return nil, fmt.Errorf("rerank must be a function")
	}
	return &Reranker{rerank: rerank}, nil
}

func (r *Reranker) Rerank(ctx context.Context, query string, chunks []search.SearchCandidate) ([]search.SearchCandidate, error) {
	if len(chunks) == 0 {
		return chunks, nil
	}
	passages := js.Global().Get("Array").New(len(chunks))
	for i, c := range chunks {
		text := c.WindowText
		if text == "" {
			text = c.Text
		}
		passages.SetIndex(i, text)
	}
	result, err := Await(ctx, r.rerank.Invoke(query, passages))
	if err != nil {
		if ctx.Err() != nil {
			return chunks, nil
		}
		return nil, fmt.Errorf("reranker call: %w", err)
	}
	if result.Type() != js.TypeObject || result.Length() != len(chunks) {
		return nil, fmt.Errorf("reranker score count mismatch: want %d", len(chunks))
	}
	reranked := make([]search.SearchCandidate, len(chunks))
	copy(reranked, chunks)
	for i := range reranked {
		reranked[i].Score = float32(result.Index(i).Float())
	}
	sort.SliceStable(reranked, func(i, j int) bool { return reranked[i].Score > reranked[j].Score })
	return reranked, nil
}
