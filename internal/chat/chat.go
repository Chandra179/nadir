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

// Turn is the outcome of starting a chat turn: everything needed to render
// the trace, plus — when generation was started — the ID of its event
// stream. The answer itself is never carried here at start time; it arrives
// as events on the stream and lands on the persisted turn when generation
// finishes.
type Turn struct {
	ID             string // event-stream id; empty when nothing streams
	SessionID      string
	Query          string
	RewrittenQuery string // set only when the rewriter changed the query
	Chunks         []store.ScoredChunk
	ElapsedMS      int64
	FromCache      bool
	Generate       bool
	Prompt         string
	Error          string // search-stage failure
	GenerateError  string // generation failed (at start or mid-stream)
	Answer         string // populated only once generation finishes
	HasAnswer      bool
	Streaming      bool // generation is running; subscribe with ID
}

// EventKind discriminates a TurnEvent.
type EventKind uint8

const (
	EventToken EventKind = iota
	EventError
	EventDone
)

// TurnEvent is one entry in a turn's event log. Seq is a per-turn cursor;
// subscribers name the last event they saw and are replayed from there.
type TurnEvent struct {
	Seq  int64
	Kind EventKind
	Text string
}
