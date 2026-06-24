package search

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"nadir/internal/embedder"
	"nadir/internal/reranker"
	"nadir/internal/store"
)

var sentenceSplit = regexp.MustCompile(`[.?;]+\s*`)

type Service struct {
	embedder     embedder.Embedder
	store        store.Store
	reranker     reranker.Reranker
	candidateMul int
}

func NewService(embedder embedder.Embedder, s store.Store) *Service {
	return &Service{embedder: embedder, store: s}
}

func (s *Service) WithReranker(r reranker.Reranker, candidateMul int) *Service {
	s.reranker = r
	if candidateMul < 1 {
		candidateMul = 3
	}
	s.candidateMul = candidateMul
	return s
}

func (s *Service) Search(ctx context.Context, query string, topK int, filter *store.SearchFilter) ([]store.ScoredChunk, error) {
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

func (s *Service) KeywordSearch(ctx context.Context, keyword string, topK int, filter *store.SearchFilter) ([]store.ScoredChunk, error) {
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

func (s *Service) postProcess(ctx context.Context, query string, chunks []store.ScoredChunk, topK int) ([]store.ScoredChunk, error) {
	if s.reranker != nil && len(chunks) > 0 {
		reranked, err := s.reranker.Rerank(ctx, query, chunks)
		if err != nil {
			return nil, fmt.Errorf("rerank failed: %w", err)
		}
		chunks = reranked
		if len(chunks) > topK {
			chunks = chunks[:topK]
		}
	}

	return chunks, nil
}

func (s *Service) multiSearch(ctx context.Context, query string, topK int, filter *store.SearchFilter) ([]store.ScoredChunk, error) {
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
