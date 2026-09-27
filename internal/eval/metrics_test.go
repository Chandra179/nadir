package evaluation

import (
	"math"
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
