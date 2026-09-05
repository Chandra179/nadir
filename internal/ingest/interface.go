package ingest

import "context"

// Ingest runs an ingest pass over a batch of uploaded files: chunk, embed,
// and upsert each one that's new or changed since the last run (by content
// SHA-256).
type Ingest interface {
	Run(ctx context.Context, files []UploadFile) (Result, error)
}

// Enricher performs index-time LLM enrichment. Both methods are best-effort:
// failures degrade to indexing the chunk without enrichment.
type Enricher interface {
	// HypotheticalQuestions generates up to n short user questions that the
	// given chunk answers (HyPE).
	HypotheticalQuestions(ctx context.Context, header, text string, n int) ([]string, error)
	// ContextualIntro writes a short situational summary situating chunkText
	// within documentExcerpt (Anthropic-style contextual retrieval).
	ContextualIntro(ctx context.Context, documentExcerpt, chunkText string) (string, error)
}
