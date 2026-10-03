package evaluation

import (
	"math"
	"slices"
	"testing"
)

func TestMetrics(t *testing.T) {
	if got := ReciprocalRank(2); got != 0.5 {
		t.Fatalf("ReciprocalRank(2) = %v, want 0.5", got)
	}
	if got := MRR([]int{1, 3, 0}); math.Abs(got-(1+1.0/3)/3) > 1e-9 {
		t.Fatalf("MRR = %v", got)
	}
	if got := HitRate([]int{2, 0, 5, 1}); got != 0.75 {
		t.Fatalf("HitRate = %v, want 0.75", got)
	}
	if got := Recall([]int{2, 0, 1}, []int{2, 4, 1}); math.Abs(got-2.0/3) > 1e-9 {
		t.Fatalf("Recall = %v, want %v", got, 2.0/3)
	}
	if got := NDCG([]bool{true, false}, 1, 5); got != 1 {
		t.Fatalf("perfect NDCG = %v, want 1", got)
	}
	if got := Percentile([]float64{10, 20, 30, 40, 100}, 95); math.Abs(got-88) > 1e-9 {
		t.Fatalf("p95 = %v, want 88", got)
	}
}

func TestPercentileAndDistributionPreserveRunOrder(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		samples               []float64
		min, median, max, p25 float64
	}{
		{"empty", nil, 0, 0, 0, 0},
		{"one", []float64{7}, 7, 7, 7, 7},
		{"odd", []float64{9, 1, 5}, 1, 5, 9, 3},
		{"even", []float64{9, 1, 5, 3}, 1, 4, 9, 2.5},
		{"ties", []float64{5, 1, 5, 1}, 1, 3, 5, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := slices.Clone(tc.samples)
			got := distribution(tc.samples)
			if got.Min != tc.min || got.Median != tc.median || got.Max != tc.max {
				t.Fatalf("distribution = %+v, want min=%v median=%v max=%v", got, tc.min, tc.median, tc.max)
			}
			if Percentile(tc.samples, -10) != tc.min || Percentile(tc.samples, 110) != tc.max || Percentile(tc.samples, 25) != tc.p25 {
				t.Fatal("percentile boundaries or interpolation changed")
			}
			if !slices.Equal(tc.samples, original) || !slices.Equal(got.Samples, original) {
				t.Fatalf("samples no longer match run order: got %v, want %v", got.Samples, original)
			}
		})
	}
}
