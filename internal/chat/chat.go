// Package chat implements the chat use-case: turn a user question into a
// persisted conversation turn by orchestrating retrieval, answer
// generation, and history — the composition the HTTP handlers previously
// performed inline. It is a domain package and must not import
// api/server/middleware packages.
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
}

// Request is one chat turn as submitted.
type Request struct {
	Query         string
	TopK          int
	Filter        *store.SearchFilter
	Generate      bool
	SessionID     string // empty → mint a new session (when History is set)
	AttachedFiles []string
}

// Result is everything the caller needs to render or store one turn.
// SessionID is set even when the search stage failed, so the response can
// still hand back the minted id for subsequent turns.
type Result struct {
	SessionID string
	Chunks    []store.ScoredChunk
	ElapsedMS int64
	FromCache bool

	Prompt        string
	Answer        string
	HasAnswer     bool
	Error         string // search-stage failure
	GenerateError string // search succeeded but generation failed
}

// Chat is consumed by the API layer.
type Chat interface {
	Ask(ctx context.Context, req Request) Result
}
