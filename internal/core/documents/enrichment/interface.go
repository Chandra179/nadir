package enrichment

import "context"

// Enricher is consumed by the ingest pipeline; both methods are allowed to
// fail — callers degrade gracefully.
type Enricher interface {
	// HypotheticalQuestions generates up to n short user questions that the
	// given chunk answers (HyPE).
	HypotheticalQuestions(ctx context.Context, header, text string, n int) ([]string, error)
	// ContextualIntro writes a short situational summary situating chunkText
	// within documentExcerpt (Anthropic-style contextual retrieval).
	ContextualIntro(ctx context.Context, documentExcerpt, chunkText string) (string, error)
}
