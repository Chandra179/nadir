package pkb

import (
	"context"
)

// RelevanceJudge determines whether a chunk is relevant to a query.
type RelevanceJudge interface {
	IsRelevant(ctx context.Context, query string, chunk ScoredChunk) (bool, error)
}

// ContextScorer is an optional extension of RelevanceJudge.
// Returns 0–1 relevance score per chunk; averaged as ContextRelevance in EvalMetrics.
type ContextScorer interface {
	ScoreContext(ctx context.Context, query string, chunk ScoredChunk) (float64, error)
}

// Qrel is a stored relevance judgment for (query, chunk).
// Grade follows TREC 4-point scale: 0=not relevant, 1=relevant, 2=highly relevant, 3=perfect.
// Relevant field is legacy; Grade takes precedence when nonzero.
type Qrel struct {
	Query    string `json:"query"`
	ChunkID  string `json:"chunk_id"`
	FilePath string `json:"file_path"`
	Relevant bool   `json:"relevant"`
	Grade    int    `json:"grade"` // 0-3; 0 = not relevant, >=1 = relevant
}
