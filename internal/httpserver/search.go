package httpserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"nadir/internal/pkb"
)

type searchRequest struct {
	Query    string            `json:"query"`
	TopK     int               `json:"top_k"`
	Keyword  string            `json:"keyword"`
	Generate bool              `json:"generate"`
	Filter   *pkb.SearchFilter `json:"filter,omitempty"`
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

type SearchHandler struct {
	searcher      *pkb.SearchService
	topK          int
	generator     pkb.Generator
	semanticCache *pkb.SemanticCache
}

func NewSearchHandler(searcher *pkb.SearchService, topK int) *SearchHandler {
	return &SearchHandler{searcher: searcher, topK: topK}
}

func (h *SearchHandler) WithGenerator(g pkb.Generator) *SearchHandler {
	h.generator = g
	return h
}

func (h *SearchHandler) WithSemanticCache(sc *pkb.SemanticCache) *SearchHandler {
	h.semanticCache = sc
	return h
}

func (h *SearchHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req searchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.Query == "" && req.Keyword == "" {
		http.Error(w, "query or keyword required", http.StatusBadRequest)
		return
	}
	topK := h.topK
	if req.TopK > 0 {
		topK = req.TopK
	}

	if h.semanticCache != nil && req.Query != "" && !req.Generate {
		if cached, hit, err := h.semanticCache.Get(r.Context(), req.Query); err == nil && hit {
			if len(cached) > topK {
				cached = cached[:topK]
			}
			results := make([]searchResult, len(cached))
			for i, c := range cached {
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
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(searchResponse{Results: results})
			return
		}
	}

	var chunks []pkb.ScoredChunk
	var err error
	if req.Keyword != "" {
		chunks, err = h.searcher.KeywordSearch(r.Context(), req.Keyword, topK, req.Filter)
	} else {
		chunks, err = h.searcher.Search(r.Context(), req.Query, topK, req.Filter)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if h.semanticCache != nil && req.Query != "" && len(chunks) > 0 {
		go func() {
			_ = h.semanticCache.Set(context.Background(), req.Query, chunks)
		}()
	}

	if req.Generate && h.generator != nil && req.Query != "" && len(chunks) > 0 {
		stream, err := h.generator.Generate(r.Context(), req.Query, chunks)
		if err != nil {
			http.Error(w, "generate failed", http.StatusInternalServerError)
			return
		}
		defer stream.Close()
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Transfer-Encoding", "chunked")
		buf := make([]byte, 512)
		if f, ok := w.(http.Flusher); ok {
			for {
				n, err := stream.Read(buf)
				if n > 0 {
					w.Write(buf[:n])
					f.Flush()
				}
				if err != nil {
					break
				}
			}
		} else {
			n, _ := stream.Read(buf)
			if n > 0 {
				w.Write(buf[:n])
			}
			io.Copy(w, stream)
		}
		return
	}

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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(searchResponse{Results: results})
}
