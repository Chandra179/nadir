package api

import (
	"html/template"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nadir/internal/store"
)

// Retrieval renders the chat page shell (sidebar + composer, no messages
// yet) — the composer posts to RetrievalSearch, which appends one turn
// (question + trace + answer) as an HTML fragment via htmx.
func (d *dependencies) Retrieval(c *gin.Context) {
	tmpl, err := template.ParseFiles(
		"dashboard/search.html",
		"dashboard/partials/sidebar.html",
		"dashboard/partials/composer.html",
	)
	if err != nil {
		d.log.Error("parse retrieval template failed", zap.Error(err))
		c.String(http.StatusInternalServerError, "retrieval dashboard unavailable")
		return
	}

	topK := d.topK
	if topK <= 0 {
		topK = 8
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(c.Writer, "page", struct{ DefaultTopK int }{topK}); err != nil {
		d.log.Error("render retrieval template failed", zap.Error(err))
	}
}

type retrievalResultView struct {
	FilePath  string
	Header    string
	LineStart int
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
}

// RetrievalSearch runs a search from the chat composer and renders one
// appended turn: the question, the retrieval tool call, and the answer.
// Unlike POST /search, a requested answer is buffered in full rather than
// streamed, since it's rendered as a single fragment append.
func (d *dependencies) RetrievalSearch(c *gin.Context) {
	start := time.Now()

	query := c.PostForm("query")
	generate := c.PostForm("generate") == "on"

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

	chunks, fromCache, err := d.search.Query(c.Request.Context(), query, "", topK, filter, generate)
	if err != nil {
		d.log.Warn("retrieval chat search failed", zap.Error(err))
		data.Error = "Search failed: " + err.Error()
		d.renderTurn(c, data)
		return
	}

	if generate && d.generator != nil && len(chunks) > 0 {
		prompt, stream, err := d.generator.Generate(c.Request.Context(), query, chunks)
		data.Prompt = prompt
		if err != nil {
			d.log.Warn("retrieval chat generate failed", zap.Error(err))
		} else {
			answer, err := io.ReadAll(stream)
			stream.Close()
			if err != nil {
				d.log.Warn("retrieval chat generate stream read failed", zap.Error(err))
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
			ScorePct:  pct,
			ScoreStr:  strconv.FormatFloat(float64(ch.Score), 'f', 3, 32),
			Text:      text,
		}
	}
	return views
}
