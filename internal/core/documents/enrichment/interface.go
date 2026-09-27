package enrichment

import "context"

// Enricher is consumed by the ingest pipeline; the method is allowed to
// fail — callers degrade gracefully.
type Enricher interface {
	// ContextualIntro writes a short situational summary situating chunkText
	// within documentExcerpt (Anthropic-style contextual retrieval).
	ContextualIntro(ctx context.Context, documentExcerpt, chunkText string) (string, error)
}
