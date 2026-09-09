package search

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"nadir/internal/embedder"
	"nadir/internal/store"

	"go.uber.org/zap"
)

// maxChunksPerFile caps how many chunks from the same source file can
// appear in a result set, so one large or heavily-overlapping document
// can't crowd out relevant context from other files.
const maxChunksPerFile = 3

var sentenceSplit = regexp.MustCompile(`[.?;]+\s*`)

func (s *dependencies) search(ctx context.Context, query string, topK int, filter *store.SearchFilter) ([]store.ScoredChunk, error) {
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

// Query is the top-level search entry point: dispatches to keyword or
// semantic search, consulting the semantic cache first when wired
// (skipCache bypasses it). fromCache reports a cache hit.
func (s *dependencies) Query(ctx context.Context, query, keyword string, topK int, filter *store.SearchFilter, skipCache bool) (chunks []store.ScoredChunk, fromCache bool, err error) {
	if keyword == "" {
		if err := s.validateQuery(query, topK); err != nil {
			return nil, false, err
		}
	} else {
		if len([]rune(strings.TrimSpace(keyword))) > s.maxQueryChars {
			return nil, false, errQueryTooLong
		}
		if topK <= 0 {
			return nil, false, fmt.Errorf("search top_k must be greater than zero")
		}
	}
	if topK > s.maxTopK {
		topK = s.maxTopK
	}
	if keyword != "" {
		chunks, err = s.keywordSearch(ctx, keyword, topK, filter)
		return chunks, false, err
	}

	if cached, ok := s.getCached(ctx, query, topK, filter, skipCache); ok {
		return cached, true, nil
	}

	chunks, err = s.search(ctx, query, topK, filter)
	if err != nil {
		return nil, false, err
	}

	if s.cache != nil && isEmptyFilter(filter) && query != "" && len(chunks) > 0 {
		go func() {
			_ = s.cache.Set(context.Background(), query, chunks)
		}()
	}

	return chunks, false, nil
}

// getCached consults the semantic cache unless the caller asked to skip it.
// Returns false on miss or cache error so lookups stay best-effort; hits
// are truncated to topK to match a fresh search's result size.
func (s *dependencies) getCached(ctx context.Context, query string, topK int, filter *store.SearchFilter, skip bool) ([]store.ScoredChunk, bool) {
	if s.cache == nil || skip || query == "" || !isEmptyFilter(filter) {
		return nil, false
	}
	cached, hit, err := s.cache.Get(ctx, query)
	if err != nil || !hit {
		return nil, false
	}
	if len(cached) > topK {
		cached = cached[:topK]
	}
	return cached, true
}

func isEmptyFilter(filter *store.SearchFilter) bool {
	return filter == nil || (filter.FilePath == "" && filter.Header == "" && filter.SourceSHA == "")
}

func (s *dependencies) keywordSearch(ctx context.Context, keyword string, topK int, filter *store.SearchFilter) ([]store.ScoredChunk, error) {
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
func (s *dependencies) rerankTopK(ctx context.Context, query string, chunks []store.ScoredChunk, topK int) []store.ScoredChunk {
	if s.reranker == nil || len(chunks) == 0 {
		return chunks
	}
	reranked, err := s.reranker.Rerank(ctx, query, chunks)
	if err != nil {
		s.log.Warn("reranker failed, falling back to un-reranked results", zap.Error(err))
		if len(chunks) > topK {
			return chunks[:topK]
		}
		return chunks
	}
	if len(reranked) > topK {
		reranked = reranked[:topK]
	}
	return reranked
}

func (s *dependencies) multiSearch(ctx context.Context, query string, topK int, filter *store.SearchFilter) ([]store.ScoredChunk, error) {
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
		seen     = make(map[string]store.ScoredChunk)
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

	merged := make([]store.ScoredChunk, 0, len(seen))
	for _, c := range seen {
		merged = append(merged, c)
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].Score > merged[j].Score })
	merged = capPerFile(merged, maxChunksPerFile)
	if len(merged) > topK {
		merged = merged[:topK]
	}
	return merged, nil
}

// embedFragments embeds all query fragments in one batch call when the
// embedder supports it, instead of one round trip per fragment. The query
// task prefix (if configured) is applied to every fragment.
func (s *dependencies) embedFragments(ctx context.Context, fragments []string) ([][]float32, error) {
	if s.queryPrefix != "" {
		for i := range fragments {
			fragments[i] = s.queryPrefix + fragments[i]
		}
	}
	if be, ok := s.embedder.(embedder.BatchEmbedder); ok {
		return be.EmbedBatch(ctx, fragments)
	}
	vecs := make([][]float32, len(fragments))
	for i, frag := range fragments {
		vec, err := s.embedder.Embed(ctx, frag)
		if err != nil {
			return nil, err
		}
		vecs[i] = vec
	}
	return vecs, nil
}

// capPerFile keeps at most maxPerFile chunks per source file, preserving
// the input (score-sorted) order, so one document can't crowd out context
// from other files in the result set.
func capPerFile(chunks []store.ScoredChunk, maxPerFile int) []store.ScoredChunk {
	counts := make(map[string]int)
	out := make([]store.ScoredChunk, 0, len(chunks))
	for _, c := range chunks {
		if counts[c.FilePath] >= maxPerFile {
			continue
		}
		counts[c.FilePath]++
		out = append(out, c)
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
