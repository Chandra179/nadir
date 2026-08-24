// Package eval implements retrieval-quality measurement over a golden query
// set: metric math, relevance judging, and report generation used by
// cmd/evalbench to A/B retrieval changes.
package eval

import "math"

// ReciprocalRank returns 1/rank for a 1-based rank, or 0 when rank <= 0
// (no hit found within the evaluated depth).
func ReciprocalRank(rank int) float64 {
	if rank <= 0 {
		return 0
	}
	return 1.0 / float64(rank)
}

// MRR returns the Mean Reciprocal Rank over per-query first-hit ranks,
// where 0 means no hit.
func MRR(firstHitRanks []int) float64 {
	if len(firstHitRanks) == 0 {
		return 0
	}
	var sum float64
	for _, r := range firstHitRanks {
		sum += ReciprocalRank(r)
	}
	return sum / float64(len(firstHitRanks))
}

// HitRate returns the fraction of queries with at least one relevant chunk
// in the result list (first-hit rank > 0).
func HitRate(firstHitRanks []int) float64 {
	if len(firstHitRanks) == 0 {
		return 0
	}
	hits := 0
	for _, r := range firstHitRanks {
		if r > 0 {
			hits++
		}
	}
	return float64(hits) / float64(len(firstHitRanks))
}

// Recall returns mean per-query recall given parallel slices of
// relevant items found and total relevant items per query.
func Recall(found, total []int) float64 {
	if len(found) == 0 || len(found) != len(total) {
		return 0
	}
	var sum float64
	for i := range found {
		if total[i] > 0 {
			sum += float64(found[i]) / float64(total[i])
		}
	}
	return sum / float64(len(found))
}

// DCG computes binary-relevance discounted cumulative gain over a ranked
// hit list (true = relevant document at that position).
func DCG(hits []bool) float64 {
	var d float64
	for i, h := range hits {
		if h {
			d += 1.0 / math.Log2(float64(i+2))
		}
	}
	return d
}

// IDCG is the ideal DCG for a query with n relevant documents truncated
// at depth k.
func IDCG(n, k int) float64 {
	if n > k {
		n = k
	}
	var d float64
	for i := 0; i < n; i++ {
		d += 1.0 / math.Log2(float64(i+2))
	}
	return d
}

// NDCG computes a single query's normalized DCG from its ranked hit flags
// and its number of relevant documents. Returns 0 when there is nothing
// relevant to find.
func NDCG(hits []bool, numRelevant, k int) float64 {
	if numRelevant <= 0 {
		return 0
	}
	if len(hits) > k {
		hits = hits[:k]
	}
	ideal := IDCG(numRelevant, k)
	if ideal == 0 {
		return 0
	}
	return DCG(hits) / ideal
}

// MeanNDCG averages NDCG over queries, where hitLists[i] is the ranked hit
// flag slice and numRelevant[i] the golden relevant count of query i.
func MeanNDCG(hitLists [][]bool, numRelevant []int, k int) float64 {
	if len(hitLists) == 0 {
		return 0
	}
	var sum float64
	for i, hits := range hitLists {
		total := 0
		if i < len(numRelevant) {
			total = numRelevant[i]
		}
		sum += NDCG(hits, total, k)
	}
	return sum / float64(len(hitLists))
}

// Percentile returns the p-th percentile (0..100) of a copy of vals using
// nearest-rank interpolation; returns 0 for empty input.
func Percentile(vals []float64, p float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sorted := make([]float64, len(vals))
	copy(sorted, vals)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j] < sorted[j-1]; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	rank := (p / 100) * float64(len(sorted)-1)
	lo := int(math.Floor(rank))
	hi := int(math.Ceil(rank))
	if hi >= len(sorted) {
		return sorted[len(sorted)-1]
	}
	frac := rank - float64(lo)
	return sorted[lo] + frac*(sorted[hi]-sorted[lo])
}
