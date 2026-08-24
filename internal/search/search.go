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

func (s *dependencies) Search(ctx context.Context, query string, topK int, filter *store.SearchFilter) ([]store.ScoredChunk, error) {
	fetchN := topK
	if s.reranker != nil {
		fetchN = topK * s.candidateMul
	}

	chunks, err := s.multiSearch(ctx, query, fetchN, filter)

	if err != nil {
		return nil, err
	}

	return s.postProcess(ctx, query, chunks, topK)
}

// Query is the top-level entry point for a search request: it dispatches to
// keyword or semantic search and, for semantic queries, transparently
// consults the semantic cache before searching and writes back on miss.
// skipCache bypasses the cache (e.g. the caller wants a fresh generation
// answer rather than a cached one). fromCache reports whether the result
// came from the cache.
func (s *dependencies) Query(ctx context.Context, query, keyword string, topK int, filter *store.SearchFilter, skipCache bool) (chunks []store.ScoredChunk, fromCache bool, err error) {
	if keyword != "" {
		chunks, err = s.KeywordSearch(ctx, keyword, topK, filter)
		return chunks, false, err
	}

	if s.cache != nil && !skipCache && query != "" {
		if cached, hit, cerr := s.cache.Get(ctx, query); cerr == nil && hit {
			if len(cached) > topK {
				cached = cached[:topK]
			}
			return cached, true, nil
		}
	}

	chunks, err = s.Search(ctx, query, topK, filter)
	if err != nil {
		return nil, false, err
	}

	if s.cache != nil && query != "" && len(chunks) > 0 {
		go func() {
			_ = s.cache.Set(context.Background(), query, chunks)
		}()
	}

	return chunks, false, nil
}

func (s *dependencies) KeywordSearch(ctx context.Context, keyword string, topK int, filter *store.SearchFilter) ([]store.ScoredChunk, error) {
	fetchN := topK
	if s.reranker != nil {
		fetchN = topK * s.candidateMul
	}

	chunks, err := s.store.KeywordSearch(ctx, keyword, fetchN, filter)
	if err != nil {
		return nil, err
	}

	return s.postProcess(ctx, keyword, chunks, topK)
}

func (s *dependencies) postProcess(ctx context.Context, query string, chunks []store.ScoredChunk, topK int) ([]store.ScoredChunk, error) {
	if s.reranker != nil && len(chunks) > 0 {
		reranked, err := s.reranker.Rerank(ctx, query, chunks)
		if err != nil {
			s.log.Warn("reranker failed, falling back to un-reranked results", zap.Error(err))
		} else {
			chunks = reranked
			if len(chunks) > topK {
				chunks = chunks[:topK]
			}
		}
	}

	return chunks, nil
}

func (s *dependencies) multiSearch(ctx context.Context, query string, topK int, filter *store.SearchFilter) ([]store.ScoredChunk, error) {
	fragments := splitFragments(query)

	vecs, err := s.embedFragments(ctx, fragments)
	if err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}

	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		seen     = make(map[string]store.ScoredChunk)
		firstErr error
	)
	for i, frag := range fragments {
		wg.Add(1)
		go func(frag string, vec []float32) {
			defer wg.Done()
			results, err := s.store.HybridSearch(ctx, vec, frag, topK, filter)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("search failed")
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
		prefixed := make([]string, len(fragments))
		for i, f := range fragments {
			prefixed[i] = s.queryPrefix + f
		}
		fragments = prefixed
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
