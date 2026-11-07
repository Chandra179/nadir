package documents

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"nadir/internal/core/retrieval/search"
	"nadir/internal/providers/cloudflare/d1"
	"nadir/internal/providers/cloudflare/vectorize"
	"nadir/internal/providers/lexical"
)

// HybridSearch runs the dense leg (Vectorize, resolved through D1) and the
// keyword leg (D1 FTS5) concurrently and fuses them with reciprocal-rank
// fusion. Component rankings are returned only when adaptive reranking asks for
// them, as the Qdrant store does.
//
// The dense leg is capped by Vectorize's topK limit, so for large requests it
// holds fewer candidates than the Qdrant store's topK*prefetch_mul; the keyword
// leg is not capped and fills the fused pool.
func (s *dependencies) HybridSearch(ctx context.Context, vector []float32, query string, topK int, filter *search.Filter) (search.HybridSearchResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if err := s.notReady(); err != nil {
		return search.HybridSearchResult{}, err
	}
	fetchN := topK * s.prefetchMul
	terms := uniqueTerms(lexical.QueryTerms(query))

	var dense, lex []search.SearchCandidate
	var denseErr, lexErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		dense, denseErr = s.denseLeg(ctx, vector, min(fetchN, maxQueryTopK), filter)
	}()
	if len(terms) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lex, lexErr = s.lexicalLeg(ctx, terms, fetchN, filter)
		}()
	}
	wg.Wait()
	if denseErr != nil {
		return search.HybridSearchResult{}, fmt.Errorf("hybrid search: dense leg: %w", denseErr)
	}
	if lexErr != nil {
		return search.HybridSearchResult{}, fmt.Errorf("hybrid search: keyword leg: %w", lexErr)
	}

	result := search.HybridSearchResult{Fused: reciprocalRankFusion(topK, dense, lex)}
	if s.adaptiveSignals {
		result.Dense = dense
		result.Lexical = lex
	}
	return result, nil
}

// KeywordSearch ranks a keyword-only query with BM25, keeping every term.
func (s *dependencies) KeywordSearch(ctx context.Context, keyword string, topK int, filter *search.Filter) ([]search.SearchCandidate, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if err := s.notReady(); err != nil {
		return nil, err
	}
	terms := uniqueTerms(lexical.Tokens(keyword))
	if len(terms) == 0 {
		return nil, nil
	}
	results, err := s.lexicalLeg(ctx, terms, topK, filter)
	if err != nil {
		return nil, fmt.Errorf("keyword search: %w", err)
	}
	return results, nil
}

func (s *dependencies) denseLeg(ctx context.Context, vector []float32, n int, filter *search.Filter) ([]search.SearchCandidate, error) {
	matches, err := s.index.QueryWith(ctx, vector, vectorize.QueryOptions{TopK: n, Filter: vectorFilter(filter)})
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, nil
	}
	var statements []d1.Statement
	for start := 0; start < len(matches); start += idsPerStatement {
		batch := matches[start:min(start+idsPerStatement, len(matches))]
		params := make([]any, len(batch))
		for i, m := range batch {
			params[i] = m.ID
		}
		statements = append(statements, d1.Statement{
			SQL: `SELECT ` + chunkColumns + ` FROM doc_chunks c ` + activeJoin + `
				WHERE v.active = 1 AND c.generation = ` + currentGeneration + ` AND c.id IN (` + strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",") + `)`,
			Params: params,
		})
	}
	sets, err := s.db.Exec(ctx, statements...)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]d1.Row, len(matches))
	for _, set := range sets {
		for _, row := range set {
			byID[row.Str("id")] = row
		}
	}
	// Vectorize order is the ranking. Ids with no active row (a retired version
	// or a vector of another generation) drop out, and the filter is re-checked
	// exactly because the metadata keys are hashes.
	out := make([]search.SearchCandidate, 0, len(matches))
	for _, m := range matches {
		row, ok := byID[m.ID]
		if !ok {
			continue
		}
		candidate := candidateFromRow(row)
		if !matchesFilter(candidate, filter) {
			continue
		}
		candidate.Score = m.Score
		out = append(out, candidate)
	}
	return out, nil
}

func (s *dependencies) lexicalLeg(ctx context.Context, terms []string, n int, filter *search.Filter) ([]search.SearchCandidate, error) {
	quoted := make([]string, len(terms))
	for i, t := range terms {
		// Terms are letters and digits only, so quoting cannot be broken out of.
		quoted[i] = `"` + t + `"`
	}
	sql := `SELECT ` + chunkColumns + `, -bm25(doc_fts) AS score
		FROM doc_fts JOIN doc_chunks c ON c.seq = doc_fts.rowid ` + activeJoin + `
		WHERE doc_fts MATCH ? AND v.active = 1 AND c.generation = ` + currentGeneration
	params := []any{strings.Join(quoted, " OR ")}
	if filter != nil {
		if filter.FilePath != "" {
			sql += ` AND c.file_path = ?`
			params = append(params, filter.FilePath)
		}
		if filter.Header != "" {
			sql += ` AND c.header = ?`
			params = append(params, filter.Header)
		}
		if filter.SourceSHA != "" {
			sql += ` AND c.source_sha = ?`
			params = append(params, filter.SourceSHA)
		}
	}
	sql += ` ORDER BY bm25(doc_fts) LIMIT ?`
	params = append(params, n)

	sets, err := s.db.Exec(ctx, d1.Statement{SQL: sql, Params: params})
	if err != nil {
		return nil, err
	}
	out := make([]search.SearchCandidate, len(sets[0]))
	for i, row := range sets[0] {
		out[i] = candidateFromRow(row)
		out[i].Score = float32(row.Float("score"))
	}
	return out, nil
}

// reciprocalRankFusion merges ranked lists by 1/(k+rank) summed over the lists
// a chunk appears in. Ties keep the order of first appearance, so the result is
// deterministic. Chunks are identified by version and position, which is what
// both legs share.
func reciprocalRankFusion(topK int, lists ...[]search.SearchCandidate) []search.SearchCandidate {
	type entry struct {
		candidate search.SearchCandidate
		score     float64
		first     int
	}
	entries := map[string]*entry{}
	next := 0
	for _, list := range lists {
		for rank, c := range list {
			key := fmt.Sprintf("%s\x00%s\x00%d\x00%d", c.FilePath, c.SourceSHA, c.LineStart, c.ChunkIndex)
			e, ok := entries[key]
			if !ok {
				e = &entry{candidate: c, first: next}
				entries[key] = e
				next++
			}
			e.score += 1 / float64(rrfK+rank+1)
		}
	}
	ordered := make([]*entry, 0, len(entries))
	for _, e := range entries {
		ordered = append(ordered, e)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].score != ordered[j].score {
			return ordered[i].score > ordered[j].score
		}
		return ordered[i].first < ordered[j].first
	})
	if len(ordered) > topK {
		ordered = ordered[:topK]
	}
	out := make([]search.SearchCandidate, len(ordered))
	for i, e := range ordered {
		out[i] = e.candidate
		out[i].Score = float32(e.score)
	}
	return out
}

func vectorFilter(f *search.Filter) map[string]string {
	if f == nil {
		return nil
	}
	filter := map[string]string{}
	if f.FilePath != "" {
		filter[metaFile] = metaKey(f.FilePath)
	}
	if f.Header != "" {
		filter[metaHeader] = metaKey(f.Header)
	}
	if f.SourceSHA != "" {
		filter[metaSHA] = metaKey(f.SourceSHA)
	}
	return filter
}

func matchesFilter(c search.SearchCandidate, f *search.Filter) bool {
	if f == nil {
		return true
	}
	return (f.FilePath == "" || c.FilePath == f.FilePath) &&
		(f.Header == "" || c.Header == f.Header) &&
		(f.SourceSHA == "" || c.SourceSHA == f.SourceSHA)
}

func candidateFromRow(r d1.Row) search.SearchCandidate {
	return search.SearchCandidate{
		Text:        r.Str("text"),
		WindowText:  r.Str("window_text"),
		FilePath:    r.Str("file_path"),
		Header:      r.Str("header"),
		SectionPath: r.Str("section_path"),
		LineStart:   int(r.Int("line_start")),
		ChunkIndex:  int(r.Int("chunk_index")),
		SourceSHA:   r.Str("source_sha"),
	}
}

func uniqueTerms(terms []string) []string {
	seen := make(map[string]bool, len(terms))
	out := make([]string, 0, len(terms))
	for _, t := range terms {
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}
