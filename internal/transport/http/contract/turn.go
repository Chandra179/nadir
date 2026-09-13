// Package contract contains the public HTTP representations shared by API
// feature handlers. Keeping these shapes here prevents one feature package
// from importing another feature package just to reuse a response type.
package contract

import (
	"nadir/internal/conversation/chat"
	"nadir/internal/conversation/history"
	"nadir/internal/retrieval/search"
)

// TurnResponse is the public JSON representation of a live or persisted turn.
type TurnResponse struct {
	Error          string           `json:"error,omitempty"`
	Query          string           `json:"query"`
	RewrittenQuery string           `json:"rewritten_query,omitempty"`
	AttachedFiles  []string         `json:"attached_files,omitempty"`
	SessionID      string           `json:"session_id"`
	Sequence       int              `json:"sequence,omitempty"`
	TopK           int              `json:"top_k"`
	Generate       bool             `json:"generate"`
	Results        []ResultResponse `json:"results"`
	Count          int              `json:"count"`
	ElapsedMS      int64            `json:"elapsed_ms"`
	FromCache      bool             `json:"from_cache"`
	Answer         string           `json:"answer,omitempty"`
	HasAnswer      bool             `json:"has_answer"`
	TurnID         string           `json:"turn_id,omitempty"`
	StreamURL      string           `json:"stream_url,omitempty"`
	Prompt         string           `json:"prompt,omitempty"`
	GenerateError  string           `json:"generate_error,omitempty"`
	Streaming      bool             `json:"streaming"`
}

// ResultResponse is the public JSON representation of one retrieved chunk.
type ResultResponse struct {
	FilePath  string  `json:"file_path"`
	Header    string  `json:"header,omitempty"`
	LineStart int     `json:"line_start"`
	Score     float32 `json:"score"`
	SourceSHA string  `json:"source_sha,omitempty"`
	Text      string  `json:"text"`
}

// FromChatTurn maps a live chat result to the public HTTP representation.
func FromChatTurn(req chat.Request, turn chat.Turn) TurnResponse {
	view := TurnResponse{
		Error:          turn.Error,
		Query:          req.Query,
		RewrittenQuery: turn.RewrittenQuery,
		AttachedFiles:  req.AttachedFiles,
		SessionID:      turn.SessionID,
		TopK:           req.TopK,
		Generate:       turn.Generate,
		Results:        toResultResponses(turn.Chunks),
		Count:          len(turn.Chunks),
		ElapsedMS:      turn.ElapsedMS,
		FromCache:      turn.FromCache,
		Answer:         turn.Answer,
		HasAnswer:      turn.HasAnswer,
		Prompt:         turn.Prompt,
		GenerateError:  turn.GenerateError,
		Streaming:      turn.Streaming,
	}
	if turn.Streaming {
		view.TurnID = turn.ID
		view.StreamURL = "/api/v1/turns/" + turn.ID + "/events"
	}
	return view
}

// FromHistoryTurn maps a persisted turn to the same public HTTP representation.
func FromHistoryTurn(t history.Turn) TurnResponse {
	return TurnResponse{
		Error:          t.Error,
		Query:          t.Query,
		RewrittenQuery: t.RewrittenQuery,
		AttachedFiles:  t.AttachedFiles,
		SessionID:      t.SessionID,
		Sequence:       t.Sequence,
		TopK:           t.TopK,
		Generate:       t.Generate,
		Results:        toHistoryResultResponses(t.Results),
		Count:          t.Count,
		ElapsedMS:      t.ElapsedMS,
		FromCache:      t.FromCache,
		Answer:         t.Answer,
		HasAnswer:      t.HasAnswer,
		Prompt:         t.Prompt,
		GenerateError:  t.GenerateError,
	}
}

func toResultResponses(chunks []search.Chunk) []ResultResponse {
	responses := make([]ResultResponse, len(chunks))
	for i, ch := range chunks {
		text := ch.WindowText
		if text == "" {
			text = ch.Text
		}
		responses[i] = ResultResponse{
			FilePath:  ch.FilePath,
			Header:    ch.Header,
			LineStart: ch.LineStart,
			Score:     ch.Score,
			SourceSHA: ch.SourceSHA,
			Text:      text,
		}
	}
	return responses
}

func toHistoryResultResponses(results []history.TurnResult) []ResultResponse {
	responses := make([]ResultResponse, len(results))
	for i, result := range results {
		responses[i] = ResultResponse{
			FilePath:  result.FilePath,
			Header:    result.Header,
			LineStart: result.LineStart,
			Score:     result.Score,
			SourceSHA: result.SourceSHA,
			Text:      result.Text,
		}
	}
	return responses
}
