package search

import (
	"context"
	"errors"
	"sync"
	"testing"

	"nadir/internal/retrieval/cache"
)

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
	mu          sync.Mutex
	hybridCalls int
	lastFilter  *Filter
	results     []SearchCandidate
	signals     HybridSearchResult
}

func (s *searchTestStore) KeywordSearch(context.Context, string, int, *Filter) ([]SearchCandidate, error) {
	return nil, nil
}

func (s *searchTestStore) HybridSearch(_ context.Context, _ []float32, _ string, _ int, filter *Filter) (HybridSearchResult, error) {
	s.mu.Lock()
	s.hybridCalls++
	if filter != nil {
		copyFilter := *filter
		s.lastFilter = &copyFilter
	}
	results := s.signals
	if results.Fused == nil {
		results.Fused = append([]SearchCandidate(nil), s.results...)
	}
	s.mu.Unlock()
	return results, nil
}

var _ documentSearcher = (*searchTestStore)(nil)

type searchTestCache struct {
	chunks   []cache.Candidate
	hit      bool
	getCalls int
}

func (c *searchTestCache) Get(context.Context, string) ([]cache.Candidate, bool, error) {
	c.getCalls++
	return append([]cache.Candidate(nil), c.chunks...), c.hit, nil
}
func (c *searchTestCache) Set(context.Context, string, []cache.Candidate) error { return nil }
func (c *searchTestCache) Clear(context.Context) error                          { return nil }

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
	wantInputs := []string{"search_query: alpha", "search_query: beta"}
	for i, want := range wantInputs {
		if emb.batchInputs[0][i] != want {
			t.Errorf("batch input %d = %q, want %q", i, emb.batchInputs[0][i], want)
		}
	}
	if st.hybridCalls != 2 {
		t.Fatalf("hybrid calls = %d, want 2", st.hybridCalls)
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
