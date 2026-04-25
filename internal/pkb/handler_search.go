package pkb

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
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
	embedder     Embedder
	store        Store
	topK         int
	reranker     Reranker     // nil = disabled
	candidateMul int          // fetch topK*candidateMul candidates when reranking (default 3)
	hyde         *HyDESearcher // nil = disabled; replaces embedding step when set
}

func NewSearchHandler(embedder Embedder, store Store, topK int) *SearchHandler {
	return &SearchHandler{embedder: embedder, store: store, topK: topK}
}

// WithReranker enables cross-encoder re-ranking. candidateMul controls oversampling (e.g. 3 → fetch 3× candidates before rerank).
func (h *SearchHandler) WithReranker(r Reranker, candidateMul int) *SearchHandler {
	h.reranker = r
	if candidateMul < 1 {
		candidateMul = 3
	}
	h.candidateMul = candidateMul
	return h
}

// WithHyDE enables Hypothetical Document Embedding retrieval.
// When set, query embedding is replaced by generating a hypothetical document via LLM,
// embedding that instead, then searching. Falls back to standard search on generation error.
func (h *SearchHandler) WithHyDE(s *HyDESearcher) *SearchHandler {
	h.hyde = s
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

	fetchN := topK
	if h.reranker != nil {
		fetchN = topK * h.candidateMul
	}

	var chunks []ScoredChunk
	if req.Keyword != "" {
		var err error
		chunks, err = h.store.KeywordSearch(r.Context(), req.Keyword, fetchN)
		if err != nil {
			http.Error(w, "search failed", http.StatusInternalServerError)
			return
		}
	} else if h.hyde != nil {
		var err error
		chunks, err = h.hyde.Search(r.Context(), req.Query, fetchN)
		if err != nil {
			// fall back to standard search on HyDE failure
			chunks, err = h.multiSearch(r, req.Query, fetchN)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
	} else {
		var err error
		chunks, err = h.multiSearch(r, req.Query, fetchN)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if h.reranker != nil && len(chunks) > 0 {
		reranked, err := h.reranker.Rerank(r.Context(), req.Query, chunks)
		if err != nil {
			http.Error(w, "rerank failed", http.StatusInternalServerError)
			return
		}
		chunks = reranked
		if len(chunks) > topK {
			chunks = chunks[:topK]
		}
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
			key := c.Key()
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
