package search

import (
	"context"
	"fmt"
	"testing"

	"nadir/internal/core/retrieval/cache"
)

func TestCacheMissEmbedsTheQuestionOnceAndSharesItsVector(t *testing.T) {
	for _, query := range []string{"what is x?", "alpha. beta"} {
		t.Run(query, func(t *testing.T) {
			emb := &searchTestEmbedder{}
			policy := &searchTestCache{}
			store := &searchTestStore{results: []SearchCandidate{{Text: "fresh", FilePath: "a.md", Score: 1}}}
			var detached func(context.Context)
			d := NewDependencies(DependenciesConfig{
				Embedder: emb, Store: store, SemanticCache: policy,
				CacheWrite: func(_ context.Context, work func(context.Context)) bool { detached = work; return true },
			})
			if _, err := d.Query(context.Background(), Request{Query: query, TopK: 5}); err != nil {
				t.Fatal(err)
			}
			if detached == nil {
				t.Fatal("miss did not schedule a cache write")
			}
			detached(context.Background())

			// The batch is the only model call: the lookup, the search and
			// the detached write all reuse the question's own (first) vector.
			if len(emb.batchInputs) != 1 {
				t.Fatalf("embedding calls = %d, want 1: %q", len(emb.batchInputs), emb.batchInputs)
			}
			fullQuestionVector := []float32{1}
			if len(policy.getVectors) != 1 || policy.getVectors[0][0] != fullQuestionVector[0] {
				t.Fatalf("cache lookup vectors = %v, want the full-question vector", policy.getVectors)
			}
			if len(policy.writes) != 1 || policy.writes[0].vector[0] != fullQuestionVector[0] || policy.writes[0].requestedTopK != 5 {
				t.Fatalf("cache writes = %+v, want the same vector and the request size", policy.writes)
			}
		})
	}
}

func TestCompleteButShortCacheEntryAnswersTheRequest(t *testing.T) {
	// A one-document corpus (or the per-file cap) returns fewer chunks than
	// top_k; the entry still records that it answered a top_k=5 request.
	policy := &searchTestCache{hit: true, requestedTopK: 5, chunks: []cache.Candidate{
		{Text: "one", FilePath: "a.md"}, {Text: "two", FilePath: "a.md", ChunkIndex: 1},
	}}
	store := &searchTestStore{results: []SearchCandidate{{Text: "fresh", FilePath: "a.md", Score: 1}}}
	d := NewDependencies(DependenciesConfig{Embedder: embTestEmbedder{}, Store: store, SemanticCache: policy})

	result, err := d.Query(context.Background(), Request{Query: "query", TopK: 5})
	if err != nil {
		t.Fatal(err)
	}
	if !result.FromCache || len(result.Chunks) != 2 || store.hybridCalls != 0 {
		t.Fatalf("complete short entry missed: %+v hybridCalls=%d", result, store.hybridCalls)
	}

	result, err = d.Query(context.Background(), Request{Query: "query", TopK: 8})
	if err != nil {
		t.Fatal(err)
	}
	if result.FromCache || store.hybridCalls != 1 {
		t.Fatalf("an entry from a smaller request answered a larger one: %+v", result)
	}
}

func TestPerFileCapBackfillsFromLowerRanks(t *testing.T) {
	var results []SearchCandidate
	for i := range 6 {
		results = append(results, SearchCandidate{Text: fmt.Sprintf("a-%d", i), FilePath: "a.md", LineStart: i, Score: 0.9 - float32(i)*0.01})
	}
	for i := range 4 {
		results = append(results, SearchCandidate{Text: fmt.Sprintf("b-%d", i), FilePath: "b.md", LineStart: i, Score: 0.5 - float32(i)*0.01})
	}
	store := &searchTestStore{results: results}
	d := NewDependencies(DependenciesConfig{Embedder: embTestEmbedder{}, Store: store, MaxChunksPerFile: 3})

	got, err := d.Query(context.Background(), Request{Query: "query", TopK: 5})
	if err != nil {
		t.Fatal(err)
	}
	perFile := map[string]int{}
	for _, chunk := range got.Chunks {
		perFile[chunk.FilePath]++
	}
	if len(got.Chunks) != 5 || perFile["a.md"] != 3 || perFile["b.md"] != 2 {
		t.Fatalf("cap shrank the result set instead of backfilling: %+v", got.Chunks)
	}
	if len(store.hybridLimits) != 1 || store.hybridLimits[0] != 5*capOverfetchMul {
		t.Fatalf("store limits = %v, want one request for %d candidates", store.hybridLimits, 5*capOverfetchMul)
	}
}

func TestRerankerStillReceivesOnlyItsCandidateBudget(t *testing.T) {
	var results []SearchCandidate
	for i := range 40 {
		results = append(results, SearchCandidate{Text: fmt.Sprintf("c-%d", i), FilePath: fmt.Sprintf("f%d.md", i), Score: 1 - float32(i)*0.01})
	}
	var seen int
	ranker := capturingReranker{seen: &seen}
	d := NewDependencies(DependenciesConfig{
		Embedder: embTestEmbedder{}, Store: &searchTestStore{results: results},
		Reranker: ranker, CandidateMul: 4, MaxChunksPerFile: 3,
	})
	if _, err := d.Query(context.Background(), Request{Query: "query", TopK: 5}); err != nil {
		t.Fatal(err)
	}
	if seen != 5*4 {
		t.Fatalf("reranker candidates = %d, want topK*candidateMul = 20 despite store overfetch", seen)
	}
}

type capturingReranker struct{ seen *int }

func (r capturingReranker) Rerank(_ context.Context, _ string, candidates []SearchCandidate) ([]SearchCandidate, error) {
	*r.seen = len(candidates)
	return candidates, nil
}
