package pkb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"nadir/pkg/otel"
)

type searchRequest struct {
	Query    string `json:"query"`
	TopK     int    `json:"top_k"`
	Keyword  string `json:"keyword"`
	Generate bool   `json:"generate"` // if true, pipe chunks into LLM and stream answer
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
	embedder      Embedder
	store         Store
	topK          int
	reranker      Reranker       // nil = disabled
	candidateMul  int            // fetch topK*candidateMul candidates when reranking (default 3)
	hyde          *HyDESearcher  // nil = disabled; replaces embedding step when set
	semanticCache *SemanticCache // nil = disabled
	generator     Generator      // nil = disabled; streams LLM answer when generate:true in request
	metrics       *otel.Metrics  // nil = no-op
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

// WithSemanticCache enables query-result caching. Cache is checked before any embedding or search.
// On miss, results are written to the cache after retrieval.
func (h *SearchHandler) WithSemanticCache(sc *SemanticCache) *SearchHandler {
	h.semanticCache = sc
	return h
}

// WithGenerator enables LLM answer generation from retrieved chunks.
// When the request includes "generate": true, chunks are passed to the generator
// and the answer is streamed back as text/plain.
func (h *SearchHandler) WithGenerator(g Generator) *SearchHandler {
	h.generator = g
	return h
}

// WithMetrics attaches an otel.Metrics recorder for instrumentation.
func (h *SearchHandler) WithMetrics(m *otel.Metrics) *SearchHandler {
	h.metrics = m
	return h
}

func (h *SearchHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
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

	// semantic cache: only applies to vector queries (not keyword-only), skip when generate=true
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
			h.metrics.RecordCacheHit(r.Context())
			h.metrics.RecordSearch(r.Context(), time.Since(start), len(results), h.searchMode(req))
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(searchResponse{Results: results})
			return
		}
		h.metrics.RecordCacheMiss(r.Context())
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
		rerankStart := time.Now()
		var scoreBefore float32
		if len(chunks) > 0 {
			scoreBefore = chunks[0].Score
		}
		reranked, err := h.reranker.Rerank(r.Context(), req.Query, chunks)
		if err != nil {
			http.Error(w, "rerank failed", http.StatusInternalServerError)
			return
		}
		var scoreAfter float32
		if len(reranked) > 0 {
			scoreAfter = reranked[0].Score
		}
		h.metrics.RecordRerank(r.Context(), time.Since(rerankStart), scoreBefore, scoreAfter)
		chunks = reranked
		if len(chunks) > topK {
			chunks = chunks[:topK]
		}
	}

	// populate cache on miss (fire-and-forget; don't block response)
	if h.semanticCache != nil && req.Query != "" && len(chunks) > 0 {
		go func() {
			_ = h.semanticCache.Set(context.Background(), req.Query, chunks)
		}()
	}

	h.metrics.RecordSearch(r.Context(), time.Since(start), len(chunks), h.searchMode(req))

	// generate: stream LLM answer grounded in retrieved chunks
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
		if f, ok := w.(http.Flusher); ok {
			buf := make([]byte, 512)
			for {
				n, err := stream.Read(buf)
				if n > 0 {
					w.Write(buf[:n]) //nolint:errcheck
					f.Flush()
				}
				if err != nil {
					break
				}
			}
		} else {
			io.Copy(w, stream) //nolint:errcheck
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

func (h *SearchHandler) searchMode(req searchRequest) string {
	switch {
	case req.Keyword != "":
		return "keyword"
	case h.hyde != nil:
		return "hyde"
	case h.reranker != nil:
		return "hybrid+rerank"
	default:
		return "hybrid"
	}
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
