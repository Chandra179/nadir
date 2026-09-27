package search

import "testing"

func TestFuseHybridUsesWeightedRanksAndDeterministicTies(t *testing.T) {
	a := SearchCandidate{FilePath: "a.md", LineStart: 1, Text: "alpha"}
	b := SearchCandidate{FilePath: "b.md", LineStart: 1, Text: "beta"}
	result := HybridSearchResult{
		Dense:   []SearchCandidate{a, b},
		Lexical: []SearchCandidate{b, a},
	}
	base := FusionConfig{RRFK: 60, DenseWeight: 1, BM25Weight: 1, MinExactTokens: 1, MinHeaderTokens: 1}
	got := fuseHybrid("unrelated", QueryTypeFactoid, result, base)
	if len(got) != 2 || got[0].Key() != "a.md:1" || got[1].Key() != "b.md:1" {
		t.Fatalf("equal RRF scores = %#v, want deterministic key order", got)
	}

	got = fuseHybrid("unrelated", QueryTypeFactoid, result, FusionConfig{
		RRFK: 60, DenseWeight: 1, BM25Weight: 2, MinExactTokens: 1, MinHeaderTokens: 1,
	})
	if got[0].Key() != "b.md:1" {
		t.Fatalf("weighted RRF top = %s, want lexical rank-1 candidate b.md:1", got[0].Key())
	}
}

func TestFuseHybridAppliesExactAndHeaderBoostsWithQueryProfile(t *testing.T) {
	result := HybridSearchResult{
		Dense: []SearchCandidate{
			{FilePath: "formula.md", LineStart: 1, Text: "The secant formula is useful."},
			{FilePath: "other.md", LineStart: 1, Text: "A method uses a line."},
		},
		Lexical: []SearchCandidate{
			{FilePath: "other.md", LineStart: 1, Text: "A method uses a line."},
			{FilePath: "formula.md", LineStart: 1, Text: "The secant formula is useful."},
		},
	}
	got := fuseHybrid("secant formula", QueryTypeFormula, result, FusionConfig{
		RRFK: 60, DenseWeight: 1, BM25Weight: 1, MinExactTokens: 2, MinHeaderTokens: 1,
		Profiles: map[QueryType]FusionProfile{
			QueryTypeFormula: {
				DenseWeight: 1, BM25Weight: 1, ExactMatchBoost: 0.1,
				HeaderMatchBoost: 0.2, MinExactTokens: 2, MinHeaderTokens: 1,
			},
		},
	})
	if got[0].FilePath != "formula.md" {
		t.Fatalf("boosted top = %#v, want exact-match candidate first", got)
	}
	if got[0].Score <= got[1].Score {
		t.Fatalf("boosted scores = %v/%v, want strict ordering", got[0].Score, got[1].Score)
	}
}

func TestResolveQueryTypeUsesExplicitTypeOrStableHeuristics(t *testing.T) {
	if got := resolveQueryType("how does the method work", QueryTypeFormula); got != QueryTypeFormula {
		t.Fatalf("explicit query type = %q, want formula", got)
	}
	tests := map[string]QueryType{
		"derivative formula":              QueryTypeFormula,
		"compare cosine and sine":         QueryTypeComparison,
		"how does the secant method work": QueryTypeProcedure,
		"what is the secant of an angle":  QueryTypeFactoid,
	}
	for query, want := range tests {
		if got := resolveQueryType(query, QueryTypeUnknown); got != want {
			t.Errorf("resolveQueryType(%q) = %q, want %q", query, got, want)
		}
	}
}
