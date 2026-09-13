package search

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"nadir/internal/platform/observability"
	semanticcache "nadir/internal/retrieval/cache"

	"go.uber.org/zap"
)

var sentenceSplit = regexp.MustCompile(`[.?;]+\s*`)

func (s *dependencies) search(ctx context.Context, query string, topK int, filter *Filter) ([]SearchCandidate, error) {
	fetchN := topK
	if s.reranker != nil {
		fetchN = topK * s.candidateMul
	}

	chunks, err := s.multiSearch(ctx, query, fetchN, filter)

	if err != nil {
		return nil, err
	}

	return s.rerankTopK(ctx, query, chunks, topK), nil
}

// Query is the top-level Retrieval entry point. It normalizes the request,
// dispatches to keyword or semantic search, consults the semantic cache, and
// returns storage-independent chunks to the caller.
func (s *dependencies) Query(ctx context.Context, request Request) (Result, error) {
	started := time.Now()
	finish := func(outcome string, err error, fields ...zap.Field) {
		observability.Stage(s.log, "retrieval", outcome, started, err, fields...)
	}
	query, keyword, topK := request.Query, request.Keyword, request.TopK
	filter := request.Filter
	if keyword == "" {
		if err := s.validateQuery(query, topK); err != nil {
			finish("error", err)
			return Result{}, err
		}
	} else {
		if len([]rune(strings.TrimSpace(keyword))) > s.maxQueryChars {
			finish("error", errQueryTooLong)
			return Result{}, errQueryTooLong
		}
		if topK <= 0 {
			err := fmt.Errorf("search top_k must be greater than zero")
			finish("error", err)
			return Result{}, err
		}
	}
	if topK > s.maxTopK {
		topK = s.maxTopK
	}
	if keyword != "" {
		chunks, err := s.keywordSearch(ctx, keyword, topK, filter)
		outcome := "success"
		if err != nil {
			outcome = "error"
		}
		finish(outcome, err,
			zap.Bool("keyword", true), zap.Int("results", len(chunks)))
		return Result{Chunks: fromStoreChunks(chunks)}, err
	}

	if cached, ok := s.getCached(ctx, query, topK, filter, request.SkipCache); ok {
		finish("cache_hit", nil, zap.Bool("from_cache", true), zap.Int("results", len(cached)))
		return Result{Chunks: fromStoreChunks(cached), FromCache: true}, nil
	}

	chunks, err := s.search(ctx, query, topK, filter)
	if err != nil {
		finish("error", err)
		return Result{}, err
	}

	if s.cache != nil && isEmptyFilter(filter) && query != "" && len(chunks) > 0 {
		go func() {
			cacheStarted := time.Now()
			err := s.cache.Set(context.Background(), query, toCacheCandidates(chunks))
			outcome := "success"
			if err != nil {
				outcome = "error"
			}
			observability.Stage(s.log, "cache_write", outcome, cacheStarted, err,
				zap.Int("results", len(chunks)))
		}()
	}

	finish("success", nil, zap.Bool("from_cache", false), zap.Int("results", len(chunks)))
	return Result{Chunks: fromStoreChunks(chunks)}, nil
}

func fromStoreChunks(chunks []SearchCandidate) []Chunk {
	if len(chunks) == 0 {
		return nil
	}
	out := make([]Chunk, len(chunks))
	for i, chunk := range chunks {
		out[i] = Chunk{
			Text:       chunk.Text,
			WindowText: chunk.WindowText,
			FilePath:   chunk.FilePath,
			Header:     chunk.Header,
			LineStart:  chunk.LineStart,
			ChunkIndex: chunk.ChunkIndex,
			SourceSHA:  chunk.SourceSHA,
			Score:      chunk.Score,
		}
	}
	return out
}

// getCached consults the semantic cache unless the caller asked to skip it.
// Returns false on miss or cache error so lookups stay best-effort; hits
// are truncated to topK to match a fresh search's result size.
func (s *dependencies) getCached(ctx context.Context, query string, topK int, filter *Filter, skip bool) ([]SearchCandidate, bool) {
	started := time.Now()
	if s.cache == nil || skip || query == "" || !isEmptyFilter(filter) {
		return nil, false
	}
	cached, hit, err := s.cache.Get(ctx, query)
	if err != nil {
		observability.Stage(s.log, "cache_read", "error", started, err)
		return nil, false
	}
	if !hit {
		observability.Stage(s.log, "cache_read", "miss", started, nil)
		return nil, false
	}
	if len(cached) > topK {
		cached = cached[:topK]
	}
	observability.Stage(s.log, "cache_read", "hit", started, nil, zap.Int("results", len(cached)))
	return fromCacheCandidates(cached), true
}

func isEmptyFilter(filter *Filter) bool {
	return filter == nil || (filter.FilePath == "" && filter.Header == "" && filter.SourceSHA == "")
}

func (s *dependencies) keywordSearch(ctx context.Context, keyword string, topK int, filter *Filter) ([]SearchCandidate, error) {
	fetchN := topK
	if s.reranker != nil {
		fetchN = topK * s.candidateMul
	}

	chunks, err := s.store.KeywordSearch(ctx, keyword, fetchN, filter)
	if err != nil {
		return nil, err
	}

	return s.rerankTopK(ctx, keyword, chunks, topK), nil
}

// rerankTopK re-scores candidates with the cross-encoder when configured,
// keeping the best topK. Best-effort: on reranker failure the original
// score ordering is retained, but the result count remains bounded.
func (s *dependencies) rerankTopK(ctx context.Context, query string, chunks []SearchCandidate, topK int) []SearchCandidate {
	if s.reranker == nil || len(chunks) == 0 {
		return chunks
	}
	started := time.Now()
	reranked, err := s.reranker.Rerank(ctx, query, chunks)
	if err != nil {
		observability.Stage(s.log, "reranking", "error", started, err, zap.Int("candidates", len(chunks)))
		s.log.Warn("reranker failed, falling back to un-reranked results", zap.Error(err))
		if len(chunks) > topK {
			return chunks[:topK]
		}
		return chunks
	}
	if len(reranked) > topK {
		reranked = reranked[:topK]
	}
	observability.Stage(s.log, "reranking", "success", started, nil,
		zap.Int("candidates", len(chunks)), zap.Int("results", len(reranked)))
	return reranked
}

func (s *dependencies) multiSearch(ctx context.Context, query string, topK int, filter *Filter) ([]SearchCandidate, error) {
	fragments := splitFragments(query, s.maxFragments)

	vecs, err := s.embedFragments(ctx, fragments)
	if err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}
	if len(vecs) != len(fragments) {
		return nil, fmt.Errorf("embed returned %d vectors for %d fragments", len(vecs), len(fragments))
	}

	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		seen     = make(map[string]SearchCandidate)
		firstErr error
	)
	sem := make(chan struct{}, s.maxConcurrentFragments)
	for i, frag := range fragments {
		wg.Add(1)
		sem <- struct{}{}
		go func(frag string, vec []float32) {
			defer wg.Done()
			defer func() { <-sem }()
			results, err := s.store.HybridSearch(ctx, vec, frag, topK, filter)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("search failed for fragment %q: %w", frag, err)
				}
				return
			}
			for _, c := range results {
				key := c.Key()
				if existing, ok := seen[key]; !ok || c.Score > existing.Score {
					seen[key] = c
				}
			}
		}(frag, vecs[i])
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}

	merged := make([]SearchCandidate, 0, len(seen))
	for _, c := range seen {
		merged = append(merged, c)
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].Score > merged[j].Score })
	merged = capPerFile(merged, s.maxChunksPerFile)
	if len(merged) > topK {
		merged = merged[:topK]
	}
	return merged, nil
}

// embedFragments embeds all query fragments in one batch call when the
// embedder supports it, instead of one round trip per fragment. The query
// task prefix (if configured) is applied to every fragment.
func (s *dependencies) embedFragments(ctx context.Context, fragments []string) ([][]float32, error) {
	started := time.Now()
	if s.queryPrefix != "" {
		for i := range fragments {
			fragments[i] = s.queryPrefix + fragments[i]
		}
	}
	if be, ok := s.embedder.(batchEmbedder); ok {
		vecs, err := be.EmbedBatch(ctx, fragments)
		outcome := "success"
		if err != nil {
			outcome = "error"
		}
		observability.Stage(s.log, "query_embedding", outcome, started, err, zap.Int("fragments", len(fragments)))
		return vecs, err
	}
	vecs := make([][]float32, len(fragments))
	for i, frag := range fragments {
		vec, err := s.embedder.Embed(ctx, frag)
		if err != nil {
			observability.Stage(s.log, "query_embedding", "error", started, err, zap.Int("fragments", len(fragments)))
			return nil, err
		}
		vecs[i] = vec
	}
	observability.Stage(s.log, "query_embedding", "success", started, nil, zap.Int("fragments", len(fragments)))
	return vecs, nil
}

// capPerFile keeps at most maxPerFile chunks per source file, preserving
// the input (score-sorted) order, so one document can't crowd out context
// from other files in the result set.
func capPerFile(chunks []SearchCandidate, maxPerFile int) []SearchCandidate {
	counts := make(map[string]int)
	out := make([]SearchCandidate, 0, len(chunks))
	for _, c := range chunks {
		if counts[c.FilePath] >= maxPerFile {
			continue
		}
		counts[c.FilePath]++
		out = append(out, c)
	}
	return out
}

func toCacheCandidates(candidates []SearchCandidate) []semanticcache.Candidate {
	if len(candidates) == 0 {
		return nil
	}
	out := make([]semanticcache.Candidate, len(candidates))
	for i, c := range candidates {
		out[i] = semanticcache.Candidate{
			Text: c.Text, WindowText: c.WindowText, FilePath: c.FilePath,
			Header: c.Header, LineStart: c.LineStart, ChunkIndex: c.ChunkIndex,
			SourceSHA: c.SourceSHA, Score: c.Score,
		}
	}
	return out
}

func fromCacheCandidates(candidates []semanticcache.Candidate) []SearchCandidate {
	if len(candidates) == 0 {
		return nil
	}
	out := make([]SearchCandidate, len(candidates))
	for i, c := range candidates {
		out[i] = SearchCandidate{
			Text: c.Text, WindowText: c.WindowText, FilePath: c.FilePath,
			Header: c.Header, LineStart: c.LineStart, ChunkIndex: c.ChunkIndex,
			SourceSHA: c.SourceSHA, Score: c.Score,
		}
	}
	return out
}

func splitFragments(query string, maxFragments int) []string {
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
	if maxFragments > 0 && len(out) > maxFragments {
		out = out[:maxFragments]
	}
	return out
}
