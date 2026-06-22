package eval

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"nadir/internal/engine"
)

type fakeRetriever struct {
	results []engine.ScoredChunk
	err     error
}

func (f *fakeRetriever) Search(_ context.Context, _ string, _ int, _ *engine.SearchFilter) ([]engine.ScoredChunk, error) {
	return f.results, f.err
}

func chunk(file string) engine.ScoredChunk {
	return engine.ScoredChunk{DocumentChunk: engine.DocumentChunk{FilePath: file}}
}

func chunkWithLine(file string, line int) engine.ScoredChunk {
	return engine.ScoredChunk{DocumentChunk: engine.DocumentChunk{FilePath: file, LineStart: line}}
}

func TestDedupRankedFiles(t *testing.T) {
	chunks := []engine.ScoredChunk{
		chunk("gitbook/math/trigonometry.md"),
		chunk("gitbook/math/trigonometry.md"),
		chunk("gitbook/golang/goroutine.md"),
	}
	graded := map[string]int{"math/trigonometry.md": 1}
	got := dedupRankedFiles(chunks, graded)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (deduped)", len(got))
	}
	if got[0] != "math/trigonometry.md" {
		t.Errorf("got[0] = %q, want mapped expected form", got[0])
	}
	if got[1] != "gitbook/golang/goroutine.md" {
		t.Errorf("got[1] = %q, want passthrough", got[1])
	}
}

func TestRankChunks(t *testing.T) {
	chunks := []engine.ScoredChunk{
		chunkWithLine("gitbook/math/trigonometry.md", 10),
		chunkWithLine("gitbook/math/trigonometry.md", 50),
		chunkWithLine("gitbook/golang/goroutine.md", 1),
	}
	graded := map[string]int{"math/trigonometry.md": 1}
	got := rankChunks(chunks, graded)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (no dedup at chunk level)", len(got))
	}
	if got[0] != "math/trigonometry.md" || got[1] != "math/trigonometry.md" {
		t.Errorf("chunk 0,1 = %q, %q, want both mapped to expected", got[0], got[1])
	}
}

func TestMatchFile(t *testing.T) {
	tests := []struct {
		retrieved string
		expected  string
		want      bool
	}{
		{"gitbook/math/trigonometry.md", "math/trigonometry.md", true},
		{"math/trigonometry.md", "math/trigonometry.md", true},
		{"gitbook/math/trigonometry.md", "trigonometry.md", true},
		{"gitbook/math/calculus.md", "math/trigonometry.md", false},
		{"a/b/c.md", "x/b/c.md", false},
		{"trigonometry.md", "trigonometry.md", true},
	}
	for _, tc := range tests {
		got := MatchFile(tc.retrieved, tc.expected)
		if got != tc.want {
			t.Errorf("MatchFile(%q, %q) = %v, want %v", tc.retrieved, tc.expected, got, tc.want)
		}
	}
}

func TestRunner_Run_FileLevel(t *testing.T) {
	fr := &fakeRetriever{results: []engine.ScoredChunk{
		chunk("gitbook/math/trigonometry.md"),
		chunk("gitbook/golang/goroutine.md"),
		chunk("gitbook/system-design/rate-limit.md"),
	}}
	gs := &GoldenSet{Queries: []GoldenQuery{
		{Query: "secant formula", ExpectedFiles: []string{"math/trigonometry.md"}},
	}}
	r := &Runner{Searcher: fr, TopK: 10, Granularity: FileLevel}
	rep, err := r.Run(context.Background(), gs, 10)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if rep.NumQueries != 1 {
		t.Fatalf("NumQueries = %d, want 1", rep.NumQueries)
	}
	if rep.MRR != 1.0 {
		t.Errorf("MRR = %.4f, want 1.0 (relevant at rank 1)", rep.MRR)
	}
	if rep.RecallAt5 != 1.0 {
		t.Errorf("RecallAt5 = %.4f, want 1.0", rep.RecallAt5)
	}
	if rep.NDCG10 <= 0 {
		t.Errorf("NDCG10 = %.4f, want > 0", rep.NDCG10)
	}
}

func TestRunner_Run_ChunkLevel(t *testing.T) {
	fr := &fakeRetriever{results: []engine.ScoredChunk{
		chunkWithLine("gitbook/math/trigonometry.md", 10),
		chunkWithLine("gitbook/math/trigonometry.md", 50),
		chunkWithLine("gitbook/golang/goroutine.md", 1),
	}}
	gs := &GoldenSet{Queries: []GoldenQuery{
		{Query: "secant formula", ExpectedFiles: []string{"math/trigonometry.md"}},
	}}
	r := &Runner{Searcher: fr, TopK: 10, Granularity: ChunkLevel}
	rep, err := r.Run(context.Background(), gs, 10)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	// Chunk level: 2 of 3 retrieved are from the relevant file
	// Retrieved = [trig.md, trig.md, goroutine.md]
	// R@5 = 2 hits / 1 relevant file = 1.0 (recall counts distinct relevant files found)
	// But wait — recall counts hits in the graded map, which is file-level.
	// At chunk level, both trig chunks map to "math/trigonometry.md" in the graded map.
	// Recall still counts unique relevant items found / total relevant.
	// Since we have 1 relevant item and it appears (at least once), Recall@5 = 1.0.
	if rep.RecallAt5 != 1.0 {
		t.Errorf("RecallAt5 = %.4f, want 1.0", rep.RecallAt5)
	}
	// Precision@5: 2 hits out of 3 retrieved (within top 5) → but P@5 = hits/k = 2/5 = 0.4
	// Wait, limit = min(5, 3) = 3, hits = 2, P@5 = 2/5 = 0.4
	if rep.PrecisionAt5 != 0.4 {
		t.Errorf("Precision@5 = %.4f, want 0.4 (2 chunk hits / k=5)", rep.PrecisionAt5)
	}
}

func TestRunner_Run_GradedRelevance(t *testing.T) {
	fr := &fakeRetriever{results: []engine.ScoredChunk{
		chunk("gitbook/math/trigonometry.md"),
		chunk("gitbook/math/calculus.md"),
	}}
	gs := &GoldenSet{Queries: []GoldenQuery{
		{
			Query:     "math formulas",
			Relevance: map[string]int{"math/trigonometry.md": 3, "math/calculus.md": 1},
		},
	}}
	r := &Runner{Searcher: fr, TopK: 10, Granularity: FileLevel}
	rep, err := r.Run(context.Background(), gs, 10)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	// Both relevant files retrieved → Recall@10 = 1.0
	if rep.RecallAt10 != 1.0 {
		t.Errorf("Recall@10 = %.4f, want 1.0", rep.RecallAt10)
	}
	// NDCG should reflect grading: trig (grade 3) at rank 1 is ideal
	// Ideal: [3, 1], Actual: [3, 1] → NDCG = 1.0
	if rep.NDCG10 != 1.0 {
		t.Errorf("NDCG10 = %.4f, want 1.0 (ideal ranking)", rep.NDCG10)
	}
}

func TestRunner_Run_Error(t *testing.T) {
	fr := &fakeRetriever{err: errors.New("boom")}
	gs := &GoldenSet{Queries: []GoldenQuery{{Query: "q", ExpectedFiles: []string{"a.md"}}}}
	r := &Runner{Searcher: fr}
	if _, err := r.Run(context.Background(), gs, 10); err == nil {
		t.Fatal("expected error from retriever, got nil")
	}
}

func TestLoadGolden(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "golden.yaml")
	content := []byte("queries:\n  - query: \"secant formula\"\n    expected_files:\n      - \"math/trigonometry.md\"\n  - query: \"goroutine\"\n    expected_files:\n      - \"golang/goroutine.md\"\n    expected_answer: \"Goroutines are lightweight threads managed by the Go runtime.\"\n")
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatal(err)
	}
	gs, err := LoadGolden(p)
	if err != nil {
		t.Fatalf("LoadGolden: %v", err)
	}
	if len(gs.Queries) != 2 {
		t.Fatalf("queries = %d, want 2", len(gs.Queries))
	}
	if gs.Queries[0].Query != "secant formula" {
		t.Errorf("q0 = %q", gs.Queries[0].Query)
	}
	if gs.Queries[1].ExpectedAnswer == "" {
		t.Errorf("q1 expected_answer should be set")
	}
}

func TestGradedRelevance_FromExpectedFiles(t *testing.T) {
	q := GoldenQuery{ExpectedFiles: []string{"a.md", "b.md"}}
	g := q.GradedRelevance()
	if g["a.md"] != 1 || g["b.md"] != 1 {
		t.Errorf("expected grade 1 for both files, got %v", g)
	}
}

func TestGradedRelevance_FromRelevanceMap(t *testing.T) {
	q := GoldenQuery{
		Relevance: map[string]int{"a.md": 3, "b.md": 1, "c.md": 0},
	}
	g := q.GradedRelevance()
	if g["a.md"] != 3 || g["b.md"] != 1 || g["c.md"] != 0 {
		t.Errorf("graded relevance = %v, want {a.md:3, b.md:1, c.md:0}", g)
	}
}
