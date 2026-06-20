package eval

import (
	"math"
	"testing"
)

func graded(set ...string) map[string]int {
	m := make(map[string]int, len(set))
	for _, s := range set {
		m[s] = 1
	}
	return m
}

func TestRecallAtK(t *testing.T) {
	tests := []struct {
		name      string
		retrieved []string
		graded    map[string]int
		k         int
		want      float64
	}{
		{
			name:      "all relevant in top-3",
			retrieved: []string{"a", "b", "c", "d"},
			graded:    graded("a", "b"),
			k:         3,
			want:      1.0,
		},
		{
			name:      "one of two relevant in top-2",
			retrieved: []string{"a", "x", "b"},
			graded:    graded("a", "b"),
			k:         2,
			want:      0.5,
		},
		{
			name:      "none relevant",
			retrieved: []string{"x", "y"},
			graded:    graded("a", "b"),
			k:         2,
			want:      0,
		},
		{
			name:      "k larger than retrieved",
			retrieved: []string{"a"},
			graded:    graded("a", "b"),
			k:         10,
			want:      0.5,
		},
		{
			name:      "empty relevant set",
			retrieved: []string{"a"},
			graded:    graded(),
			k:         1,
			want:      0,
		},
		{
			name:      "k zero",
			retrieved: []string{"a"},
			graded:    graded("a"),
			k:         0,
			want:      0,
		},
		{
			name:      "graded relevance: grade 0 is not relevant",
			retrieved: []string{"a", "b"},
			graded:    map[string]int{"a": 0, "b": 2},
			k:         2,
			want:      1.0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := RecallAtK(tc.retrieved, tc.graded, tc.k)
			if math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("RecallAtK = %.6f, want %.6f", got, tc.want)
			}
		})
	}
}

func TestPrecisionAtK(t *testing.T) {
	tests := []struct {
		name      string
		retrieved []string
		graded    map[string]int
		k         int
		want      float64
	}{
		{
			name:      "2 hits in top-5",
			retrieved: []string{"a", "x", "b", "y", "z"},
			graded:    graded("a", "b"),
			k:         5,
			want:      0.4,
		},
		{
			name:      "all hits",
			retrieved: []string{"a", "b"},
			graded:    graded("a", "b"),
			k:         2,
			want:      1.0,
		},
		{
			name:      "k larger than retrieved",
			retrieved: []string{"a", "x"},
			graded:    graded("a"),
			k:         5,
			want:      0.2,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := PrecisionAtK(tc.retrieved, tc.graded, tc.k)
			if math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("PrecisionAtK = %.6f, want %.6f", got, tc.want)
			}
		})
	}
}

func TestReciprocalRank(t *testing.T) {
	tests := []struct {
		name      string
		retrieved []string
		graded    map[string]int
		want      float64
	}{
		{
			name:      "relevant at rank 1",
			retrieved: []string{"a", "b", "c"},
			graded:    graded("a"),
			want:      1.0,
		},
		{
			name:      "relevant at rank 3",
			retrieved: []string{"x", "y", "a", "z"},
			graded:    graded("a"),
			want:      1.0 / 3.0,
		},
		{
			name:      "no relevant",
			retrieved: []string{"x", "y"},
			graded:    graded("a"),
			want:      0,
		},
		{
			name:      "first of multiple relevant wins",
			retrieved: []string{"x", "a", "b"},
			graded:    graded("a", "b"),
			want:      0.5,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ReciprocalRank(tc.retrieved, tc.graded)
			if math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("ReciprocalRank = %.6f, want %.6f", got, tc.want)
			}
		})
	}
}

func TestSuccessAtK(t *testing.T) {
	tests := []struct {
		name      string
		retrieved []string
		graded    map[string]int
		k         int
		want      float64
	}{
		{
			name:      "hit at rank 1, k=5",
			retrieved: []string{"a", "x", "y"},
			graded:    graded("a"),
			k:         5,
			want:      1.0,
		},
		{
			name:      "hit at rank 3, k=5",
			retrieved: []string{"x", "y", "a"},
			graded:    graded("a"),
			k:         5,
			want:      1.0,
		},
		{
			name:      "hit at rank 6, k=5",
			retrieved: []string{"x", "y", "z", "w", "v", "a"},
			graded:    graded("a"),
			k:         5,
			want:      0,
		},
		{
			name:      "no relevant at all",
			retrieved: []string{"x", "y"},
			graded:    graded("a"),
			k:         5,
			want:      0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SuccessAtK(tc.retrieved, tc.graded, tc.k)
			if math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("SuccessAtK = %.6f, want %.6f", got, tc.want)
			}
		})
	}
}

func TestAveragePrecision(t *testing.T) {
	// Hand-derived:
	// retrieved = ["a", "x", "b", "y", "c"], relevant = {a, b, c}
	// R = 3
	// k=1: "a" relevant, P@1 = 1/1 = 1.0
	// k=3: "b" relevant, P@3 = 2/3 = 0.6667
	// k=5: "c" relevant, P@5 = 3/5 = 0.6
	// AP = (1/3) * (1.0 + 0.6667 + 0.6) = 0.7556
	retrieved := []string{"a", "x", "b", "y", "c"}
	g := graded("a", "b", "c")
	got := AveragePrecision(retrieved, g)
	want := (1.0 + 2.0/3.0 + 3.0/5.0) / 3.0
	if math.Abs(got-want) > 1e-4 {
		t.Errorf("AveragePrecision = %.6f, want %.6f", got, want)
	}

	// No relevant items → AP = 0
	if got := AveragePrecision([]string{"x"}, graded("a")); got != 0 {
		t.Errorf("AP with no hits = %.6f, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// NDCG — oracle values from Järvelin & Kekäläinen 2002 worked example
// (ACM TOIS 20(4):422–446, via Wikipedia "Discounted cumulative gain")
//
// Retrieved ranking: D1..D6 with grades [3, 2, 3, 0, 1, 2]
// Ideal pool adds D7=3, D8=2 → ideal sorted desc: [3, 3, 3, 2, 2, 2, 1, 0]
// Truncated to k=6: [3, 3, 3, 2, 2, 2]
//
// DCG@6 (linear)    = 6.861, IDCG@6 = 8.740 → nDCG@6 = 0.785
// DCG@6 (exponential) = 13.849, IDCG@6 = 18.437 → nDCG@6 = 0.751
// ---------------------------------------------------------------------------

func jk2002Graded() map[string]int {
	return map[string]int{
		"D1": 3, "D2": 2, "D3": 3, "D4": 0, "D5": 1, "D6": 2,
		"D7": 3, "D8": 2,
	}
}

func jk2002Retrieved() []string {
	return []string{"D1", "D2", "D3", "D4", "D5", "D6"}
}

func TestDCG_Linear(t *testing.T) {
	// DCG@6 = 3 + 2/log2(3) + 3/log2(4) + 0 + 1/log2(6) + 2/log2(7)
	//       = 3 + 1.262 + 1.5 + 0 + 0.387 + 0.712 = 6.861
	got := DCG([]int{3, 2, 3, 0, 1, 2})
	want := 6.861
	if math.Abs(got-want) > 1e-2 {
		t.Errorf("DCG linear = %.4f, want %.3f", got, want)
	}
}

func TestDCG_Exp(t *testing.T) {
	// DCG@6 exp = 7/1 + 3/log2(3) + 7/2 + 0 + 1/log2(6) + 3/log2(7)
	//           = 7 + 1.893 + 3.5 + 0 + 0.387 + 1.069 = 13.849
	got := DCGExp([]int{3, 2, 3, 0, 1, 2})
	want := 13.849
	if math.Abs(got-want) > 1e-2 {
		t.Errorf("DCG exp = %.4f, want %.3f", got, want)
	}
}

func TestNDCGAtK_JK2002(t *testing.T) {
	// Oracle: nDCG@6 = 0.785 (Järvelin & Kekäläinen 2002)
	got := NDCGAtK(jk2002Retrieved(), jk2002Graded(), 6)
	want := 0.785
	if math.Abs(got-want) > 1e-3 {
		t.Errorf("NDCGAtK linear = %.6f, want %.3f (J&K 2002 oracle)", got, want)
	}
}

func TestNDCGAtKExp_JK2002(t *testing.T) {
	// Oracle: nDCG@6 = 0.751 (exponential formula, Burges 2005 / BEIR-style)
	got := NDCGAtKExp(jk2002Retrieved(), jk2002Graded(), 6)
	want := 0.751
	if math.Abs(got-want) > 1e-3 {
		t.Errorf("NDCGAtKExp = %.6f, want %.3f (Burges 2005 oracle)", got, want)
	}
}

func TestNDCG_PerfectRanking(t *testing.T) {
	// When retrieved is already ideal, NDCG = 1.0
	g := map[string]int{"a": 3, "b": 2, "c": 1}
	got := NDCGAtK([]string{"a", "b", "c"}, g, 3)
	if math.Abs(got-1.0) > 1e-9 {
		t.Errorf("NDCG perfect = %.6f, want 1.0", got)
	}
}

func TestNDCG_NoRelevant(t *testing.T) {
	g := map[string]int{"a": 0, "b": 0}
	got := NDCGAtK([]string{"a", "b"}, g, 2)
	if got != 0 {
		t.Errorf("NDCG no relevant = %.6f, want 0", got)
	}
}

func TestNDCG_BinaryMatchesBothFormulas(t *testing.T) {
	// With binary relevance (grades 0/1), both DCG formulas produce the same NDCG.
	g := map[string]int{"a": 1, "b": 1, "c": 0}
	retrieved := []string{"a", "c", "b"}
	lin := NDCGAtK(retrieved, g, 3)
	exp := NDCGAtKExp(retrieved, g, 3)
	if math.Abs(lin-exp) > 1e-9 {
		t.Errorf("binary NDCG: linear=%.6f exp=%.6f, should be equal", lin, exp)
	}
}

func TestBootstrapCI(t *testing.T) {
	values := []float64{0.5, 0.6, 0.7, 0.8, 0.9}
	mean, low, high := BootstrapCI(values, 1000, 42)

	if math.Abs(mean-0.7) > 1e-9 {
		t.Errorf("mean = %.6f, want 0.7", mean)
	}
	if low > mean || high < mean {
		t.Errorf("CI [%.4f, %.4f] should bracket mean %.4f", low, high, mean)
	}
	if low < 0.5 || high > 0.9 {
		t.Errorf("CI [%.4f, %.4f] should be within data range [0.5, 0.9]", low, high)
	}
}

func TestBootstrapCI_Empty(t *testing.T) {
	mean, low, high := BootstrapCI(nil, 100, 1)
	if mean != 0 || low != 0 || high != 0 {
		t.Errorf("empty: mean=%.4f low=%.4f high=%.4f, want all 0", mean, low, high)
	}
}

func TestBootstrapCI_SingleValue(t *testing.T) {
	mean, low, high := BootstrapCI([]float64{0.5}, 100, 1)
	if mean != 0.5 || low != 0.5 || high != 0.5 {
		t.Errorf("single: mean=%.4f low=%.4f high=%.4f, want all 0.5", mean, low, high)
	}
}

func TestAggregate(t *testing.T) {
	retrieved := [][]string{
		{"a", "b", "c"},
		{"x", "a", "y"},
	}
	gradedSets := []map[string]int{
		graded("a"),
		graded("a"),
	}
	queries := []string{"q1", "q2"}

	rep := Aggregate(retrieved, gradedSets, queries)

	if rep.NumQueries != 2 {
		t.Fatalf("NumQueries = %d, want 2", rep.NumQueries)
	}
	// q1: a at rank1 → RR=1, R@5=1
	// q2: a at rank2 → RR=0.5, R@5=1
	// MRR = (1 + 0.5)/2 = 0.75
	if math.Abs(rep.MRR-0.75) > 1e-9 {
		t.Errorf("MRR = %.6f, want 0.75", rep.MRR)
	}
	if math.Abs(rep.RecallAt5-1.0) > 1e-9 {
		t.Errorf("RecallAt5 = %.6f, want 1.0", rep.RecallAt5)
	}
	// NDCG@10 should be > 0 for both queries
	if rep.NDCG10 <= 0 {
		t.Errorf("NDCG10 = %.6f, want > 0", rep.NDCG10)
	}
	if len(rep.PerQuery) != 2 {
		t.Errorf("PerQuery len = %d, want 2", len(rep.PerQuery))
	}
	if rep.PerQuery[0].RR != 1.0 {
		t.Errorf("q1 RR = %.6f, want 1.0", rep.PerQuery[0].RR)
	}
	// CIs should bracket the means
	if rep.RecallAt5CI[0] > rep.RecallAt5 || rep.RecallAt5CI[1] < rep.RecallAt5 {
		t.Errorf("RecallAt5 CI [%.4f, %.4f] should bracket mean %.4f",
			rep.RecallAt5CI[0], rep.RecallAt5CI[1], rep.RecallAt5)
	}
}

func TestAggregate_Empty(t *testing.T) {
	rep := Aggregate(nil, nil, nil)
	if rep.NumQueries != 0 {
		t.Errorf("NumQueries = %d, want 0", rep.NumQueries)
	}
	if rep.MRR != 0 {
		t.Errorf("MRR = %.6f, want 0", rep.MRR)
	}
}
