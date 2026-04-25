package pkb

import (
	"context"
	"strings"
)

// SparseScorer scores a chunk against a query for client-side BM25 reranking.
// Prefer SparseEmbedder for indexed hybrid search; SparseScorer is the client-side fallback.
// The context is passed through to allow cancellation of HTTP calls in implementations.
type SparseScorer interface {
	Score(ctx context.Context, query, text string) (float64, error)
}

// TFSparseScorer scores by raw term frequency — sum of query term occurrences in text.
// Fast, zero-dependency default for the BM25 hybrid search leg.
type TFSparseScorer struct{}

func (TFSparseScorer) Score(_ context.Context, query, text string) (float64, error) {
	textLower := strings.ToLower(text)
	var score float64
	for _, term := range strings.Fields(strings.ToLower(query)) {
		if term != "" {
			score += float64(strings.Count(textLower, term))
		}
	}
	return score, nil
}
