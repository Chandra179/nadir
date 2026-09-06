package chat

import (
	"context"
	"go.uber.org/zap"
	"strings"
	"time"

	"github.com/google/uuid"

	"nadir/internal/generator"
	"nadir/internal/history"
	"nadir/internal/rewriter"
	"nadir/internal/store"
)

// StartTurn runs one chat turn: mint session (first turn) → rewrite
// follow-ups → retrieve → start generation. Never returns an error:
// failures land in Turn.Error/Turn.GenerateError. Generation is owned by
// the service — it runs on its own goroutine, fans events out to any number
// of subscribers, and persists the final turn itself; the caller only
// renders the trace and (when Turn.Streaming) subscribes via Subscribe.
func (d *dependencies) StartTurn(ctx context.Context, req Request) Turn {
	turn := Turn{SessionID: req.SessionID, Query: req.Query, Generate: req.Generate}

	if req.Query == "" {
		turn.Error = "Enter a question to search."
		d.persist(ctx, req, turn, true)
		return turn
	}

	start := time.Now()

	if d.history != nil && turn.SessionID == "" {
		turn.SessionID = d.mintSession(ctx, req.Query)
	}

	retrievalQuery := req.Query
	// Gated on the request's session id, not the minted one: a turn that
	// mints a session cannot have prior turns by construction.
	if d.rewriter != nil && d.history != nil && req.SessionID != "" {
		retrievalQuery = d.rewriteQuery(ctx, req.SessionID, req.Query)
		if retrievalQuery != req.Query {
			turn.RewrittenQuery = retrievalQuery
		}
	}

	chunks, fromCache, err := d.searcher.Query(ctx, retrievalQuery, "", req.TopK, req.Filter, req.Generate)
	turn.FromCache = fromCache
	if err != nil {
		d.log.Warn("chat search failed", zap.String("query", req.Query), zap.Error(err))
		turn.Error = "Search failed: " + err.Error()
		d.persist(ctx, req, turn, true)
		return turn
	}
	turn.Chunks = chunks
	turn.ElapsedMS = time.Since(start).Milliseconds()

	// Every non-generating outcome is final here: persist and return.
	if !req.Generate || d.generator == nil || len(chunks) == 0 {
		d.persist(ctx, req, turn, false)
		return turn
	}

	turn.Prompt = buildPrompt(retrievalQuery, chunks, d.maxContextTokens)

	// The generation context is detached from this POST: the request that
	// starts a turn must not be the one that can kill it. CancelTurn (not
	// browser disconnects) is what stops generation. The Ollama dial is
	// synchronous so a start failure is a deterministic GenerateError with
	// no stream; only a live stream gets an ID and an event log.
	genCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	events, err := d.generator.Generate(genCtx, turn.Prompt)
	if err != nil {
		cancel()
		d.log.Warn("chat generate failed", zap.String("query", req.Query), zap.Error(err))
		turn.GenerateError = "Answer generation failed: " + err.Error()
		d.persist(ctx, req, turn, false)
		return turn
	}

	turn.ID = uuid.NewString()
	stream := d.broker.create(turn.ID)
	stream.cancel = cancel
	go d.consumeGeneration(stream, req, turn, events)
	turn.Streaming = true
	return turn
}

// CancelTurn aborts an in-flight generation. The supervisor observes the
// cancelled context, keeps the answer generated so far, and persists it.
func (d *dependencies) CancelTurn(turnID string) bool {
	stream := d.broker.get(turnID)
	if stream == nil {
		return false
	}
	stream.cancelGeneration()
	return true
}

// consumeGeneration drains one in-flight answer: it maps the generator's
// typed events onto the turn's event log and persists the final turn when
// the stream ends. Runs on its own goroutine — no HTTP request owns this.
func (d *dependencies) consumeGeneration(stream *turnStream, req Request, turn Turn, events <-chan generator.Event) {
	defer stream.finish()

	var answer strings.Builder
	for ev := range events {
		switch e := ev.(type) {
		case generator.TokenEvent:
			answer.WriteString(e.Text)
			stream.publish(EventToken, e.Text)
		case generator.ErrorEvent:
			turn.GenerateError = "Answer generation failed: " + e.Err.Error()
		case generator.DoneEvent:
		}
	}
	if turn.GenerateError != "" {
		stream.publish(EventError, turn.GenerateError)
	} else {
		turn.Answer = answer.String()
		turn.HasAnswer = true
		stream.publish(EventDone, "")
	}
	d.saveTurn(req, turn)
}

// Subscribe attaches to a turn's event log, replaying after since. The
// returned cancel is also invoked when ctx ends, so a disconnecting SSE
// client detaches its subscriber without affecting generation.
func (d *dependencies) Subscribe(ctx context.Context, turnID string, since int64) (<-chan TurnEvent, func(), bool) {
	stream := d.broker.get(turnID)
	if stream == nil {
		return nil, nil, false
	}
	events, cancel := stream.subscribe(since)
	go func() {
		<-ctx.Done()
		cancel()
	}()
	return events, cancel, true
}

// rewriteQuery rewrites a follow-up into a standalone query against the
// session's recent turns (Rewrite-Retrieve-Read). The rewritten query drives
// retrieval and generation; the raw query is what gets persisted.
func (d *dependencies) rewriteQuery(ctx context.Context, sessionID, query string) string {
	turns, err := d.history.ListTurns(ctx, sessionID)
	if err != nil {
		d.log.Warn("rewrite skipped: list turns failed",
			zap.String("session_id", sessionID), zap.Error(err))
		return query
	}
	prior := make([]rewriter.Turn, 0, len(turns))
	for _, t := range turns {
		prior = append(prior, rewriter.Turn{Query: t.Query, Answer: t.Answer})
	}
	if len(prior) == 0 {
		return query
	}
	if len(prior) > d.rewriteTurns {
		prior = prior[len(prior)-d.rewriteTurns:]
	}
	rewritten, err := d.rewriter.Rewrite(ctx, prior, query)
	if err != nil {
		d.log.Warn("rewrite failed; searching raw query",
			zap.String("session_id", sessionID), zap.String("query", query), zap.Error(err))
		return query
	}
	if rewritten != query {
		d.log.Info("rewrote follow-up query",
			zap.String("session_id", sessionID),
			zap.String("raw", query),
			zap.String("rewritten", rewritten))
	}
	return rewritten
}

// mintSession creates a conversation session for the first turn of a chat.
// Best-effort: failures are logged and return "" so the turn proceeds
// without a session.
func (d *dependencies) mintSession(ctx context.Context, query string) string {
	session, err := d.history.CreateSession(ctx, query)
	if err != nil {
		d.log.Warn("chat create session failed", zap.String("query", query), zap.Error(err))
		return ""
	}
	return session.ID
}

// persistTurn writes the turn's current state to history; the 5s timeout
// keeps an unreachable store from pinning the caller.
func (d *dependencies) persistTurn(ctx context.Context, req Request, turn Turn, failed bool) error {
	if d.history == nil || turn.SessionID == "" {
		return nil
	}
	ht := history.Turn{
		Query:          req.Query,
		RewrittenQuery: turn.RewrittenQuery,
		AttachedFiles:  req.AttachedFiles,
		TopK:           req.TopK,
		Generate:       req.Generate,
		Results:        chunkResults(turn.Chunks),
		Count:          len(turn.Chunks),
		ElapsedMS:      turn.ElapsedMS,
		FromCache:      turn.FromCache,
		Prompt:         turn.Prompt,
		Answer:         turn.Answer,
		HasAnswer:      turn.HasAnswer,
		Error:          turn.Error,
		GenerateError:  turn.GenerateError,
		Model:          d.model,
		Failed:         failed,
	}
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return d.history.AppendTurn(cctx, turn.SessionID, ht, req.Query)
}

// persist saves a turn in a best-effort, detached goroutine — a slow or
// unreachable store must never delay the response the user is watching.
func (d *dependencies) persist(reqCtx context.Context, req Request, turn Turn, failed bool) {
	if d.history == nil || turn.SessionID == "" {
		return
	}
	go func() {
		if err := d.persistTurn(reqCtx, req, turn, failed); err != nil {
			d.log.Warn("chat append turn failed", zap.String("session_id", turn.SessionID), zap.Error(err))
		}
	}()
}

// saveTurn persists a finished generation from the supervisor goroutine.
func (d *dependencies) saveTurn(req Request, turn Turn) {
	if err := d.persistTurn(context.Background(), req, turn, false); err != nil {
		d.log.Warn("chat append turn failed", zap.String("session_id", turn.SessionID), zap.Error(err))
	}
}

// chunkResults snapshots retrieved chunks for persistence — captured at
// write time rather than referenced by pointer, since source documents can
// be re-ingested or deleted after the fact. WindowText is preferred when
// present, matching what was displayed live.
func chunkResults(chunks []store.ScoredChunk) []history.TurnResult {
	if len(chunks) == 0 {
		return nil
	}
	out := make([]history.TurnResult, len(chunks))
	for i, ch := range chunks {
		text := ch.WindowText
		if text == "" {
			text = ch.Text
		}
		out[i] = history.TurnResult{
			FilePath:  ch.FilePath,
			Header:    ch.Header,
			LineStart: ch.LineStart,
			Score:     ch.Score,
			Text:      text,
			SourceSHA: ch.SourceSHA,
		}
	}
	return out
}
