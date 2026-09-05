package enrichment

import "context"

// Enricher is consumed by the ingest pipeline; both methods are allowed to
// fail — callers degrade gracefully.
type Enricher interface {
	HypotheticalQuestions(ctx context.Context, header, text string, n int) ([]string, error)
	ContextualIntro(ctx context.Context, documentExcerpt, chunkText string) (string, error)
}