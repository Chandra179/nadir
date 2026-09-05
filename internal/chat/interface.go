package chat

import (
	"context"
	"io"

	"nadir/internal/history"
	"nadir/internal/store"
)

// Searcher is the retrieval entry point (satisfied by *search.Dependencies).
type Searcher interface {
	Query(ctx context.Context, query, keyword string, topK int, filter *store.SearchFilter, skipCache bool) (chunks []store.ScoredChunk, fromCache bool, err error)
}

// Generator produces an answer stream over retrieved chunks
// (satisfied by *generator.Dependencies).
type Generator interface {
	Generate(ctx context.Context, query string, chunks []store.ScoredChunk) (prompt string, r io.ReadCloser, err error)
}

// History persists sessions and turns; optional — when nil, turns are not
// minted or saved and Ask degrades to stateless search+generate.
type History interface {
	CreateSession(ctx context.Context, title string) (history.Session, error)
	AppendTurn(ctx context.Context, sessionID string, turn history.Turn, firstTurnTitle string) error
	// ListTurns returns a session's turns in sequence order; read when
	// rewriting a follow-up query against prior conversation context.
	ListTurns(ctx context.Context, sessionID string) ([]history.Turn, error)
}

// Chat is consumed by the API layer.
type Chat interface {
	Ask(ctx context.Context, req Request) Result
}
