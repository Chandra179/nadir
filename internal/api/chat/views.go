package chat

import (
	"strconv"

	"nadir/internal/chat"
	"nadir/internal/history"
	"nadir/internal/store"
)

// TurnView is one exchange in the chat: the question, the retrieval tool
// call (request + retrieved chunks), the rewrite step, and the generated
// answer — fully rendered for history replay, or as an SSE placeholder for
// live streaming turns. Exported because the page shell (package api)
// embeds it in pageView.
type TurnView struct {
	Error          string
	Query          string
	RewrittenQuery string
	AttachedFiles  []string
	// SessionID lets the turn's edit button re-ask the question in the same
	// conversation instead of minting a new one.
	SessionID string
	TopK      int
	Generate  bool
	Results        []RetrievalResultView
	Count          int
	ElapsedMS      int64
	FromCache      bool
	Answer         string
	HasAnswer      bool
	TurnID         string
	StreamURL      string
	Prompt         string
	// GenerateError is set when search succeeded but generation failed —
	// distinct from Error, which only covers a search-stage failure.
	GenerateError string
}

type RetrievalResultView struct {
	FilePath  string
	Header    string
	LineStart int
	Score     float32
	SourceSHA string
	ScorePct  int
	ScoreStr  string
	Text      string
}

// turnViewFromResult maps a started chat turn onto what the turn fragment
// renders. A streaming turn exposes its event-stream URL and id; the
// fragment subscribes to the stream and the stop button targets the id.
func turnViewFromResult(req chat.Request, turn chat.Turn) TurnView {
	view := TurnView{
		Error:          turn.Error,
		Query:          req.Query,
		RewrittenQuery: turn.RewrittenQuery,
		AttachedFiles:  req.AttachedFiles,
		SessionID:      turn.SessionID,
		TopK:           req.TopK,
		Generate:       turn.Generate,
		Results:        toRetrievalResultViews(turn.Chunks),
		Count:          len(turn.Chunks),
		ElapsedMS:      turn.ElapsedMS,
		FromCache:      turn.FromCache,
		Answer:         turn.Answer,
		HasAnswer:      turn.HasAnswer,
		Prompt:         turn.Prompt,
		GenerateError:  turn.GenerateError,
	}
	if turn.Streaming {
		view.TurnID = turn.ID
		view.StreamURL = "/retrieval/turns/" + turn.ID + "/events"
	}
	return view
}

// HistoryTurnToView converts a persisted turn back into the same TurnView
// shape RetrievalSearch renders live, so replay renders identically.
// Exported for the page shell (package api), which replays sessions.
func HistoryTurnToView(t history.Turn) TurnView {
	results := make([]RetrievalResultView, len(t.Results))
	for i, r := range t.Results {
		results[i] = RetrievalResultView{
			FilePath:  r.FilePath,
			Header:    r.Header,
			LineStart: r.LineStart,
			Score:     r.Score,
			SourceSHA: r.SourceSHA,
			Text:      r.Text,
		}
	}
	applyRelativeScores(results)
	return TurnView{
		Error:          t.Error,
		Query:          t.Query,
		RewrittenQuery: t.RewrittenQuery,
		AttachedFiles:  t.AttachedFiles,
		SessionID:      t.SessionID,
		TopK:           t.TopK,
		Generate:       t.Generate,
		Results:        results,
		Count:          t.Count,
		ElapsedMS:      t.ElapsedMS,
		FromCache:      t.FromCache,
		Answer:         t.Answer,
		HasAnswer:      t.HasAnswer,
		Prompt:         t.Prompt,
		GenerateError:  t.GenerateError,
	}
}

// toRetrievalResultViews builds the display rows for a result set, then
// scales bar widths relative to the top score (see applyRelativeScores).
func toRetrievalResultViews(chunks []store.ScoredChunk) []RetrievalResultView {
	views := make([]RetrievalResultView, len(chunks))
	for i, ch := range chunks {
		text := ch.WindowText
		if text == "" {
			text = ch.Text
		}
		views[i] = RetrievalResultView{
			FilePath:  ch.FilePath,
			Header:    ch.Header,
			LineStart: ch.LineStart,
			Score:     ch.Score,
			SourceSHA: ch.SourceSHA,
			Text:      text,
		}
	}
	applyRelativeScores(views)
	return views
}

// applyRelativeScores fills each view's ScorePct. Scores aren't bounded to
// [0,1] (RRF and reranker produce different ranges), so bars scale relative
// to the top score of the result set.
func applyRelativeScores(views []RetrievalResultView) {
	var maxScore float32
	for _, v := range views {
		maxScore = max(maxScore, v.Score)
	}
	for i := range views {
		pct := 100
		if maxScore > 0 {
			pct = max(int(views[i].Score/maxScore*100), 4)
		}
		views[i].ScorePct = pct
		views[i].ScoreStr = strconv.FormatFloat(float64(views[i].Score), 'f', 3, 32)
	}
}
