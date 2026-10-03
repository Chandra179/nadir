package search

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"nadir/internal/core/observability"
	semanticcache "nadir/internal/core/retrieval/cache"

	"log/slog"
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
	finish := func(outcome string, err error, fields ...slog.Attr) {
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
		if utf8.RuneCountInString(strings.TrimSpace(keyword)) > s.maxQueryChars {
			operationErr = ErrQueryTooLong
			finish("error", ErrQueryTooLong)
			return Result{}, ErrQueryTooLong
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
			slog.Bool("keyword", true), slog.Int("results", len(chunks)))
		return Result{Chunks: fromStoreChunks(chunks), Rerank: telemetry, OperationID: operation.ID()}, err
	}

	// A typed fusion request can use a different ranking policy from the
	// automatically classified query, so it cannot share query-only entries.
	cacheEligible := s.cache != nil && !request.SkipCache && isEmptyFilter(filter) && query != "" &&
		(!s.fusion.Enabled || request.QueryType == QueryTypeUnknown)
	var cacheSet func(context.Context, string, []semanticcache.Candidate) error
	if cacheEligible {
		cacheSet = s.cache.PrepareWrite()
	}
	if cached, ok := s.getCached(ctx, query, topK, filter, !cacheEligible); ok {
		finish("cache_hit", nil, slog.Bool("from_cache", true), slog.Int("results", len(cached)))
		return Result{Chunks: fromStoreChunks(cached), FromCache: true, OperationID: operation.ID()}, nil
	}

	chunks, telemetry, err := s.search(ctx, query, request.QueryType, topK, filter)
	if err != nil {
		operationErr = err
		finish("error", err)
		return Result{}, err
	}

	if cacheSet != nil && len(chunks) > 0 {
		write := func(workCtx context.Context) {
			cacheCtx, cacheOperation := observability.Start(workCtx, s.telemetry, s.log, "cache_write")
			cacheStarted := time.Now()
			err := cacheSet(cacheCtx, query, toCacheCandidates(chunks))
			outcome := "success"
			if err != nil {
				outcome = "error"
			}
			cacheOperation.End(outcome, err, slog.Int("results", len(chunks)))
			observability.StageContext(cacheCtx, s.log, "cache_write", outcome, cacheStarted, err,
				slog.Int("results", len(chunks)))
		}
		if s.cacheWrite != nil {
			s.cacheWrite(ctx, write)
		} else {
			write(ctx)
		}
	}

	finish("success", nil, slog.Bool("from_cache", false), slog.Int("results", len(chunks)))
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
	if !hit || len(cached) < topK {
		observability.StageContext(ctx, s.log, "cache_read", "miss", started, nil)
		return nil, false
	}
	if len(cached) > topK {
		cached = cached[:topK]
	}
	observability.StageContext(ctx, s.log, "cache_read", "hit", started, nil, slog.Int("results", len(cached)))
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
				slog.Bool("adaptive", true), slog.String("reason", reason), slog.Int("candidates", len(chunks)))
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
		observability.StageContext(ctx, s.log, "reranking", "error", started, err, slog.Int("candidates", len(chunks)))
		s.log.Warn("reranker failed, falling back to un-reranked results", slog.String("error_label", observability.ErrorLabel(err)))
		return trimCandidates(chunks, topK), telemetry
	}
	reranked = trimCandidates(reranked, topK)
	observability.StageContext(ctx, s.log, "reranking", "success", started, nil,
		slog.Int("candidates", len(chunks)), slog.Int("results", len(reranked)))
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
	inputs := fragments
	if s.queryPrefix != "" {
		// Embedding task instructions belong only to the dense input. Preserve
		// the original fragments used by lexical search and fusion scoring.
		inputs = make([]string, len(fragments))
		for i, fragment := range fragments {
			inputs[i] = s.queryPrefix + fragment
		}
	}
	if be, ok := s.embedder.(batchEmbedder); ok {
		vecs, err := be.EmbedBatch(ctx, inputs)
		outcome := "success"
		if err != nil {
			outcome = "error"
		}
		observability.StageContext(ctx, s.log, "query_embedding", outcome, started, err, slog.Int("fragments", len(fragments)))
		return vecs, err
	}
	vecs := make([][]float32, len(inputs))
	for i, frag := range inputs {
		vec, err := s.embedder.Embed(ctx, frag)
		if err != nil {
			observability.StageContext(ctx, s.log, "query_embedding", "error", started, err, slog.Int("fragments", len(fragments)))
			return nil, err
		}
		vecs[i] = vec
	}
	observability.StageContext(ctx, s.log, "query_embedding", "success", started, nil, slog.Int("fragments", len(fragments)))
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
			Header: c.Header, SectionPath: c.SectionPath, LineStart: c.LineStart, ChunkIndex: c.ChunkIndex,
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
			Header: c.Header, SectionPath: c.SectionPath, LineStart: c.LineStart, ChunkIndex: c.ChunkIndex,
			SourceSHA: c.SourceSHA, Score: c.Score,
		}
	}
	return out
}

// splitFragments breaks a multi-sentence query into per-sentence fragments so
// each part is searchable on its own, and keeps the full query as the first
// fragment: per-sentence searching alone can lose cross-sentence intent.
// maxFragments trimming never drops the original query.
func splitFragments(query string, maxFragments int) []string {
	trimmed := strings.TrimSpace(query)
	parts := sentenceSplit.Split(trimmed, -1)
	out := make([]string, 0, len(parts)+1)
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return []string{query}
	}
	if len(out) > 1 && out[0] != trimmed {
		out = append([]string{trimmed}, out...)
	}
	if maxFragments > 0 && len(out) > maxFragments {
		out = out[:maxFragments]
	}
	return out
}
