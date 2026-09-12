package rewriter

import "context"

// Turn is one prior exchange from the conversation, used to resolve
// references in a follow-up question.
type Turn struct {
	Query string
	// Answer is the assistant's reply to Query; may be empty (e.g. a turn
	// whose search or generation failed).
	Answer string
}

// Rewriter turns a conversational follow-up question into a standalone
// search query using prior conversation turns (Rewrite-Retrieve-Read,
// arXiv 2305.14283). Callers treat it as best-effort: any failure falls
// back to searching the raw follow-up.
type Rewriter interface {
	Rewrite(ctx context.Context, turns []Turn, query string) (string, error)
}
