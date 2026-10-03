package chat

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"log/slog"

	"nadir/internal/core/conversation/generation"
	"nadir/internal/core/conversation/history"
	"nadir/internal/core/conversation/rewriting"
	"nadir/internal/core/observability"
	"nadir/internal/core/retrieval/search"
)

// StartTurn runs one chat turn: mint a session (first turn) or prune an edit
// tail → rewrite follow-ups → retrieve → start generation. Never returns an error:
// failures land in Turn.Error/Turn.GenerateError. Generation is owned by
// the service — it runs on its own goroutine, fans events out to any number
// of subscribers, and persists the final turn itself; the caller only
// renders the trace and (when Turn.Streaming) subscribes via Subscribe.
func (d *dependencies) StartTurn(ctx context.Context, req Request) Turn {
	ctx, operation := observability.Start(ctx, d.telemetry, d.log, "chat")
	var operationErr error
	defer func() {
		outcome := "success"
		if operationErr != nil {
			outcome = "error"
		}
		operation.End(outcome, operationErr)
	}()
	if !d.beginStart() {
		operationErr = errors.New("chat service is shutting down")
		return Turn{OperationID: operation.ID(), Error: "Chat service is shutting down."}
	}
	defer d.endStart()

	turn := Turn{OperationID: operation.ID(), Query: req.Query, Generate: req.Generate,
		SessionID: req.SessionID}
	var mutation historyMutation
	if d.history != nil && req.SessionID != "" {
		mutation = d.mutations.capture(req.SessionID)
	}

	if strings.TrimSpace(req.Query) == "" {
		operationErr = errors.New("empty chat query")
		turn.Error = "Enter a question to search."
		d.persistStart(ctx, req, turn, mutation, true)
		return turn
	}

	start := time.Now()
	if req.Edit {
		if d.history == nil || req.SessionID == "" {
			operationErr = errors.New("chat editing is unavailable")
			turn.Error = "Chat editing is unavailable."
			return turn
		}
		var err error
		mutation, err = d.mutations.prepareEdit(ctx, req.SessionID, req.EditSequence)
		if err != nil {
			operationErr = err
			d.log.Warn("chat edit prune failed",
				slog.String("session_id", req.SessionID),
				slog.Int("edit_sequence", req.EditSequence),
				slog.String("error_label", observability.ErrorLabel(err)))
			turn.Error = failureMessage("Conversation edit", err)
			return turn
		}
	}

	if d.history != nil && turn.SessionID == "" {
		turn.SessionID, mutation = d.mintSession(ctx, req.Query)
	}
	if req.Generate && d.generator != nil && requiresLiveObservation(req.Query) {
		turn.Answer, turn.HasAnswer = liveStateUnavailable, true
		turn.ElapsedMS = time.Since(start).Milliseconds()
		d.persistStart(ctx, req, turn, mutation, false)
		return turn
	}

	retrievalQuery := req.Query
	referenceContext := ""
	// A minted first session has no prior turns; edited sessions have already
	// been truncated, so rewrite sees exactly the retained prefix.
	if d.history != nil && req.SessionID != "" {
		retrievalQuery, referenceContext, turn.Subject = d.rewriteQuery(ctx, req.SessionID, req.Query)
		if retrievalQuery != req.Query {
			turn.RewrittenQuery = retrievalQuery
		}
	}
	// Staleness guard for the awaits above (session mint, follow-up
	// rewrite): a concurrent delete or edit invalidates the captured
	// mutation.
	if d.mutationStale(mutation) {
		operationErr = errors.New("conversation changed while turn was starting")
		turn.Error = "Conversation changed while this turn was starting; please retry."
		return turn
	}

	searchResult, err := d.searcher.Query(ctx, search.Request{
		Query:     retrievalQuery,
		TopK:      req.TopK,
		Filter:    req.Filter,
		SkipCache: req.SkipCache,
	})
	turn.FromCache = searchResult.FromCache
	if err != nil {
		operationErr = err
		d.log.Warn("chat search failed", slog.String("operation_id", operation.ID()), slog.String("error_label", observability.ErrorLabel(err)))
		turn.Error = failureMessage("Search", err)
		d.persistStart(ctx, req, turn, mutation, true)
		return turn
	}
	turn.Chunks = searchResult.Chunks
	turn.ElapsedMS = time.Since(start).Milliseconds()
	// Staleness guard for the retrieval await: history may have changed
	// while the search ran.
	if d.mutationStale(mutation) {
		operationErr = errors.New("conversation changed while turn was running")
		turn.Error = "Conversation changed while this turn was running; please retry."
		return turn
	}

	// Every non-generating outcome is final here: persist and return.
	if !req.Generate || d.generator == nil || len(turn.Chunks) == 0 {
		d.persistStart(ctx, req, turn, mutation, false)
		return turn
	}

	// Retrieval rewrites resolve references, but may reduce a question to
	// keywords. Keep the original intent visible to the answer model.
	if retrievalQuery != req.Query {
		referenceContext += "\nSearch rewrite (reference resolution only): " + retrievalQuery
	}
	built := BuildPromptWithBudget(req.Query, turn.Chunks, PromptBudget{
		MaxContextTokens:     d.maxContextTokens,
		ContextWindowTokens:  d.contextWindowTokens,
		ReservedOutputTokens: d.reservedOutputTokens,
		ReferenceContext:     referenceContext,
		ResolvedSubject:      subjectLabel(turn.Subject),
	})
	if built.Err != nil {
		operationErr = built.Err
		turn.GenerateError = "Question and conversation context exceed the answer budget. Shorten the question or start a new conversation."
		d.persistStart(ctx, req, turn, mutation, false)
		return turn
	}
	turn.Prompt = built.Prompt
	turn.Citations = built.Context.Citations
	if answer := AnswerExplicitComparison(req.Query, turn.Citations); answer != "" {
		turn.Answer, turn.HasAnswer = answer, true
		d.persistStart(ctx, req, turn, mutation, false)
		return turn
	}
	if decline := missingLiteralAddress(req.Query, turn.Citations); decline != "" {
		turn.Answer, turn.HasAnswer = decline, true
		d.persistStart(ctx, req, turn, mutation, false)
		return turn
	}
	if decline := missingScopedCondition(req.Query, turn.Subject, turn.Citations); decline != "" {
		turn.Answer, turn.HasAnswer = decline, true
		d.persistStart(ctx, req, turn, mutation, false)
		return turn
	}

	turn, operationErr = d.startGeneration(ctx, req, turn, mutation)
	return turn
}

// startGeneration owns everything after prompt assembly: it dials the
// generator on a context detached from the starting request, registers the
// turn's event stream with the broker, and spawns the supervisor that drains
// the answer and persists the final turn. The returned error is StartTurn's
// operation outcome; every failure path also fills turn.GenerateError.
func (d *dependencies) startGeneration(ctx context.Context, req Request, turn Turn, mutation historyMutation) (Turn, error) {
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
		observability.StageContext(genCtx, d.log, "generation", "error", generationStarted, err)
		turn.GenerateError = failureMessage("Answer generation", err)
		d.persistStart(ctx, req, turn, mutation, false)
		return turn, err
	}

	turn.ID = uuid.NewString()
	stream, ok := d.broker.create(turn.ID)
	if !ok {
		cancel()
		err := errors.New("broker rejected generation")
		observability.StageContext(genCtx, d.log, "generation", "error", generationStarted, err)
		d.log.Warn("chat broker rejected generation",
			slog.String("operation_id", observability.OperationID(ctx)), slog.Int("max_retained_turns", d.broker.maxRetainedTurns))
		turn.GenerateError = "Answer generation is temporarily unavailable: too many active streams."
		d.persistStart(ctx, req, turn, mutation, false)
		return turn, err
	}
	stream.setCancel(cancel)
	// registerGeneration re-checks mutation currency atomically with the
	// registration, closing the window the earlier staleness guards leave open.
	if !d.mutations.registerGeneration(turn.ID, mutation, stream) {
		cancel()
		stream.finish()
		turn.ID = ""
		turn.GenerateError = "Conversation changed while generation was starting; please retry."
		return turn, errors.New("conversation changed while generation was starting")
	}
	turn.Streaming = true
	d.generations.Add(1)
	go func(supervisorTurn Turn) {
		defer d.generations.Done()
		d.consumeGeneration(genCtx, stream, req, supervisorTurn, mutation, events, generationStarted)
	}(turn)
	return turn, nil
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
	return d.mutations.deleteSession(ctx, sessionID)
}

// DeleteAllSessions removes every conversation through the chat lifecycle
// seam, preventing detached generation persistence from recreating turns.
func (d *dependencies) DeleteAllSessions(ctx context.Context) error {
	if d.history == nil {
		return errors.New("chat history is disabled")
	}
	return d.mutations.deleteAll(ctx)
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
func (d *dependencies) consumeGeneration(ctx context.Context, stream *turnStream, req Request, turn Turn, mutation historyMutation, events <-chan generation.Event, started time.Time) {
	ctx, operation := observability.Start(ctx, d.telemetry, d.log, "chat_stream", slog.String("turn_id", turn.ID))
	var operationErr error
	defer func() {
		outcome := "success"
		if operationErr != nil {
			outcome = "error"
		}
		operation.End(outcome, operationErr)
	}()
	defer func() {
		d.mutations.unregisterGeneration(turn.ID)
		stream.finish()
	}()

	var answer strings.Builder
	citations := citationStream{query: turn.Query, citations: turn.Citations}
	for ev := range events {
		switch ev.Kind {
		case generation.EventToken:
			text := citations.push(ev.Text)
			answer.WriteString(text)
			if text != "" {
				stream.publish(EventToken, text)
			}
		case generation.EventError:
			operationErr = ev.Err
			if operationErr == nil {
				operationErr = errors.New("generation failed")
			}
			turn.GenerateError = failureMessage("Answer generation", operationErr)
		case generation.EventDone:
		}
	}
	if pending := citations.flush(); pending != "" {
		answer.WriteString(pending)
		stream.publish(EventToken, pending)
	}
	if turn.GenerateError != "" {
		stream.publish(EventError, turn.GenerateError)
		observability.StageContext(ctx, d.log, "generation", "error", started, operationErr,
			slog.Int("answer_bytes", answer.Len()))
	} else {
		turn.Answer = answer.String()
		turn.HasAnswer = true
		stream.publish(EventDone, "")
		observability.StageContext(ctx, d.log, "generation", "success", started, nil,
			slog.Int("answer_bytes", answer.Len()))
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
	stop := context.AfterFunc(ctx, cancel)
	return events, func() {
		stop()
		cancel()
	}, true
}

// rewriteQuery rewrites a follow-up into a standalone query against the
// session's recent turns (Rewrite-Retrieve-Read). The rewritten query drives
// retrieval and supplies a reference-resolution hint for generation; the raw
// question remains authoritative and is what gets persisted.
func (d *dependencies) rewriteQuery(ctx context.Context, sessionID, query string) (string, string, *history.Subject) {
	turns, err := d.history.ListTurns(ctx, sessionID)
	if err != nil {
		d.log.Warn("rewrite skipped: list turns failed",
			slog.String("session_id", sessionID), slog.String("error_label", observability.ErrorLabel(err)))
		return query, "", nil
	}
	prior := make([]rewriting.Turn, 0, len(turns))
	for _, t := range turns {
		prior = append(prior, rewriting.Turn{Query: t.Query, Answer: referenceAnswer(t)})
	}
	if len(prior) == 0 {
		return query, "", nil
	}
	if len(prior) > d.rewriteTurns {
		prior = prior[len(prior)-d.rewriteTurns:]
	}
	// Reference context does not establish facts. It identifies what the user
	// is asking about even when the search rewrite omits intent or qualifiers.
	last := prior[len(prior)-1]
	lastTurn := turns[len(turns)-1]
	needsReference := needsTurnReference(query, lastTurn)
	reference := "\nPrior user question (reference context only): " + truncateToTokens(last.Query, 180) +
		"\nPrior answer (reference context only; verify claims against the document evidence): " + truncateToTokens(last.Answer, 180)
	if needsReference {
		subject := selectedSubject(lastTurn)
		if subject != nil {
			// Selection is already explicit in the retained conversation. An LLM
			// rewrite must not replace it with an adjacent alternative.
			return query + "\nConversation subject: " + subjectLabel(subject), reference, subject
		}
	}
	if d.rewriter == nil {
		if needsReference {
			return referenceSearchQuery(query, last), reference, nil
		}
		return query, "", nil
	}
	rewritten, err := d.rewriter.Rewrite(ctx, prior, query)
	if err != nil {
		d.log.Warn("rewrite failed; searching raw query",
			slog.String("session_id", sessionID), slog.String("error_label", observability.ErrorLabel(err)))
		return query, reference, nil
	}
	if rewritten == query && needsReference {
		// A successful but unchanged rewrite may still contain an unresolved
		// reference. Search with bounded prior user context rather than losing
		// the subject. Provider failures retain the established raw-query fallback.
		rewritten = referenceSearchQuery(query, last)
	}
	if rewritten == query && !needsReference {
		reference = ""
	}
	if rewritten != query {
		d.log.Info("rewrote follow-up query",
			slog.String("session_id", sessionID), slog.String("operation_id", observability.OperationID(ctx)))
	}
	return rewritten, reference, nil
}

// mintSession creates a conversation session for the first turn of a chat.
// Best-effort: failures are logged and return "" so the turn proceeds
// without a session.
func (d *dependencies) mintSession(ctx context.Context, query string) (string, historyMutation) {
	session, mutation, err := d.mutations.createSession(ctx, query)
	if err != nil {
		d.log.Warn("chat create session failed", slog.String("operation_id", observability.OperationID(ctx)), slog.String("error_label", observability.ErrorLabel(err)))
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
		ID:             turn.ID,
		Query:          req.Query,
		RewrittenQuery: turn.RewrittenQuery,
		Subject:        turn.Subject,
		AttachedFiles:  req.AttachedFiles,
		TopK:           req.TopK,
		Generate:       req.Generate,
		Results:        chunkResults(turn.Chunks),
		Citations:      citationResults(turn.Citations),
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
	if ht.Subject == nil && turn.HasAnswer && turn.GenerateError == "" {
		ht.Subject = selectedSubject(ht)
	}
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), d.persistTimeout)
	defer cancel()
	return d.mutations.append(cctx, mutation, ht, req.Query)
}

// logPersistErr reports a failed history append. Stale mutations are
// expected — the turn lost a race with a delete or edit — and are dropped;
// anything else is logged.
func (d *dependencies) logPersistErr(err error, sessionID string) {
	if errors.Is(err, errStaleHistoryMutation) {
		return
	}
	d.log.Warn("chat append turn failed", slog.String("session_id", sessionID), slog.String("error_label", observability.ErrorLabel(err)))
}

// persistStart keeps an edited turn together with its prune before the UI
// renders the replacement. Ordinary turns remain best-effort and detached
// so a slow history store cannot delay the live response.
func (d *dependencies) persistStart(ctx context.Context, req Request, turn Turn, mutation historyMutation, failed bool) {
	if req.Edit {
		if err := d.persistTurn(ctx, req, turn, mutation, failed); err != nil {
			d.logPersistErr(err, turn.SessionID)
		}
		return
	}
	d.persistDetached(ctx, req, turn, mutation, failed)
}

// persistDetached saves a turn in a best-effort, detached goroutine — a slow
// or unreachable store must never delay the response the user is watching.
func (d *dependencies) persistDetached(reqCtx context.Context, req Request, turn Turn, mutation historyMutation, failed bool) {
	if d.history == nil || turn.SessionID == "" {
		return
	}
	d.persists.Go(func() {
		if err := d.persistTurn(reqCtx, req, turn, mutation, failed); err != nil {
			d.logPersistErr(err, turn.SessionID)
		}
	})
}

// saveTurn persists a finished generation from the supervisor goroutine.
func (d *dependencies) saveTurn(req Request, turn Turn, mutation historyMutation) {
	if err := d.persistTurn(context.Background(), req, turn, mutation, false); err != nil {
		d.logPersistErr(err, turn.SessionID)
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
			FilePath:   ch.FilePath,
			Header:     ch.Header,
			LineStart:  ch.LineStart,
			ChunkIndex: ch.ChunkIndex,
			Score:      ch.Score,
			Text:       text,
			SourceSHA:  ch.SourceSHA,
		}
	}
	return out
}

func citationResults(citations []Citation) []history.Citation {
	if len(citations) == 0 {
		return nil
	}
	out := make([]history.Citation, len(citations))
	for i, citation := range citations {
		out[i] = history.Citation{
			Number: citation.Number, RetrievalRank: citation.RetrievalRank,
			FilePath: citation.FilePath, Header: citation.Header,
			LineStart: citation.LineStart, ChunkIndex: citation.ChunkIndex,
			SourceSHA: citation.SourceSHA, Text: citation.Text, Truncated: citation.Truncated,
		}
	}
	return out
}
