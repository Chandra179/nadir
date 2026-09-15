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

func (s *dependencies) search(ctx context.Context, query string, queryType QueryType, topK int, filter *Filter) ([]SearchCandidate, RerankTelemetry, error) {
	fetchN := topK
	if s.reranker != nil {
		fetchN = topK * s.candidateMul
	}

	result, err := s.multiSearch(ctx, query, queryType, fetchN, filter)

	if err != nil {
		return nil, RerankTelemetry{}, err
	}

	chunks, telemetry := s.rerankTopK(ctx, query, result.Fused, topK, result)
	return chunks, telemetry, nil
}

// Query is the top-level Retrieval entry point. It normalizes the request,
// dispatches to keyword or semantic search, consults the semantic cache, and
// returns storage-independent chunks to the caller.
func (s *dependencies) Query(ctx context.Context, request Request) (Result, error) {
	ctx, operation := observability.Start(ctx, s.telemetry, s.log, "retrieval")
	var operationErr error
	defer func() {
		outcome := "success"
		if operationErr != nil {
			outcome = "error"
		}
		operation.End(outcome, operationErr)
	}()
	started := time.Now()
	finish := func(outcome string, err error, fields ...zap.Field) {
		observability.StageContext(ctx, s.log, "retrieval", outcome, started, err, fields...)
	}
	query, keyword, topK := request.Query, request.Keyword, request.TopK
	filter := request.Filter
	if keyword == "" {
		if err := s.validateQuery(query, topK); err != nil {
			operationErr = err
			finish("error", err)
			return Result{}, err
		}
	} else {
		if len([]rune(strings.TrimSpace(keyword))) > s.maxQueryChars {
			operationErr = errQueryTooLong
			finish("error", errQueryTooLong)
			return Result{}, errQueryTooLong
		}
		if topK <= 0 {
			err := fmt.Errorf("search top_k must be greater than zero")
			operationErr = err
			finish("error", err)
			return Result{}, err
		}
	}
	if topK > s.maxTopK {
		topK = s.maxTopK
	}
	if keyword != "" {
		chunks, telemetry, err := s.keywordSearch(ctx, keyword, topK, filter)
		outcome := "success"
		if err != nil {
			outcome = "error"
			operationErr = err
		}
		finish(outcome, err,
			zap.Bool("keyword", true), zap.Int("results", len(chunks)))
		return Result{Chunks: fromStoreChunks(chunks), Rerank: telemetry, OperationID: operation.ID()}, err
	}

	if cached, ok := s.getCached(ctx, query, topK, filter, request.SkipCache); ok {
		finish("cache_hit", nil, zap.Bool("from_cache", true), zap.Int("results", len(cached)))
		return Result{Chunks: fromStoreChunks(cached), FromCache: true, OperationID: operation.ID()}, nil
	}

	chunks, telemetry, err := s.search(ctx, query, request.QueryType, topK, filter)
	if err != nil {
		operationErr = err
		finish("error", err)
		return Result{}, err
	}

	if s.cache != nil && isEmptyFilter(filter) && query != "" && len(chunks) > 0 {
		go func(parent context.Context) {
			cacheCtx, cacheOperation := observability.Start(context.WithoutCancel(parent), s.telemetry, s.log, "cache_write")
			cacheStarted := time.Now()
			err := s.cache.Set(cacheCtx, query, toCacheCandidates(chunks))
			outcome := "success"
			if err != nil {
				outcome = "error"
			}
			cacheOperation.End(outcome, err, zap.Int("results", len(chunks)))
			observability.StageContext(cacheCtx, s.log, "cache_write", outcome, cacheStarted, err,
				zap.Int("results", len(chunks)))
		}(ctx)
	}

	finish("success", nil, zap.Bool("from_cache", false), zap.Int("results", len(chunks)))
	return Result{Chunks: fromStoreChunks(chunks), Rerank: telemetry, OperationID: operation.ID()}, nil
}

func fromStoreChunks(chunks []SearchCandidate) []Chunk {
	if len(chunks) == 0 {
		return nil
	}
	out := make([]Chunk, len(chunks))
	for i, chunk := range chunks {
		out[i] = Chunk(chunk)
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
		observability.StageContext(ctx, s.log, "cache_read", "error", started, err)
		return nil, false
	}
	if !hit {
		observability.StageContext(ctx, s.log, "cache_read", "miss", started, nil)
		return nil, false
	}
	if len(cached) > topK {
		cached = cached[:topK]
	}
	observability.StageContext(ctx, s.log, "cache_read", "hit", started, nil, zap.Int("results", len(cached)))
	return fromCacheCandidates(cached), true
}

func isEmptyFilter(filter *Filter) bool {
	return filter == nil || (filter.FilePath == "" && filter.Header == "" && filter.SourceSHA == "")
}

func (s *dependencies) keywordSearch(ctx context.Context, keyword string, topK int, filter *Filter) ([]SearchCandidate, RerankTelemetry, error) {
	fetchN := topK
	if s.reranker != nil {
		fetchN = topK * s.candidateMul
	}

	if s.fragmentAdmission != nil {
		release, err := s.fragmentAdmission(ctx)
		if err != nil {
			return nil, RerankTelemetry{}, fmt.Errorf("retrieval admission: %w", err)
		}
		defer release()
	}
	chunks, err := s.store.KeywordSearch(ctx, keyword, fetchN, filter)
	if err != nil {
		return nil, RerankTelemetry{}, err
	}

	reranked, telemetry := s.rerankTopK(ctx, keyword, chunks, topK, HybridSearchResult{Fused: chunks})
	return reranked, telemetry, nil
}

// rerankTopK re-scores candidates with the cross-encoder when configured,
// keeping the best topK. Best-effort: on reranker failure the original
// score ordering is retained, but the result count remains bounded.
func (s *dependencies) rerankTopK(ctx context.Context, query string, chunks []SearchCandidate, topK int, signals HybridSearchResult) ([]SearchCandidate, RerankTelemetry) {
	telemetry := RerankTelemetry{Enabled: s.reranker != nil}
	if s.reranker == nil || len(chunks) == 0 {
		telemetry.Reason = "disabled"
		return chunks, telemetry
	}

	started := time.Now()
	if s.adaptiveRerank {
		shouldRerank, reason := adaptiveRerankDecision(chunks, signals, s.adaptiveMarginThreshold)
		telemetry.Reason = reason
		if !shouldRerank {
			observability.StageContext(ctx, s.log, "reranking", "skipped", started, nil,
				zap.Bool("adaptive", true), zap.String("reason", reason), zap.Int("candidates", len(chunks)))
			return trimCandidates(chunks, topK), telemetry
		}
	} else {
		telemetry.Reason = "always"
	}

	telemetry.Attempted = true
	telemetry.Candidates = len(chunks)
	reranked, err := s.reranker.Rerank(ctx, query, chunks)
	telemetry.LatencyMS = float64(time.Since(started).Microseconds()) / 1000
	if err != nil {
		telemetry.DependencyErr = true
		observability.StageContext(ctx, s.log, "reranking", "error", started, err, zap.Int("candidates", len(chunks)))
		s.log.Warn("reranker failed, falling back to un-reranked results", zap.Error(err))
		return trimCandidates(chunks, topK), telemetry
	}
	reranked = trimCandidates(reranked, topK)
	observability.StageContext(ctx, s.log, "reranking", "success", started, nil,
		zap.Int("candidates", len(chunks)), zap.Int("results", len(reranked)))
	return reranked, telemetry
}

func trimCandidates(chunks []SearchCandidate, topK int) []SearchCandidate {
	if len(chunks) > topK {
		return chunks[:topK]
	}
	return chunks
}

func adaptiveRerankDecision(fused []SearchCandidate, signals HybridSearchResult, marginThreshold float32) (bool, string) {
	if len(fused) < 2 {
		return false, "single_candidate"
	}
	if len(signals.Dense) == 0 || len(signals.Lexical) == 0 {
		return true, "missing_leg"
	}
	if signals.Dense[0].Key() != signals.Lexical[0].Key() {
		return true, "leg_disagreement"
	}
	if relativeMargin(fused[0].Score, fused[1].Score) < marginThreshold {
		return true, "weak_margin"
	}
	return false, "high_confidence"
}

func relativeMargin(first, second float32) float32 {
	denominator := first
	if denominator < 0 {
		denominator = -denominator
	}
	if denominator < 1e-6 {
		return 0
	}
	delta := first - second
	if delta < 0 {
		delta = -delta
	}
	return delta / denominator
}

func (s *dependencies) multiSearch(ctx context.Context, query string, queryType QueryType, topK int, filter *Filter) (HybridSearchResult, error) {
	fragments := splitFragments(query, s.maxFragments)
	queryType = resolveQueryType(query, queryType)

	vecs, err := s.embedFragments(ctx, fragments)
	if err != nil {
		return HybridSearchResult{}, fmt.Errorf("embed: %w", err)
	}
	if len(vecs) != len(fragments) {
		return HybridSearchResult{}, fmt.Errorf("embed returned %d vectors for %d fragments", len(vecs), len(fragments))
	}

	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		fused    = make(map[string]SearchCandidate)
		dense    = make(map[string]SearchCandidate)
		lexical  = make(map[string]SearchCandidate)
		firstErr error
	)
	sem := make(chan struct{}, s.maxConcurrentFragments)
	for i, frag := range fragments {
		wg.Add(1)
		sem <- struct{}{}
		go func(frag string, vec []float32) {
			defer wg.Done()
			defer func() { <-sem }()
			if s.fragmentAdmission != nil {
				release, err := s.fragmentAdmission(ctx)
				if err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = fmt.Errorf("retrieval admission for fragment %q: %w", frag, err)
					}
					mu.Unlock()
					return
				}
				defer release()
			}
			results, err := s.store.HybridSearch(ctx, vec, frag, topK, filter)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("search failed for fragment %q: %w", frag, err)
				}
				return
			}
			fusedCandidates := results.Fused
			if s.fusion.Enabled {
				fusedCandidates = fuseHybrid(frag, queryType, results, s.fusion)
			}
			mergeBest(fused, fusedCandidates)
			mergeBest(dense, results.Dense)
			mergeBest(lexical, results.Lexical)
		}(frag, vecs[i])
	}
	wg.Wait()
	if firstErr != nil {
		return HybridSearchResult{}, firstErr
	}

	merged := make([]SearchCandidate, 0, len(fused))
	for _, c := range fused {
		merged = append(merged, c)
	}
	sort.Slice(merged, func(i, j int) bool {
		if merged[i].Score != merged[j].Score {
			return merged[i].Score > merged[j].Score
		}
		return merged[i].Key() < merged[j].Key()
	})
	merged = capPerFile(merged, s.maxChunksPerFile)
	if len(merged) > topK {
		merged = merged[:topK]
	}
	return HybridSearchResult{
		Fused:   merged,
		Dense:   sortCandidates(dense),
		Lexical: sortCandidates(lexical),
	}, nil
}

func mergeBest(dst map[string]SearchCandidate, candidates []SearchCandidate) {
	for _, candidate := range candidates {
		key := candidate.Key()
		if existing, ok := dst[key]; !ok || candidate.Score > existing.Score {
			dst[key] = candidate
		}
	}
}

func sortCandidates(candidates map[string]SearchCandidate) []SearchCandidate {
	result := make([]SearchCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		result = append(result, candidate)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Score != result[j].Score {
			return result[i].Score > result[j].Score
		}
		return result[i].Key() < result[j].Key()
	})
	return result
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
		observability.StageContext(ctx, s.log, "query_embedding", outcome, started, err, zap.Int("fragments", len(fragments)))
		return vecs, err
	}
	vecs := make([][]float32, len(fragments))
	for i, frag := range fragments {
		vec, err := s.embedder.Embed(ctx, frag)
		if err != nil {
			observability.StageContext(ctx, s.log, "query_embedding", "error", started, err, zap.Int("fragments", len(fragments)))
			return nil, err
		}
		vecs[i] = vec
	}
	observability.StageContext(ctx, s.log, "query_embedding", "success", started, nil, zap.Int("fragments", len(fragments)))
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
