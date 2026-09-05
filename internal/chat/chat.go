// Package chat implements the chat use-case: turn a user question into a
// persisted conversation turn by orchestrating retrieval, answer
// generation, and history. A domain package: must not import api/server/middleware.
package chat

import (
	"nadir/internal/store"
)

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
