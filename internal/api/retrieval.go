package api

import (
	"context"
	"html/template"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nadir/internal/history"
	"nadir/internal/store"
)

// pageView is the data passed to the "page" template: the composer's
// default top_k, and — when replaying a past conversation via
// /history/sessions/:id — the session id to keep appending to and its
// turns to pre-render in place of the empty state.
type pageView struct {
	DefaultTopK int
	SessionID   string
	Turns       []turnView
}

// Retrieval renders the chat page shell (sidebar + composer, no messages
// yet) — the composer posts to RetrievalSearch, which appends one turn
// (question + trace + answer) as an HTML fragment via htmx.
func (d *dependencies) Retrieval(c *gin.Context) {
	topK := d.topK
	if topK <= 0 {
		topK = 8
	}
	d.renderPage(c, pageView{DefaultTopK: topK})
}

// HistorySession replays a persisted conversation: the full page shell,
// pre-populated with its turns, with the composer wired to keep appending
// to the same session.
func (d *dependencies) HistorySession(c *gin.Context) {
	topK := d.topK
	if topK <= 0 {
		topK = 8
	}

	if d.history == nil {
		c.String(http.StatusNotFound, "chat history is disabled")
		return
	}

	sessionID := c.Param("id")
	turns, err := d.history.ListTurns(c.Request.Context(), sessionID)
	if err != nil {
		d.log.Warn("history list turns failed", zap.String("session_id", sessionID), zap.Error(err))
		c.String(http.StatusNotFound, "session not found")
		return
	}

	views := make([]turnView, len(turns))
	for i, t := range turns {
		views[i] = historyTurnToView(t)
	}
	d.renderPage(c, pageView{DefaultTopK: topK, SessionID: sessionID, Turns: views})
}

func (d *dependencies) renderPage(c *gin.Context, data pageView) {
	tmpl, err := template.ParseFiles(
		"dashboard/search.html",
		"dashboard/partials/sidebar.html",
		"dashboard/partials/composer.html",
		"dashboard/partials/turn.html",
	)
	if err != nil {
		d.log.Error("parse retrieval template failed", zap.Error(err))
		c.String(http.StatusInternalServerError, "retrieval dashboard unavailable")
		return
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(c.Writer, "page", data); err != nil {
		d.log.Error("render retrieval template failed", zap.Error(err))
	}
}

type retrievalResultView struct {
	FilePath  string
	Header    string
	LineStart int
	Score     float32
	SourceSHA string
	ScorePct  int
	ScoreStr  string
	Text      string
}

// turnView is one exchange in the chat: the question, the retrieval tool
// call (request + retrieved chunks), and the generated answer, if any.
type turnView struct {
	Error         string
	Query         string
	AttachedFiles []string
	TopK          int
	Generate      bool
	Results       []retrievalResultView
	Count         int
	ElapsedMS     int64
	FromCache     bool
	Answer        string
	HasAnswer     bool
	Prompt        string
	// GenerateError is set when search succeeded but generation failed —
	// distinct from Error, which only covers a search-stage failure.
	GenerateError string
}

// RetrievalSearch runs a search from the chat composer and renders one
// appended turn: the question, the retrieval tool call, and the answer.
// Unlike POST /search, a requested answer is buffered in full rather than
// streamed, since it's rendered as a single fragment append.
func (d *dependencies) RetrievalSearch(c *gin.Context) {
	start := time.Now()

	query := c.PostForm("query")
	generate := c.PostForm("generate") == "on"
	sessionID := c.PostForm("session_id")

	topK := d.topK
	if topK <= 0 {
		topK = 8
	}
	if v, err := strconv.Atoi(c.PostForm("top_k")); err == nil && v > 0 {
		topK = v
	}

	var filter *store.SearchFilter
	if filePath, header, sha := c.PostForm("file_path"), c.PostForm("header"), c.PostForm("source_sha"); filePath != "" || header != "" || sha != "" {
		filter = &store.SearchFilter{FilePath: filePath, Header: header, SourceSHA: sha}
	}

	data := turnView{Query: query, AttachedFiles: attachedFileNames(c.PostForm("attached_files")), TopK: topK, Generate: generate}

	if query == "" {
		data.Error = "Enter a question to search."
		d.renderTurn(c, data)
		return
	}

	// A session id is minted on the first turn of a conversation (never
	// client-generated, so a client can't spoof/collide another session's
	// id) and handed back via a response header for the composer to carry
	// on subsequent posts. Must happen before any writes to c.Writer.
	if d.history != nil && sessionID == "" {
		if session, err := d.history.CreateSession(c.Request.Context(), query); err != nil {
			d.log.Warn("history create session failed", zap.Error(err))
		} else {
			sessionID = session.ID
		}
	}
	if sessionID != "" {
		c.Header("X-Nadir-Session-Id", sessionID)
		c.Header("HX-Trigger", "nadir:turn-appended")
	}

	chunks, fromCache, err := d.search.Query(c.Request.Context(), query, "", topK, filter, generate)
	if err != nil {
		d.log.Warn("retrieval chat search failed", zap.Error(err))
		data.Error = "Search failed: " + err.Error()
		d.renderTurn(c, data)
		d.persistTurn(c.Request.Context(), sessionID, data, true)
		return
	}

	if generate && d.generator != nil && len(chunks) > 0 {
		prompt, stream, err := d.generator.Generate(c.Request.Context(), query, chunks)
		data.Prompt = prompt
		if err != nil {
			d.log.Warn("retrieval chat generate failed", zap.Error(err))
			data.GenerateError = "Answer generation failed: " + err.Error()
		} else {
			answer, err := io.ReadAll(stream)
			stream.Close()
			if err != nil {
				d.log.Warn("retrieval chat generate stream read failed", zap.Error(err))
				data.GenerateError = "Answer generation failed: " + err.Error()
			} else {
				data.Answer = string(answer)
				data.HasAnswer = true
			}
		}
	}

	data.Results = toRetrievalResultViews(chunks)
	data.Count = len(chunks)
	data.ElapsedMS = time.Since(start).Milliseconds()
	data.FromCache = fromCache

	d.renderTurn(c, data)
	d.persistTurn(c.Request.Context(), sessionID, data, false)
}

// persistTurn saves a turn to history in a best-effort, detached goroutine
// kicked off after the live response has already been written — a slow or
// unreachable Qdrant must never delay or break the chat the user is
// actually watching. Failures are logged and dropped, never retried.
func (d *dependencies) persistTurn(reqCtx context.Context, sessionID string, data turnView, failed bool) {
	if d.history == nil || sessionID == "" {
		return
	}

	turn := history.Turn{
		Query:         data.Query,
		AttachedFiles: data.AttachedFiles,
		TopK:          data.TopK,
		Generate:      data.Generate,
		Results:       toHistoryResults(data.Results),
		Count:         data.Count,
		ElapsedMS:     data.ElapsedMS,
		FromCache:     data.FromCache,
		Prompt:        data.Prompt,
		Answer:        data.Answer,
		HasAnswer:     data.HasAnswer,
		Error:         data.Error,
		GenerateError: data.GenerateError,
		Failed:        failed,
	}
	if d.cfg != nil {
		turn.Model = d.cfg.Generator.Model
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(reqCtx), 5*time.Second)
		defer cancel()
		if err := d.history.AppendTurn(ctx, sessionID, turn, data.Query); err != nil {
			d.log.Warn("history append turn failed", zap.String("session_id", sessionID), zap.Error(err))
		}
	}()
}

// attachedFileNames splits the comma-joined "attached_files" field the
// composer sends alongside a query — the names of files imported just
// before the message was sent, shown above the question for context. This
// is display-only: the files were already chunked/embedded/upserted at
// import time, independent of this search, and aren't used to scope it.
func attachedFileNames(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	names := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			names = append(names, p)
		}
	}
	return names
}

func (d *dependencies) renderTurn(c *gin.Context, data turnView) {
	tmpl, err := template.ParseFiles("dashboard/partials/turn.html")
	if err != nil {
		d.log.Error("parse turn template failed", zap.Error(err))
		c.String(http.StatusInternalServerError, "")
		return
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(c.Writer, "turn", data); err != nil {
		d.log.Error("render turn fragment failed", zap.Error(err))
	}
}

// toRetrievalResultViews builds the display rows for a result set. Scores
// aren't bounded to [0,1] — RRF fusion and the reranker each produce their
// own ranges — so the bar width is scaled relative to the top score in this
// result set rather than against an assumed absolute max.
func toRetrievalResultViews(chunks []store.ScoredChunk) []retrievalResultView {
	var maxScore float32
	for _, ch := range chunks {
		maxScore = max(maxScore, ch.Score)
	}

	views := make([]retrievalResultView, len(chunks))
	for i, ch := range chunks {
		text := ch.WindowText
		if text == "" {
			text = ch.Text
		}
		pct := 100
		if maxScore > 0 {
			pct = int(ch.Score / maxScore * 100)
			if pct < 4 {
				pct = 4
			}
		}
		views[i] = retrievalResultView{
			FilePath:  ch.FilePath,
			Header:    ch.Header,
			LineStart: ch.LineStart,
			Score:     ch.Score,
			SourceSHA: ch.SourceSHA,
			ScorePct:  pct,
			ScoreStr:  strconv.FormatFloat(float64(ch.Score), 'f', 3, 32),
			Text:      text,
		}
	}
	return views
}

func toHistoryResults(results []retrievalResultView) []history.TurnResult {
	if len(results) == 0 {
		return nil
	}
	out := make([]history.TurnResult, len(results))
	for i, r := range results {
		out[i] = history.TurnResult{
			FilePath:  r.FilePath,
			Header:    r.Header,
			LineStart: r.LineStart,
			Score:     r.Score,
			Text:      r.Text,
			SourceSHA: r.SourceSHA,
		}
	}
	return out
}

// historyTurnToView converts a persisted turn back into the same turnView
// shape RetrievalSearch renders live, so turn.html needs no changes to
// replay history.
func historyTurnToView(t history.Turn) turnView {
	results := make([]retrievalResultView, len(t.Results))
	var maxScore float32
	for _, r := range t.Results {
		maxScore = max(maxScore, r.Score)
	}
	for i, r := range t.Results {
		pct := 100
		if maxScore > 0 {
			pct = int(r.Score / maxScore * 100)
			if pct < 4 {
				pct = 4
			}
		}
		results[i] = retrievalResultView{
			FilePath:  r.FilePath,
			Header:    r.Header,
			LineStart: r.LineStart,
			Score:     r.Score,
			SourceSHA: r.SourceSHA,
			ScorePct:  pct,
			ScoreStr:  strconv.FormatFloat(float64(r.Score), 'f', 3, 32),
			Text:      r.Text,
		}
	}
	return turnView{
		Error:         t.Error,
		Query:         t.Query,
		AttachedFiles: t.AttachedFiles,
		TopK:          t.TopK,
		Generate:      t.Generate,
		Results:       results,
		Count:         t.Count,
		ElapsedMS:     t.ElapsedMS,
		FromCache:     t.FromCache,
		Answer:        t.Answer,
		HasAnswer:     t.HasAnswer,
		Prompt:        t.Prompt,
		GenerateError: t.GenerateError,
	}
}
