package chat

import (
	"context"

	"nadir/internal/generator"
	"nadir/internal/history"
	"nadir/internal/store"
)

// Searcher is the retrieval entry point (satisfied by *search.Dependencies).
type Searcher interface {
	Query(ctx context.Context, query, keyword string, topK int, filter *store.SearchFilter, skipCache bool) (chunks []store.ScoredChunk, fromCache bool, err error)
}

// Generator turns a fully-built prompt into a typed event stream
// (satisfied by *generator.Dependencies).
type Generator interface {
	Generate(ctx context.Context, prompt string) (<-chan generator.Event, error)
}

// History persists sessions and turns; optional — when nil, turns are not
// minted or saved and StartTurn degrades to stateless search.
type History interface {
	CreateSession(ctx context.Context, title string) (history.Session, error)
	AppendTurn(ctx context.Context, sessionID string, turn history.Turn, firstTurnTitle string) error
	// ListTurns returns a session's turns in sequence order; read when
	// rewriting a follow-up query against prior conversation context.
	ListTurns(ctx context.Context, sessionID string) ([]history.Turn, error)
}

// Chat is consumed by the API layer. Generation is owned by the service:
// StartTurn kicks it off on its own goroutine, and any number of transport
// connections subscribe to the turn's event log — late subscribers are
// replayed from their cursor.
type Chat interface {
	StartTurn(ctx context.Context, req Request) Turn
	// Subscribe attaches to a turn's event log, replaying everything after
	// since (0 = from the beginning). ok is false for unknown turn ids.
	Subscribe(ctx context.Context, turnID string, since int64) (<-chan TurnEvent, func(), bool)
	// CancelTurn aborts an in-flight generation; the partial answer is kept
	// and persisted. ok is false for unknown turn ids.
	CancelTurn(turnID string) bool
}
