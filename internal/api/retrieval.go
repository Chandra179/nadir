package api

import (
	"fmt"
	"html/template"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nadir/internal/store"
)

// Retrieval renders the retrieval dashboard page. It's a static shell (no
// live data) — the search form posts to RetrievalSearch, which renders the
// results fragment.
func (d *dependencies) Retrieval(c *gin.Context) {
	tmpl, err := template.ParseFiles("dashboard/search.html")
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
	if err := tmpl.Execute(c.Writer, struct{ DefaultTopK int }{topK}); err != nil {
		d.log.Error("render retrieval template failed", zap.Error(err))
	}
}

var retrievalResultsTmpl = template.Must(template.New("retrieval-results").Parse(`
{{if .Error}}
<p class="error-hint">{{.Error}}</p>
{{else}}
{{if .Answer}}
<div class="answer-panel">
<div class="a-label">Generated answer</div>
<div class="a-text">{{.Answer}}</div>
</div>
{{end}}
<div class="flex items-baseline justify-between mb-3">
<h2 class="text-[14.5px] font-semibold" style="font-family:'Public Sans';">Results</h2>
<span class="text-[12.5px] text-faint">{{.Count}} chunk{{if ne .Count 1}}s{{end}} · {{.ElapsedMS}}ms{{if .FromCache}} · cached{{end}}</span>
</div>
{{if .Results}}
<div class="flex flex-col gap-2.5">
{{range .Results}}
<div class="card result-card">
<div class="result-top">
<span class="path-crumb"><b>{{.FilePath}}</b>{{if .Header}} · {{.Header}}{{end}} · L{{.LineStart}}</span>
<span class="result-score"><span class="score-bar"><i style="width:{{.ScorePct}}%"></i></span><span class="score-val">{{.ScoreStr}}</span></span>
</div>
<div class="result-snippet">{{.Text}}</div>
</div>
{{end}}
</div>
{{else}}
<p class="empty-hint">No results.</p>
{{end}}
{{end}}
`))

type retrievalResultView struct {
	FilePath  string
	Header    string
	LineStart int
	ScorePct  int
	ScoreStr  string
	Text      string
}

// RetrievalSearch runs a search from the dashboard form and renders an HTML
// fragment (answer + result cards), mirroring what POST /search returns as
// JSON. Unlike /search, a requested answer is buffered in full rather than
// streamed, since it's rendered as a single fragment swap.
func (d *dependencies) RetrievalSearch(c *gin.Context) {
	start := time.Now()

	query := c.PostForm("query")
	mode := c.PostForm("mode")
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

	var semanticQuery, keyword string
	if mode == "keyword" {
		keyword = query
	} else {
		semanticQuery = query
	}

	data := struct {
		Error     string
		Answer    string
		Results   []retrievalResultView
		Count     int
		ElapsedMS int64
		FromCache bool
	}{}

	if semanticQuery == "" && keyword == "" {
		data.Error = "Enter a query to search."
		d.renderRetrievalResults(c, data)
		return
	}

	chunks, fromCache, err := d.search.Query(c.Request.Context(), semanticQuery, keyword, topK, filter, generate)
	if err != nil {
		d.log.Warn("retrieval dashboard search failed", zap.Error(err))
		data.Error = "Search failed: " + err.Error()
		d.renderRetrievalResults(c, data)
		return
	}

	if generate && d.generator != nil && semanticQuery != "" && len(chunks) > 0 {
		stream, err := d.generator.Generate(c.Request.Context(), semanticQuery, chunks)
		if err != nil {
			d.log.Warn("retrieval dashboard generate failed", zap.Error(err))
		} else {
			answer, err := io.ReadAll(stream)
			stream.Close()
			if err != nil {
				d.log.Warn("retrieval dashboard generate stream read failed", zap.Error(err))
			} else {
				data.Answer = string(answer)
			}
		}
	}

	data.Results = toRetrievalResultViews(chunks)
	data.Count = len(chunks)
	data.ElapsedMS = time.Since(start).Milliseconds()
	data.FromCache = fromCache

	d.renderRetrievalResults(c, data)
}

func (d *dependencies) renderRetrievalResults(c *gin.Context, data any) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := retrievalResultsTmpl.Execute(c.Writer, data); err != nil {
		d.log.Error("render retrieval results fragment failed", zap.Error(err))
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
			ScoreStr:  fmt.Sprintf("%.3f", ch.Score),
			Text:      text,
		}
	}
	return views
}
