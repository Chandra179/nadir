package pkb

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type searchRequest struct {
	Query   string `json:"query"`
	TopK    int    `json:"top_k"`
	Keyword string `json:"keyword"`
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
	if req.Query == "" && req.Keyword == "" {
		http.Error(w, "query or keyword required", http.StatusBadRequest)
		return
	}
	topK := h.topK
	if req.TopK > 0 {
		topK = req.TopK
	}

	var chunks []ScoredChunk
	if req.Keyword != "" {
		var err error
		chunks, err = h.store.KeywordSearch(r.Context(), req.Keyword, topK)
		if err != nil {
			http.Error(w, "search failed", http.StatusInternalServerError)
			return
		}
	} else {
		var err error
		chunks, err = h.multiSearch(r, req.Query, topK)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
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

var sentenceSplit = regexp.MustCompile(`[.?;]+\s*`)

// multiSearch splits query into fragments, embeds each, merges results deduped by FilePath+LineStart keeping best score.
func (h *SearchHandler) multiSearch(r *http.Request, query string, topK int) ([]ScoredChunk, error) {
	fragments := splitFragments(query)
	seen := make(map[string]ScoredChunk)
	for _, frag := range fragments {
		vec, err := h.embedder.Embed(r.Context(), frag)
		if err != nil {
			return nil, fmt.Errorf("embed: %w", err)
		}
		results, err := h.store.HybridSearch(r.Context(), vec, frag, topK)
		if err != nil {
			return nil, fmt.Errorf("search failed")
		}
		for _, c := range results {
			key := c.FilePath + ":" + strconv.Itoa(c.LineStart)
			if existing, ok := seen[key]; !ok || c.Score > existing.Score {
				seen[key] = c
			}
		}
	}
	merged := make([]ScoredChunk, 0, len(seen))
	for _, c := range seen {
		merged = append(merged, c)
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].Score > merged[j].Score })
	if len(merged) > topK {
		merged = merged[:topK]
	}
	return merged, nil
}

func splitFragments(query string) []string {
	parts := sentenceSplit.Split(strings.TrimSpace(query), -1)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return []string{query}
	}
	return out
}
