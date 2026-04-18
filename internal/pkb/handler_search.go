package pkb

import (
	"encoding/json"
	"net/http"
)

type searchRequest struct {
	Query string `json:"query"`
	TopK  int    `json:"top_k"`
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

// SearchHandler handles POST /search.
type SearchHandler struct {
	embedder Embedder
	store    Store
	topK     int
}

func NewSearchHandler(embedder Embedder, store Store, topK int) *SearchHandler {
	return &SearchHandler{embedder: embedder, store: store, topK: topK}
}

func (h *SearchHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req searchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.Query == "" {
		http.Error(w, "query required", http.StatusBadRequest)
		return
	}
	topK := h.topK
	if req.TopK > 0 {
		topK = req.TopK
	}

	vec, err := h.embedder.Embed(r.Context(), req.Query)
	if err != nil {
		http.Error(w, "embed failed", http.StatusInternalServerError)
		return
	}

	chunks, err := h.store.Search(r.Context(), vec, topK)
	if err != nil {
		http.Error(w, "search failed", http.StatusInternalServerError)
		return
	}

	results := make([]searchResult, len(chunks))
	for i, c := range chunks {
		results[i] = searchResult{
			FilePath:  c.FilePath,
			Header:    c.Header,
			LineStart: c.LineStart,
			Score:     c.Score,
			Text:      c.Text,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(searchResponse{Results: results})
}
