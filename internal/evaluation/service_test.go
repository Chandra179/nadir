package evaluation

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/mock"
	"nadir/internal/retrieval/search"
	"nadir/mocks"
)

func TestHarnessBypassesCacheAndAggregatesResults(t *testing.T) {
	searcher := &mocks.MockRetriever{}
	called := 0
	var request search.Request
	searcher.EXPECT().Query(mock.Anything, mock.Anything).
		Run(func(_ context.Context, got search.Request) {
			called++
			request = got
		}).
		Return(search.Result{Chunks: []search.Chunk{
			{FilePath: "samples/math.md", Text: "The answer is 42."},
			{FilePath: "samples/other.md", Text: "unrelated"},
		}}, nil).
		Twice()
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
	if called != 2 {
		t.Fatalf("Query calls = %d, want 2", called)
	}
	if !request.SkipCache {
		t.Fatal("evaluation must bypass semantic cache")
	}
	if report.Aggregate.HitRateAtK != 1 || report.Aggregate.MRRAt10 != 1 {
		t.Fatalf("aggregate = %+v, want perfect first-rank result", report.Aggregate)
	}
	if report.PerQuery[0].DistractorHits != 0 {
		t.Fatalf("distractor hits = %d, want 0", report.PerQuery[0].DistractorHits)
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

func TestLoadGoldenSetRequiresAnnotationsForSchemaV2(t *testing.T) {
	path := t.TempDir() + "/golden.json"
	data := []byte(`{"schema_version":2,"queries":[{"id":"q","query":"one","relevant":[{"contains":"one"}]}]}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadGoldenSet(path); err == nil {
		t.Fatal("LoadGoldenSet accepted schema v2 query without annotations")
	}
}

func TestMatchedDistractors(t *testing.T) {
	chunk := search.Chunk{FilePath: "/app/source/calculus.md", Text: "The chain rule composes derivatives."}
	matched := MatchedDistractors(chunk, []RelevantChunk{{File: "calculus.md", Contains: "chain rule"}})
	if len(matched) != 1 || matched[0] != 0 {
		t.Fatalf("MatchedDistractors() = %v, want [0]", matched)
	}
}

func TestActiveGoldenFixtureIsAnnotated(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(filepath.Dir(sourceFile), "../../test/evaluation/golden.json")
	golden, err := LoadGoldenSet(path)
	if err != nil {
		t.Fatalf("LoadGoldenSet(active fixture): %v", err)
	}
	if golden.SchemaVersion != 2 {
		t.Fatalf("schema version = %d, want 2", golden.SchemaVersion)
	}
	if len(golden.Queries) < 100 {
		t.Fatalf("query count = %d, want at least 100", len(golden.Queries))
	}
	for _, query := range golden.Queries {
		if len(query.Distractors) == 0 {
			t.Errorf("query %q has no distractor annotation", query.ID)
		}
	}
}
