package chat

import (
	"context"
	"go.uber.org/zap"
	"io"
	"time"

	"nadir/internal/history"
	"nadir/internal/rewriter"
	"nadir/internal/store"
)

// Ask runs one full chat turn: mint session (first turn) → rewrite
// follow-ups → retrieve → optionally generate → best-effort persist. Never
// returns an error: failures land in Result.Error/Result.GenerateError.
func (d *dependencies) Ask(ctx context.Context, req Request) Result {
	res := Result{SessionID: req.SessionID}

	if req.Query == "" {
		res.Error = "Enter a question to search."
		return res
	}

	start := time.Now()

	if d.history != nil && req.SessionID == "" {
		res.SessionID = d.mintSession(ctx, req.Query)
	}

	retrievalQuery := req.Query
	if d.rewriter != nil && d.history != nil && req.SessionID != "" {
		retrievalQuery = d.rewriteQuery(ctx, req.SessionID, req.Query)
	}

	chunks, fromCache, err := d.searcher.Query(ctx, retrievalQuery, "", req.TopK, req.Filter, req.Generate)
	res.FromCache = fromCache
	if err != nil {
		d.log.Warn("chat search failed", zap.String("query", req.Query), zap.Error(err))
		res.Error = "Search failed: " + err.Error()
		d.persist(ctx, req, res, true)
		return res
	}
	res.Chunks = chunks

	if req.Generate && d.generator != nil && len(chunks) > 0 {
		d.generateAnswer(ctx, retrievalQuery, chunks, &res)
	}

	res.ElapsedMS = time.Since(start).Milliseconds()
	d.persist(ctx, req, res, false)
	return res
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

// generateAnswer buffers one LLM answer into res. Best-effort: any failure
// lands in res.GenerateError while the retrieval results still render.
func (d *dependencies) generateAnswer(ctx context.Context, query string, chunks []store.ScoredChunk, res *Result) {
	prompt, stream, err := d.generator.Generate(ctx, query, chunks)
	res.Prompt = prompt
	if err != nil {
		d.log.Warn("chat generate failed", zap.String("query", query), zap.Error(err))
		res.GenerateError = "Answer generation failed: " + err.Error()
		return
	}

	answer, readErr := io.ReadAll(stream)
	stream.Close()
	if readErr != nil {
		d.log.Warn("chat generate stream read failed", zap.Error(readErr))
		res.GenerateError = "Answer generation failed: " + readErr.Error()
		return
	}

	res.Answer = string(answer)
	res.HasAnswer = true
}

// persist saves a turn to history in a best-effort, detached goroutine —
// a slow or unreachable store must never delay or break the response the
// user is already watching. Failures are logged and dropped, never retried.
func (d *dependencies) persist(reqCtx context.Context, req Request, res Result, failed bool) {
	if d.history == nil || res.SessionID == "" {
		return
	}

	turn := history.Turn{
		Query:         req.Query,
		AttachedFiles: req.AttachedFiles,
		TopK:          req.TopK,
		Generate:      req.Generate,
		Results:       chunkResults(res.Chunks),
		Count:         len(res.Chunks),
		ElapsedMS:     res.ElapsedMS,
		FromCache:     res.FromCache,
		Prompt:        res.Prompt,
		Answer:        res.Answer,
		HasAnswer:     res.HasAnswer,
		Error:         res.Error,
		GenerateError: res.GenerateError,
		Model:         d.model,
		Failed:        failed,
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(reqCtx), 5*time.Second)
		defer cancel()
		if err := d.history.AppendTurn(ctx, res.SessionID, turn, req.Query); err != nil {
			d.log.Warn("chat append turn failed", zap.String("session_id", res.SessionID), zap.Error(err))
		}
	}()
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
