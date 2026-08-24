package eval

import (
	"math"
	"testing"
)

func TestReciprocalRank(t *testing.T) {
	cases := []struct {
		rank int
		want float64
	}{
		{0, 0},
		{-1, 0},
		{1, 1},
		{2, 0.5},
		{10, 0.1},
	}
	for _, c := range cases {
		if got := ReciprocalRank(c.rank); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("ReciprocalRank(%d) = %v, want %v", c.rank, got, c.want)
		}
	}
}

func TestMRR(t *testing.T) {
	got := MRR([]int{1, 3, 0})
	want := (1.0 + 1.0/3.0 + 0.0) / 3.0
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("MRR = %v, want %v", got, want)
	}
	if MRR(nil) != 0 {
		t.Error("MRR of empty slice should be 0")
	}
}

func TestHitRate(t *testing.T) {
	if got := HitRate([]int{2, 0, 5, 1}); got != 0.75 {
		t.Errorf("HitRate = %v, want 0.75", got)
	}
	if HitRate(nil) != 0 {
		t.Error("HitRate of empty slice should be 0")
	}
}

func TestRecall(t *testing.T) {
	found := []int{2, 0, 1}
	total := []int{2, 4, 1}
	want := (1.0 + 0.0 + 1.0) / 3.0
	if got := Recall(found, total); math.Abs(got-want) > 1e-9 {
		t.Errorf("Recall = %v, want %v", got, want)
	}
}

func TestNDCGPerfectOrdering(t *testing.T) {
	hits := []bool{true, true, false}
	got := NDCG(hits, 2, 10)
	if math.Abs(got-1.0) > 1e-9 {
		t.Errorf("perfect ordering NDCG = %v, want 1", got)
	}
}

func TestNDCGImperfectOrdering(t *testing.T) {
	// Relevant doc at rank 2 out of 1 relevant: DCG = 1/log2(3), IDCG = 1/log2(2).
	got := NDCG([]bool{false, true}, 1, 10)
	want := (1 / math.Log2(3)) / (1 / math.Log2(2))
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("NDCG = %v, want %v", got, want)
	}
}

func TestNDGCTruncationAtK(t *testing.T) {
	// Hit beyond k must not count.
	if got := NDCG([]bool{false, false, true}, 1, 2); got != 0 {
		t.Errorf("NDCG@2 with hit at rank 3 = %v, want 0", got)
	}
}

func TestMeanNDCG(t *testing.T) {
	lists := [][]bool{{true, false}, {false, true}}
	nums := []int{1, 1}
	got := MeanNDCG(lists, nums, 10)
	q1 := 1.0
	q2 := (1 / math.Log2(3)) / (1 / math.Log2(2))
	want := (q1 + q2) / 2
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("MeanNDCG = %v, want %v", got, want)
	}
}

func TestPercentile(t *testing.T) {
	vals := []float64{10, 20, 30, 40, 100}
	if got := Percentile(vals, 50); got != 30 {
		t.Errorf("p50 = %v, want 30", got)
	}
	if got := Percentile(vals, 95); math.Abs(got-88.0) > 1e-9 {
		t.Errorf("p95 = %v, want 88", got)
	}
	if Percentile(nil, 50) != 0 {
		t.Error("percentile of empty slice should be 0")
	}
}
