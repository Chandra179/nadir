package chat

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"nadir/internal/conversation/generation"
	"nadir/internal/conversation/history"
	"nadir/internal/platform/observability"
	"nadir/internal/retrieval/rewriting"
	"nadir/internal/retrieval/search"
)

// StartTurn runs one chat turn: mint a session (first turn) or prune an edit
// tail → rewrite follow-ups → retrieve → start generation. Never returns an error:
// failures land in Turn.Error/Turn.GenerateError. Generation is owned by
// the service — it runs on its own goroutine, fans events out to any number
// of subscribers, and persists the final turn itself; the caller only
// renders the trace and (when Turn.Streaming) subscribes via Subscribe.
func (d *dependencies) StartTurn(ctx context.Context, req Request) Turn {
	if !d.beginStart() {
		return Turn{Error: "Chat service is shutting down."}
	}
	defer d.endStart()

	turn := Turn{Query: req.Query, Generate: req.Generate}
	turn.SessionID = req.SessionID
	var mutation historyMutation
	if d.history != nil && req.SessionID != "" {
		mutation = d.mutations.capture(req.SessionID)
	}

	if strings.TrimSpace(req.Query) == "" {
		turn.Error = "Enter a question to search."
		d.persistStart(ctx, req, turn, mutation, true)
		return turn
	}

	start := time.Now()
	if req.Edit {
		if d.history == nil || req.SessionID == "" {
			turn.Error = "Chat editing is unavailable."
			return turn
		}
		var err error
		mutation, err = d.mutations.prepareEdit(ctx, d.history, req.SessionID, req.EditSequence)
		if err != nil {
			d.log.Warn("chat edit prune failed",
				zap.String("session_id", req.SessionID),
				zap.Int("edit_sequence", req.EditSequence),
				zap.Error(err))
			turn.Error = "Unable to edit conversation: " + err.Error()
			return turn
		}
	}

	if d.history != nil && turn.SessionID == "" {
		turn.SessionID, mutation = d.mintSession(ctx, req.Query)
	}

	retrievalQuery := req.Query
	// A minted first session has no prior turns; edited sessions have already
	// been truncated, so rewrite sees exactly the retained prefix.
	if d.rewriter != nil && d.history != nil && req.SessionID != "" {
		retrievalQuery = d.rewriteQuery(ctx, req.SessionID, req.Query)
		if retrievalQuery != req.Query {
			turn.RewrittenQuery = retrievalQuery
		}
	}
	if d.mutationStale(mutation) {
		turn.Error = "Conversation changed while this turn was starting; please retry."
		return turn
	}

	searchResult, err := d.searcher.Query(ctx, search.Request{
		Query:  retrievalQuery,
		TopK:   req.TopK,
		Filter: req.Filter,
	})
	turn.FromCache = searchResult.FromCache
	if err != nil {
		d.log.Warn("chat search failed", zap.String("query", req.Query), zap.Error(err))
		turn.Error = "Search failed: " + err.Error()
		d.persistStart(ctx, req, turn, mutation, true)
		return turn
	}
	turn.Chunks = searchResult.Chunks
	turn.ElapsedMS = time.Since(start).Milliseconds()
	if d.mutationStale(mutation) {
		turn.Error = "Conversation changed while this turn was running; please retry."
		return turn
	}

	// Every non-generating outcome is final here: persist and return.
	if !req.Generate || d.generator == nil || len(turn.Chunks) == 0 {
		d.persistStart(ctx, req, turn, mutation, false)
		return turn
	}

	turn.Prompt = buildPrompt(retrievalQuery, turn.Chunks, d.maxContextTokens)

	// The generation context is detached from this POST: the request that
	// starts a turn must not be the one that can kill it. CancelTurn (not
	// browser disconnects) is what stops generation. The Ollama dial is
	// synchronous so a start failure is a deterministic GenerateError with
	// no stream; only a live stream gets an ID and an event log.
	generationStarted := time.Now()
	genCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	events, err := d.generator.Generate(genCtx, turn.Prompt)
	if err != nil {
		cancel()
		observability.Stage(d.log, "generation", "error", generationStarted, err)
		d.log.Warn("chat generate failed", zap.String("query", req.Query), zap.Error(err))
		turn.GenerateError = "Answer generation failed: " + err.Error()
		d.persistStart(ctx, req, turn, mutation, false)
		return turn
	}

	turn.ID = uuid.NewString()
	stream, ok := d.broker.create(turn.ID)
	if !ok {
		cancel()
		observability.Stage(d.log, "generation", "error", generationStarted, errors.New("broker rejected generation"))
		d.log.Warn("chat broker rejected generation",
			zap.String("query", req.Query), zap.Int("max_retained_turns", d.broker.maxRetainedTurns))
		turn.GenerateError = "Answer generation is temporarily unavailable: too many active streams."
		d.persistStart(ctx, req, turn, mutation, false)
		return turn
	}
	stream.setCancel(cancel)
	if !d.mutations.registerGeneration(turn.ID, mutation, stream) {
		cancel()
		stream.finish()
		turn.ID = ""
		turn.GenerateError = "Conversation changed while generation was starting; please retry."
		return turn
	}
	turn.Streaming = true
	d.generations.Add(1)
	go func(supervisorTurn Turn) {
		defer d.generations.Done()
		d.consumeGeneration(stream, req, supervisorTurn, mutation, events, generationStarted)
	}(turn)
	return turn
}

// CancelTurn aborts an in-flight generation. The supervisor observes the
// cancelled context, keeps the answer generated so far, and persists it.
func (d *dependencies) CancelTurn(turnID string) bool {
	stream := d.broker.get(turnID)
	if stream == nil {
		return false
	}
	return stream.cancelGeneration()
}

// DeleteSession removes one conversation through the chat lifecycle seam.
// This invalidates older turn mutations and cancels active generation before
// handing the destructive operation to the history Module.
func (d *dependencies) DeleteSession(ctx context.Context, sessionID string) error {
	if d.history == nil {
		return errors.New("chat history is disabled")
	}
	return d.mutations.deleteSession(ctx, d.history, sessionID)
}

// DeleteAllSessions removes every conversation through the chat lifecycle
// seam, preventing detached generation persistence from recreating turns.
func (d *dependencies) DeleteAllSessions(ctx context.Context) error {
	if d.history == nil {
		return errors.New("chat history is disabled")
	}
	return d.mutations.deleteAll(ctx, d.history)
}

// Drain stops active generation and waits for generation supervisors and
// detached history writes to finish. The composition root calls this after
// HTTP shutdown has stopped new requests and before closing shared stores.
//
// A timeout is reported to the caller; it never abandons the wait silently.
// The caller can then decide whether to continue process shutdown and record
// the failed drain in its operational logs.
func (d *dependencies) Drain(ctx context.Context) error {
	activeDone := d.beginDrain()
	d.mutations.cancelAll()
	d.broker.cancelAll()
	select {
	case <-activeDone:
	case <-ctx.Done():
		return ctx.Err()
	}
	// A StartTurn that was already inside the gate may have created its
	// stream after the first cancellation snapshot. Cancel once more after
	// all such callers have left so no generation slips through the drain.
	d.mutations.cancelAll()
	d.broker.cancelAll()

	done := make(chan struct{})
	go func() {
		d.generations.Wait()
		d.persists.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (d *dependencies) beginStart() bool {
	d.lifecycleMu.Lock()
	defer d.lifecycleMu.Unlock()
	if d.draining {
		return false
	}
	if d.activeStarts == 0 {
		d.activeDone = make(chan struct{})
	}
	d.activeStarts++
	return true
}

func (d *dependencies) endStart() {
	d.lifecycleMu.Lock()
	defer d.lifecycleMu.Unlock()
	d.activeStarts--
	if d.activeStarts == 0 {
		close(d.activeDone)
	}
}

func (d *dependencies) beginDrain() <-chan struct{} {
	d.lifecycleMu.Lock()
	defer d.lifecycleMu.Unlock()
	d.draining = true
	return d.activeDone
}

// consumeGeneration drains one in-flight answer: it maps the generator's
// typed events onto the turn's event log and persists the final turn when
// the stream ends. Runs on its own goroutine — no HTTP request owns this.
func (d *dependencies) consumeGeneration(stream *turnStream, req Request, turn Turn, mutation historyMutation, events <-chan generator.Event, started time.Time) {
	defer func() {
		d.mutations.unregisterGeneration(turn.ID)
		stream.finish()
	}()

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
		observability.Stage(d.log, "generation", "error", started, errors.New("generation failed"),
			zap.Int("answer_bytes", answer.Len()))
	} else {
		turn.Answer = answer.String()
		turn.HasAnswer = true
		stream.publish(EventDone, "")
		observability.Stage(d.log, "generation", "success", started, nil,
			zap.Int("answer_bytes", answer.Len()))
	}
	d.saveTurn(req, turn, mutation)
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
func (d *dependencies) mintSession(ctx context.Context, query string) (string, historyMutation) {
	session, mutation, err := d.mutations.createSession(ctx, d.history, query)
	if err != nil {
		d.log.Warn("chat create session failed", zap.String("query", query), zap.Error(err))
		return "", historyMutation{}
	}
	return session.ID, mutation
}

// persistTurn writes the turn's current state to history; the 5s timeout
// keeps an unreachable store from pinning the caller.
func (d *dependencies) persistTurn(ctx context.Context, req Request, turn Turn, mutation historyMutation, failed bool) error {
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
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), d.persistTimeout)
	defer cancel()
	return d.mutations.append(cctx, d.history, mutation, ht, req.Query)
}

// persistStart keeps an edited turn together with its prune before the UI
// renders the replacement. Ordinary turns remain best-effort and detached
// so a slow history store cannot delay the live response.
func (d *dependencies) persistStart(ctx context.Context, req Request, turn Turn, mutation historyMutation, failed bool) {
	if req.Edit {
		if err := d.persistTurn(ctx, req, turn, mutation, failed); err != nil {
			if errors.Is(err, errStaleHistoryMutation) {
				return
			}
			d.log.Warn("chat append edited turn failed", zap.String("session_id", turn.SessionID), zap.Error(err))
		}
		return
	}
	d.persist(ctx, req, turn, mutation, failed)
}

// persist saves a turn in a best-effort, detached goroutine — a slow or
// unreachable store must never delay the response the user is watching.
func (d *dependencies) persist(reqCtx context.Context, req Request, turn Turn, mutation historyMutation, failed bool) {
	if d.history == nil || turn.SessionID == "" {
		return
	}
	d.persists.Add(1)
	go func() {
		defer d.persists.Done()
		if err := d.persistTurn(reqCtx, req, turn, mutation, failed); err != nil {
			if errors.Is(err, errStaleHistoryMutation) {
				return
			}
			d.log.Warn("chat append turn failed", zap.String("session_id", turn.SessionID), zap.Error(err))
		}
	}()
}

// saveTurn persists a finished generation from the supervisor goroutine.

func (d *dependencies) saveTurn(req Request, turn Turn, mutation historyMutation) {
	if err := d.persistTurn(context.Background(), req, turn, mutation, false); err != nil {
		if errors.Is(err, errStaleHistoryMutation) {
			return
		}
		d.log.Warn("chat append turn failed", zap.String("session_id", turn.SessionID), zap.Error(err))
	}
}

func (d *dependencies) mutationStale(mutation historyMutation) bool {
	return mutation.sessionID != "" && !d.mutations.current(mutation)
}

// chunkResults snapshots retrieved chunks for persistence — captured at
// write time rather than referenced by pointer, since source documents can
// be re-ingested or deleted after the fact. WindowText is preferred when
// present, matching what was displayed live.
func chunkResults(chunks []search.Chunk) []history.TurnResult {
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
