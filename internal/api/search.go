package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"nadir/internal/store"
)

type searchRequest struct {
	Query    string              `json:"query"`
	TopK     int                 `json:"top_k"`
	Keyword  string              `json:"keyword"`
	Generate bool                `json:"generate"`
	Filter   *store.SearchFilter `json:"filter,omitempty"`
}

type searchResult struct {
	FilePath  string  `json:"file_path"`
	Header    string  `json:"header"`
	LineStart int     `json:"line_start"`
	Score     float32 `json:"score"`
	Text      string  `json:"text"`
}

type searchResponse struct {
	Results []searchResult `json:"results"`
}

func (d *dependencies) Search(c *gin.Context) {
	var req searchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.String(http.StatusBadRequest, "bad request")
		return
	}
	if req.Query == "" && req.Keyword == "" {
		c.String(http.StatusBadRequest, "query or keyword required")
		return
	}
	topK := d.topK
	if req.TopK > 0 {
		topK = req.TopK
	}

	chunks, _, err := d.search.Query(c.Request.Context(), req.Query, req.Keyword, topK, req.Filter, req.Generate)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	if req.Generate && d.generator != nil && req.Query != "" && len(chunks) > 0 {
		stream, err := d.generator.Generate(c.Request.Context(), req.Query, chunks)
		if err != nil {
			c.String(http.StatusInternalServerError, "generate failed")
			return
		}
		defer stream.Close()
		c.Header("Content-Type", "text/plain; charset=utf-8")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Status(http.StatusOK)
		buf := make([]byte, 512)
		for {
			n, err := stream.Read(buf)
			if n > 0 {
				c.Writer.Write(buf[:n])
				c.Writer.Flush()
			}
			if err != nil {
				break
			}
		}
		return
	}

	c.JSON(http.StatusOK, searchResponse{Results: toSearchResults(chunks)})
}

func toSearchResults(chunks []store.ScoredChunk) []searchResult {
	results := make([]searchResult, len(chunks))
	for i, c := range chunks {
		text := c.WindowText
		if text == "" {
			text = c.Text
		}
		results[i] = searchResult{
			FilePath:  c.FilePath,
			Header:    c.Header,
			LineStart: c.LineStart,
			Score:     c.Score,
			Text:      text,
		}
	}
	return results
}
