package evaluation

import "math"

// ReciprocalRank returns 1/rank for a 1-based rank, or zero when no result
// was found.
func ReciprocalRank(rank int) float64 {
	if rank <= 0 {
		return 0
	}
	return 1 / float64(rank)
}

// MRR returns mean reciprocal rank over first relevant result positions.
func MRR(firstHitRanks []int) float64 {
	if len(firstHitRanks) == 0 {
		return 0
	}
	var sum float64
	for _, rank := range firstHitRanks {
		sum += ReciprocalRank(rank)
	}
	return sum / float64(len(firstHitRanks))
}

// HitRate returns the fraction of queries with at least one relevant result.
func HitRate(firstHitRanks []int) float64 {
	if len(firstHitRanks) == 0 {
		return 0
	}
	hits := 0
	for _, rank := range firstHitRanks {
		if rank > 0 {
			hits++
		}
	}
	return float64(hits) / float64(len(firstHitRanks))
}

// Recall returns mean per-query recall.
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

// DCG computes binary-relevance discounted cumulative gain.
func DCG(hits []bool) float64 {
	var score float64
	for i, hit := range hits {
		if hit {
			score += 1 / math.Log2(float64(i+2))
		}
	}
	return score
}

// IDCG computes ideal binary-relevance gain for a query with n relevant
// entries, truncated at k.
func IDCG(n, k int) float64 {
	if n > k {
		n = k
	}
	var score float64
	for i := 0; i < n; i++ {
		score += 1 / math.Log2(float64(i+2))
	}
	return score
}

// NDCG computes normalized discounted cumulative gain for one query.
func NDCG(hits []bool, numRelevant, k int) float64 {
	if numRelevant <= 0 || k <= 0 {
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

// MeanNDCG averages NDCG over queries.
func MeanNDCG(hitLists [][]bool, relevantCounts []int, k int) float64 {
	if len(hitLists) == 0 {
		return 0
	}
	var sum float64
	for i, hits := range hitLists {
		count := 0
		if i < len(relevantCounts) {
			count = relevantCounts[i]
		}
		sum += NDCG(hits, count, k)
	}
	return sum / float64(len(hitLists))
}

// Percentile returns a linearly interpolated percentile of a copied sample.
func Percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j] < sorted[j-1]; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 100 {
		return sorted[len(sorted)-1]
	}
	position := (p / 100) * float64(len(sorted)-1)
	lo := int(math.Floor(position))
	hi := int(math.Ceil(position))
	if lo == hi {
		return sorted[lo]
	}
	return sorted[lo] + (sorted[hi]-sorted[lo])*(position-float64(lo))
}
