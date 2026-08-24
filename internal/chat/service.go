package chat

import (
	"context"
	"go.uber.org/zap"
	"io"
	"time"

	"nadir/internal/history"
	"nadir/internal/store"
)

// Ask runs one full chat turn: validate → mint session (first turn of a
// conversation) → retrieve → optionally buffer-generate an answer → kick
// off best-effort persistence. It never returns an error; failures land in
// Result.Error / Result.GenerateError so the caller can always render a
// turn.
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

	chunks, fromCache, err := d.searcher.Query(ctx, req.Query, "", req.TopK, req.Filter, req.Generate)
	res.FromCache = fromCache
	if err != nil {
		d.log.Warn("chat search failed", zap.String("query", req.Query), zap.Error(err))
		res.Error = "Search failed: " + err.Error()
		d.persist(ctx, req, res, true)
		return res
	}
	res.Chunks = chunks

	if req.Generate && d.generator != nil && len(chunks) > 0 {
		d.generateAnswer(ctx, req.Query, chunks, &res)
	}

	res.ElapsedMS = time.Since(start).Milliseconds()
	d.persist(ctx, req, res, false)
	return res
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
