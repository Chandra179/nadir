package pkb

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"nadir/pkg/otel"
)

var sentenceSplit = regexp.MustCompile(`[.?;]+\s*`)

type SearchService struct {
	embedder     Embedder
	store        Store
	hyde         *HyDESearcher
	adaptiveHyde *AdaptiveHyDESearcher
	reranker     Reranker
	candidateMul int
	chunkFilter  ChunkFilter
	metrics      *otel.Metrics
}

func NewSearchService(embedder Embedder, store Store) *SearchService {
	return &SearchService{embedder: embedder, store: store}
}

func (s *SearchService) WithHyDE(h *HyDESearcher) *SearchService {
	s.hyde = h
	return s
}

func (s *SearchService) WithAdaptiveHyDE(a *AdaptiveHyDESearcher) *SearchService {
	s.adaptiveHyde = a
	return s
}

func (s *SearchService) WithReranker(r Reranker, candidateMul int) *SearchService {
	s.reranker = r
	if candidateMul < 1 {
		candidateMul = 3
	}
	s.candidateMul = candidateMul
	return s
}

func (s *SearchService) WithChunkFilter(cf ChunkFilter) *SearchService {
	s.chunkFilter = cf
	return s
}

func (s *SearchService) WithMetrics(m *otel.Metrics) *SearchService {
	s.metrics = m
	return s
}

func (s *SearchService) Search(ctx context.Context, query string, topK int, filter *SearchFilter) ([]ScoredChunk, error) {
	fetchN := topK
	if s.reranker != nil {
		fetchN = topK * s.candidateMul
	}

	var chunks []ScoredChunk
	var err error

	if s.adaptiveHyde != nil {
		hydeStart := time.Now()
		chunks, err = s.adaptiveHyde.Search(ctx, query, fetchN)
		s.metrics.RecordHyDE(ctx, time.Since(hydeStart))
		if err != nil {
			retrieveStart := time.Now()
			chunks, err = s.multiSearch(ctx, query, fetchN, filter)
			s.metrics.RecordRetrieve(ctx, time.Since(retrieveStart))
		}
	} else if s.hyde != nil {
		hydeStart := time.Now()
		chunks, err = s.hyde.Search(ctx, query, fetchN)
		s.metrics.RecordHyDE(ctx, time.Since(hydeStart))
		if err != nil {
			retrieveStart := time.Now()
			chunks, err = s.multiSearch(ctx, query, fetchN, filter)
			s.metrics.RecordRetrieve(ctx, time.Since(retrieveStart))
		}
	} else {
		retrieveStart := time.Now()
		chunks, err = s.multiSearch(ctx, query, fetchN, filter)
		s.metrics.RecordRetrieve(ctx, time.Since(retrieveStart))
	}

	if err != nil {
		return nil, err
	}

	return s.postProcess(ctx, query, chunks, topK)
}

func (s *SearchService) KeywordSearch(ctx context.Context, keyword string, topK int, filter *SearchFilter) ([]ScoredChunk, error) {
	fetchN := topK
	if s.reranker != nil {
		fetchN = topK * s.candidateMul
	}

	retrieveStart := time.Now()
	chunks, err := s.store.KeywordSearch(ctx, keyword, fetchN, filter)
	s.metrics.RecordRetrieve(ctx, time.Since(retrieveStart))
	if err != nil {
		return nil, err
	}

	return s.postProcess(ctx, keyword, chunks, topK)
}

func (s *SearchService) postProcess(ctx context.Context, query string, chunks []ScoredChunk, topK int) ([]ScoredChunk, error) {
	if s.reranker != nil && len(chunks) > 0 {
		rerankStart := time.Now()
		var scoreBefore float32
		if len(chunks) > 0 {
			scoreBefore = chunks[0].Score
		}
		reranked, err := s.reranker.Rerank(ctx, query, chunks)
		if err != nil {
			return nil, fmt.Errorf("rerank failed: %w", err)
		}
		var scoreAfter float32
		if len(reranked) > 0 {
			scoreAfter = reranked[0].Score
		}
		s.metrics.RecordRerank(ctx, time.Since(rerankStart), scoreBefore, scoreAfter)
		chunks = reranked
		if len(chunks) > topK {
			chunks = chunks[:topK]
		}
	}

	if s.chunkFilter != nil && len(chunks) > 0 {
		filterStart := time.Now()
		beforeCount := len(chunks)
		filtered, err := s.chunkFilter.Filter(ctx, query, chunks)
		if err == nil && len(filtered) > 0 {
			s.metrics.RecordFilter(ctx, time.Since(filterStart), beforeCount-len(filtered))
			chunks = filtered
		} else {
			s.metrics.RecordFilter(ctx, time.Since(filterStart), 0)
		}
	}

	return chunks, nil
}

func (s *SearchService) multiSearch(ctx context.Context, query string, topK int, filter *SearchFilter) ([]ScoredChunk, error) {
	fragments := splitFragments(query)
	seen := make(map[string]ScoredChunk)
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
