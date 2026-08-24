// Chat transport: everything the chat UI talks to. Handlers here are pure
// HTTP — parse request, call the chat use-case (internal/chat) or history,
// map to a view, render a fragment. The page shell renders via "page",
// each exchange appends one "turn" fragment via htmx.
package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nadir/internal/chat"
	"nadir/internal/history"
	"nadir/internal/store"
)

// historySessionsView is the sidebar's chat list.
type historySessionsView struct {
	Enabled  bool
	Sessions []history.Session
}

// HistorySessions renders the sidebar's chat list — loaded on page load and
// again whenever RetrievalSearch fires the nadir:turn-appended event, so
// the list re-sorts live as conversations happen.
func (d *dependencies) HistorySessions(c *gin.Context) {
	view := historySessionsView{Enabled: d.history != nil}
	if d.history != nil {
		sessions, err := d.history.ListSessions(c.Request.Context(), 50)
		if err != nil {
			d.log.Warn("history list sessions failed", zap.Error(err))
		} else {
			view.Sessions = sessions
		}
	}

	d.renderHTML(c, http.StatusOK, "history-sessions", view)
}

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

// HistorySessionDelete permanently removes a chat session and all of its
// turns, invoked from the sidebar's delete modal.
func (d *dependencies) HistorySessionDelete(c *gin.Context) {
	if d.history == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "chat history is disabled"})
		return
	}

	sessionID := c.Param("id")
	if err := d.history.DeleteSession(c.Request.Context(), sessionID); err != nil {
		d.log.Warn("history delete session failed", zap.String("session_id", sessionID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "delete failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

func (d *dependencies) renderPage(c *gin.Context, data pageView) {
	d.renderHTML(c, http.StatusOK, "page", data)
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

// RetrievalSearch runs a chat turn through the chat use-case (search,
// optional buffered answer, best-effort persistence) and renders one
// appended turn: the question, the retrieval tool call, and the answer.
// Unlike POST /search, a requested answer is buffered in full rather than
// streamed, since it's rendered as a single fragment append.
func (d *dependencies) RetrievalSearch(c *gin.Context) {
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

	req := chat.Request{
		Query:         c.PostForm("query"),
		TopK:          topK,
		Filter:        filter,
		Generate:      c.PostForm("generate") == "on",
		SessionID:     c.PostForm("session_id"),
		AttachedFiles: attachedFileNames(c.PostForm("attached_files")),
	}

	res := d.chat.Ask(c.Request.Context(), req)

	// A session id is minted by the chat service on the first turn of a
	// conversation (never client-generated, so a client can't spoof/collide
	// another session's id) and handed back via a response header for the
	// composer to carry on subsequent posts.
	if res.SessionID != "" {
		c.Header("X-Nadir-Session-Id", res.SessionID)
		c.Header("HX-Trigger", "nadir:turn-appended")
	}

	d.renderTurn(c, d.turnViewFromResult(req, res))
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
	d.renderHTML(c, http.StatusOK, "turn", data)
}

// toRetrievalResultViews builds the display rows for a result set, then
// scales bar widths relative to the top score (see applyRelativeScores).
func toRetrievalResultViews(chunks []store.ScoredChunk) []retrievalResultView {
	views := make([]retrievalResultView, len(chunks))
	for i, ch := range chunks {
		text := ch.WindowText
		if text == "" {
			text = ch.Text
		}
		views[i] = retrievalResultView{
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
// [0,1] — RRF fusion and the reranker each produce their own ranges — so
// the bar width is scaled relative to the top score in this result set
// rather than against an assumed absolute max. Shared by the live path and
// history replay so both render identically.
func applyRelativeScores(views []retrievalResultView) {
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

// turnViewFromResult maps the chat use-case's result onto what the turn
// fragment renders.
func (d *dependencies) turnViewFromResult(req chat.Request, res chat.Result) turnView {
	views := toRetrievalResultViews(res.Chunks)
	return turnView{
		Error:         res.Error,
		Query:         req.Query,
		AttachedFiles: req.AttachedFiles,
		TopK:          req.TopK,
		Generate:      req.Generate,
		Results:       views,
		Count:         len(res.Chunks),
		ElapsedMS:     res.ElapsedMS,
		FromCache:     res.FromCache,
		Answer:        res.Answer,
		HasAnswer:     res.HasAnswer,
		Prompt:        res.Prompt,
		GenerateError: res.GenerateError,
	}
}

// historyTurnToView converts a persisted turn back into the same turnView
// shape RetrievalSearch renders live, so turn.html needs no changes to
// replay history.
func historyTurnToView(t history.Turn) turnView {
	results := make([]retrievalResultView, len(t.Results))
	for i, r := range t.Results {
		results[i] = retrievalResultView{
			FilePath:  r.FilePath,
			Header:    r.Header,
			LineStart: r.LineStart,
			Score:     r.Score,
			SourceSHA: r.SourceSHA,
			Text:      r.Text,
		}
	}
	applyRelativeScores(results)
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
