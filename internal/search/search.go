package search

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"nadir/internal/store"

	"go.uber.org/zap"
)

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
	seen := make(map[string]store.ScoredChunk)
	for _, frag := range fragments {
		vec, err := s.embedder.Embed(ctx, frag)
		if err != nil {
			return nil, fmt.Errorf("embed: %w", err)
		}
		results, err := s.store.HybridSearch(ctx, vec, frag, topK, filter)
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
	merged := make([]store.ScoredChunk, 0, len(seen))
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
