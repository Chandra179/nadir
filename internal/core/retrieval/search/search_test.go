package search

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"nadir/internal/core/retrieval/cache"
)

func TestRerankerFailureKeepsPrivateDetailsOutOfDiagnostics(t *testing.T) {
	const private = "private-reranker-canary"
	var logs bytes.Buffer
	d := NewDependencies(DependenciesConfig{
		Embedder: &searchTestEmbedder{},
		Store:    &searchTestStore{results: []SearchCandidate{{Text: "Evidence", FilePath: "note.md"}}},
		Reranker: searchTestReranker{err: errors.New("sidecar: " + private)},
		Log:      slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	result, err := d.Query(context.Background(), Request{Query: private, TopK: 1})
	if err != nil || len(result.Chunks) != 1 || !result.Rerank.DependencyErr {
		t.Fatalf("reranker fallback changed: %+v, %v", result, err)
	}
	if strings.Contains(logs.String(), private) || !strings.Contains(logs.String(), "dependency_error") {
		t.Fatal("diagnostics leaked content or lost the safe failure classification")
	}
}

type searchTestEmbedder struct {
	mu          sync.Mutex
	batchInputs [][]string
}

func (e *searchTestEmbedder) Embed(context.Context, string) ([]float32, error) {
	return []float32{1}, nil
}

func (e *searchTestEmbedder) Dimensions() int { return 1 }

func (e *searchTestEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	e.mu.Lock()
	e.batchInputs = append(e.batchInputs, append([]string(nil), texts...))
	e.mu.Unlock()

	vecs := make([][]float32, len(texts))
	for i := range texts {
		vecs[i] = []float32{float32(i + 1)}
	}
	return vecs, nil
}

type searchTestStore struct {
	mu            sync.Mutex
	hybridCalls   int
	hybridQueries []string
	hybridLimits  []int
	lastFilter    *Filter
	results       []SearchCandidate
	signals       HybridSearchResult
}

func (s *searchTestStore) KeywordSearch(context.Context, string, int, *Filter) ([]SearchCandidate, error) {
	return nil, nil
}

func (s *searchTestStore) HybridSearch(_ context.Context, _ []float32, query string, limit int, filter *Filter) (HybridSearchResult, error) {
	s.mu.Lock()
	s.hybridCalls++
	s.hybridQueries = append(s.hybridQueries, query)
	s.hybridLimits = append(s.hybridLimits, limit)
	if filter != nil {
		copyFilter := *filter
		s.lastFilter = &copyFilter
	}
	results := s.signals
	if results.Fused == nil {
		results.Fused = append([]SearchCandidate(nil), s.results...)
	}
	// Like a real store, never return more than the requested limit.
	if limit > 0 && len(results.Fused) > limit {
		results.Fused = results.Fused[:limit]
	}
	s.mu.Unlock()
	return results, nil
}

var _ documentSearcher = (*searchTestStore)(nil)

type searchTestCache struct {
	chunks []cache.Candidate
	// requestedTopK is the request size the stored entry reports; zero models
	// an entry that only vouches for what it holds.
	requestedTopK int
	hit           bool
	getCalls      int
	getVectors    [][]float32
	prepareCalls  int
	writes        []searchTestCacheWrite
}

type searchTestCacheWrite struct {
	query         string
	vector        []float32
	results       []cache.Candidate
	requestedTopK int
}

func (c *searchTestCache) Get(_ context.Context, vector []float32) (cache.Lookup, bool, error) {
	c.getCalls++
	c.getVectors = append(c.getVectors, append([]float32(nil), vector...))
	return cache.Lookup{Results: append([]cache.Candidate(nil), c.chunks...), RequestedTopK: c.requestedTopK}, c.hit, nil
}
func (c *searchTestCache) PrepareWrite() func(context.Context, string, []float32, []cache.Candidate, int) error {
	c.prepareCalls++
	return func(_ context.Context, query string, vector []float32, results []cache.Candidate, requestedTopK int) error {
		c.writes = append(c.writes, searchTestCacheWrite{query, append([]float32(nil), vector...), results, requestedTopK})
		return nil
	}
}
func (c *searchTestCache) Clear(context.Context) error { return nil }

var _ cache.SemanticCache = (*searchTestCache)(nil)

type searchTestReranker struct {
	err   error
	calls *int
	out   []SearchCandidate
}

func (r searchTestReranker) Rerank(context.Context, string, []SearchCandidate) ([]SearchCandidate, error) {
	if r.calls != nil {
		(*r.calls)++
	}
	return r.out, r.err
}

func TestNewDependenciesWiresOptionalAdaptersAtConstruction(t *testing.T) {
	cache := &searchTestCache{}
	ranker := &searchTestReranker{}
	d := NewDependencies(DependenciesConfig{
		Embedder:      embTestEmbedder{},
		Store:         &searchTestStore{},
		Reranker:      ranker,
		CandidateMul:  4,
		SemanticCache: cache,
	})
	if d.reranker != ranker || d.cache != cache || d.candidateMul != 4 {
		t.Fatalf("optional search adapters were not wired at construction: %+v", d)
	}
}

func TestQueryBatchesFragmentsAndCapsResultsPerFile(t *testing.T) {
	emb := &searchTestEmbedder{}
	st := &searchTestStore{results: []SearchCandidate{
		{Text: "a-1", FilePath: "a.md", LineStart: 1, Score: 0.9},
		{Text: "a-2", FilePath: "a.md", LineStart: 2, Score: 0.8},
		{Text: "b-1", FilePath: "b.md", LineStart: 1, Score: 0.7},
	}}
	d := NewDependencies(DependenciesConfig{
		Embedder:               emb,
		Store:                  st,
		QueryPrefix:            "search_query: ",
		MaxFragments:           4,
		MaxConcurrentFragments: 2,
		MaxTopK:                10,
		MaxChunksPerFile:       1,
	})

	got, err := d.Query(context.Background(), Request{Query: "alpha. beta", TopK: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Chunks) != 2 {
		t.Fatalf("got %d chunks, want 2: %+v", len(got.Chunks), got.Chunks)
	}
	if got.Chunks[0].FilePath != "a.md" || got.Chunks[1].FilePath != "b.md" {
		t.Fatalf("chunks = %+v, want one result per file in score order", got.Chunks)
	}

	if len(emb.batchInputs) != 1 {
		t.Fatalf("batch calls = %d, want 1", len(emb.batchInputs))
	}
	wantInputs := []string{"search_query: alpha. beta", "search_query: alpha", "search_query: beta"}
	for i, want := range wantInputs {
		if emb.batchInputs[0][i] != want {
			t.Errorf("batch input %d = %q, want %q", i, emb.batchInputs[0][i], want)
		}
	}
	if st.hybridCalls != 3 {
		t.Fatalf("hybrid calls = %d, want 3 (original query + one per sentence)", st.hybridCalls)
	}
	wantSearchQueries := map[string]bool{"alpha. beta": true, "alpha": true, "beta": true}
	for _, query := range st.hybridQueries {
		if !wantSearchQueries[query] {
			t.Errorf("hybrid query = %q: embedding task instructions must not become lexical search terms", query)
		}
		delete(wantSearchQueries, query)
	}
	if len(wantSearchQueries) != 0 {
		t.Errorf("missing raw hybrid queries: %v", wantSearchQueries)
	}
}

func TestSplitFragmentsKeepsOriginalQueryFirst(t *testing.T) {
	tests := []struct {
		name         string
		query        string
		maxFragments int
		want         []string
	}{
		{"multi sentence keeps original first", "alpha. beta", 16, []string{"alpha. beta", "alpha", "beta"}},
		{"single sentence unchanged", "single sentence", 16, []string{"single sentence"}},
		{"single sentence with punctuation unchanged", "single sentence.", 16, []string{"single sentence"}},
		{"empty falls back to raw query", "   ", 16, []string{"   "}},
		{"original never trimmed", "a. b. c. d. e. f", 3, []string{"a. b. c. d. e. f", "a", "b"}},
		{"original survives max of one", "a. b", 1, []string{"a. b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitFragments(tt.query, tt.maxFragments)
			if len(got) != len(tt.want) {
				t.Fatalf("splitFragments(%q) = %#v, want %#v", tt.query, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("splitFragments(%q) = %#v, want %#v", tt.query, got, tt.want)
				}
			}
		})
	}
}

// Regression test: chunks of one section share the section's LineStart, so
// dedup keys must include ChunkIndex or all but one of them vanish.
func TestMultiSearchKeepsDistinctChunksOfOneSection(t *testing.T) {
	st := &searchTestStore{results: []SearchCandidate{
		{Text: "part one", FilePath: "a.md", LineStart: 7, ChunkIndex: 0, Score: 0.9},
		{Text: "part two", FilePath: "a.md", LineStart: 7, ChunkIndex: 1, Score: 0.8},
	}}
	d := NewDependencies(DependenciesConfig{
		Embedder:         embTestEmbedder{},
		Store:            st,
		MaxTopK:          10,
		MaxChunksPerFile: 3,
	})

	got, err := d.Query(context.Background(), Request{Query: "section", TopK: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Chunks) != 2 || got.Chunks[0].Text != "part one" || got.Chunks[1].Text != "part two" {
		t.Fatalf("chunks = %+v, want both same-section chunks in score order", got.Chunks)
	}
}

func TestQueryBypassesCacheForFilteredSearches(t *testing.T) {
	cache := &searchTestCache{
		hit:    true,
		chunks: []cache.Candidate{{Text: "cached", FilePath: "cached.md", LineStart: 1}},
	}
	st := &searchTestStore{results: []SearchCandidate{{Text: "fresh", FilePath: "fresh.md", LineStart: 1}}}
	d := NewDependencies(DependenciesConfig{
		Embedder:      embTestEmbedder{},
		Store:         st,
		SemanticCache: cache,
	})

	got, err := d.Query(context.Background(), Request{Query: "same", TopK: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !got.FromCache || got.Chunks[0].Text != "cached" {
		t.Fatalf("unfiltered result = %+v, want cache hit", got)
	}

	got, err = d.Query(context.Background(), Request{
		Query:  "same",
		TopK:   1,
		Filter: &Filter{FilePath: "fresh.md"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.FromCache || got.Chunks[0].Text != "fresh" {
		t.Fatalf("filtered result = %+v, want fresh store result", got)
	}
	if cache.getCalls != 1 {
		t.Fatalf("cache get calls = %d, want 1", cache.getCalls)
	}
	if st.hybridCalls != 1 {
		t.Fatalf("hybrid calls = %d, want 1", st.hybridCalls)
	}
	if st.lastFilter == nil || st.lastFilter.FilePath != "fresh.md" {
		t.Fatalf("store filter = %+v, want fresh.md", st.lastFilter)
	}
}

type embTestEmbedder struct{}

func (embTestEmbedder) Embed(context.Context, string) ([]float32, error) { return []float32{1}, nil }
func (embTestEmbedder) Dimensions() int                                  { return 1 }

func TestQueryRerankerFailureKeepsResultsBounded(t *testing.T) {
	st := &searchTestStore{results: []SearchCandidate{
		{Text: "one", FilePath: "one.md", LineStart: 1, Score: 0.9},
		{Text: "two", FilePath: "two.md", LineStart: 1, Score: 0.8},
		{Text: "three", FilePath: "three.md", LineStart: 1, Score: 0.7},
	}}
	d := NewDependencies(DependenciesConfig{
		Embedder:         embTestEmbedder{},
		Store:            st,
		Reranker:         searchTestReranker{err: errors.New("sidecar unavailable")},
		CandidateMul:     2,
		MaxTopK:          10,
		MaxChunksPerFile: 10,
	})

	got, err := d.Query(context.Background(), Request{Query: "query", TopK: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Chunks) != 2 {
		t.Fatalf("got %d chunks, want bounded result count 2", len(got.Chunks))
	}
	if got.Chunks[0].Text != "one" || got.Chunks[1].Text != "two" {
		t.Fatalf("fallback chunks = %+v, want original score order", got.Chunks)
	}
}

func TestAdaptiveRerankingSkipsHighConfidenceHybridResult(t *testing.T) {
	calls := 0
	top := SearchCandidate{Text: "top", FilePath: "top.md", LineStart: 1, Score: 0.90}
	second := SearchCandidate{Text: "second", FilePath: "second.md", LineStart: 1, Score: 0.70}
	store := &searchTestStore{signals: HybridSearchResult{
		Fused:   []SearchCandidate{top, second},
		Dense:   []SearchCandidate{{FilePath: "top.md", LineStart: 1, Score: 0.90}},
		Lexical: []SearchCandidate{{FilePath: "top.md", LineStart: 1, Score: 12}},
	}}
	d := NewDependencies(DependenciesConfig{
		Embedder:                embTestEmbedder{},
		Store:                   store,
		Reranker:                searchTestReranker{calls: &calls},
		AdaptiveRerank:          true,
		AdaptiveMarginThreshold: 0.01,
		CandidateMul:            2,
		MaxTopK:                 10,
		MaxChunksPerFile:        10,
	})

	got, err := d.Query(context.Background(), Request{Query: "high confidence", TopK: 2})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("reranker calls = %d, want 0", calls)
	}
	if !got.Rerank.Enabled || got.Rerank.Attempted || got.Rerank.Reason != "high_confidence" {
		t.Fatalf("rerank telemetry = %+v, want a high-confidence skip", got.Rerank)
	}
	if len(got.Chunks) != 2 || got.Chunks[0].Text != "top" {
		t.Fatalf("chunks = %+v, want fused order", got.Chunks)
	}
}

func TestAdaptiveRerankingRunsWhenLegsDisagree(t *testing.T) {
	calls := 0
	top := SearchCandidate{Text: "top", FilePath: "top.md", LineStart: 1, Score: 0.90}
	second := SearchCandidate{Text: "second", FilePath: "second.md", LineStart: 1, Score: 0.89}
	store := &searchTestStore{signals: HybridSearchResult{
		Fused:   []SearchCandidate{top, second},
		Dense:   []SearchCandidate{{FilePath: "top.md", LineStart: 1, Score: 0.90}},
		Lexical: []SearchCandidate{{FilePath: "second.md", LineStart: 1, Score: 12}},
	}}
	d := NewDependencies(DependenciesConfig{
		Embedder:                embTestEmbedder{},
		Store:                   store,
		Reranker:                searchTestReranker{calls: &calls, out: []SearchCandidate{second, top}},
		AdaptiveRerank:          true,
		AdaptiveMarginThreshold: 0.01,
		CandidateMul:            2,
		MaxTopK:                 10,
		MaxChunksPerFile:        10,
	})

	got, err := d.Query(context.Background(), Request{Query: "ambiguous", TopK: 2})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !got.Rerank.Attempted || got.Rerank.Reason != "leg_disagreement" {
		t.Fatalf("calls=%d telemetry=%+v, want one disagreement rerank", calls, got.Rerank)
	}
	if got.Chunks[0].Text != "second" {
		t.Fatalf("chunks = %+v, want reranked order", got.Chunks)
	}
}
