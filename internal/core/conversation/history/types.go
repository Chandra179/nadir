// Package history owns the persisted Conversation session and turn values.
package history

import "time"

// Session is one persisted chat conversation.
type Session struct {
	ID        string
	Title     string
	CreatedAt time.Time
	UpdatedAt time.Time
	TurnCount int
}

// TurnResult is a snapshot of one retrieved chunk as it was shown to the
// user — captured at write time rather than referenced by pointer, since
// source documents can be re-ingested or deleted after the fact.
type TurnResult struct {
	FilePath  string
	Header    string
	LineStart int
	Score     float32
	Text      string
	SourceSHA string
}

// Turn is one question/answer exchange within a session.
type Turn struct {
	ID             string
	SessionID      string
	Sequence       int
	CreatedAt      time.Time
	Query          string
	RewrittenQuery string
	AttachedFiles  []string
	TopK           int
	Generate       bool
	Results        []TurnResult
	Count          int
	ElapsedMS      int64
	FromCache      bool
	Prompt         string
	Answer         string
	HasAnswer      bool
	Model          string
	// Error is set when the search stage itself failed.
	Error string
	// GenerateError is set when search succeeded but generation failed —
	// distinct from Error so a partial (search-only) result isn't confused
	// with a fully failed turn.
	GenerateError string
	Failed        bool
}
