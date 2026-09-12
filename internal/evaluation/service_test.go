package eval

import (
	"context"
	"os"
	"testing"

	"nadir/internal/retrieval/search"
)

type fakeSearcher struct {
	called  int
	request search.Request
}

func (f *fakeSearcher) Query(_ context.Context, request search.Request) (search.Result, error) {
	f.called++
	f.request = request
	return search.Result{Chunks: []search.Chunk{
		{FilePath: "samples/math.md", Text: "The answer is 42."},
		{FilePath: "samples/other.md", Text: "unrelated"},
	}}, nil
}

func TestHarnessBypassesCacheAndAggregatesResults(t *testing.T) {
	searcher := new(fakeSearcher)
	harness := NewDependencies(DependenciesConfig{Searcher: searcher})
	report, err := harness.Run(context.Background(), &GoldenSet{Queries: []GoldenQuery{{
		ID:    "answer",
		Query: "what is the answer",
		Relevant: []RelevantChunk{{
			File:     "math.md",
			Contains: "42",
		}},
	}}}, 2, 2)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if searcher.called != 2 {
		t.Fatalf("Query calls = %d, want 2", searcher.called)
	}
	if !searcher.request.SkipCache {
		t.Fatal("evaluation must bypass semantic cache")
	}
	if report.Aggregate.HitRateAtK != 1 || report.Aggregate.MRRAt10 != 1 {
		t.Fatalf("aggregate = %+v, want perfect first-rank result", report.Aggregate)
	}
}

func TestLoadGoldenSetRejectsDuplicateIDs(t *testing.T) {
	path := t.TempDir() + "/golden.json"
	if err := os.WriteFile(path, []byte(`{"queries":[{"id":"q","query":"one","relevant":[{"contains":"one"}]},{"id":"q","query":"two","relevant":[{"contains":"two"}]}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadGoldenSet(path); err == nil {
		t.Fatal("LoadGoldenSet accepted duplicate IDs")
	}
}

func TestMatchedRelevantSupportsHostAndContainerPaths(t *testing.T) {
	chunk := search.Chunk{FilePath: "/app/source/trig-functions.md", Text: "reciprocal of cosine"}
	matched := MatchedRelevant(chunk, []RelevantChunk{{File: "samples/trig-functions.md", Contains: "RECIPROCAL"}})
	if len(matched) != 1 || matched[0] != 0 {
		t.Fatalf("MatchedRelevant() = %v, want [0]", matched)
	}
}
