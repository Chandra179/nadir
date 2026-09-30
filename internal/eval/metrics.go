package evaluation

import (
	"math"
	"sort"
)

// ReciprocalRank returns 1/rank for a 1-based rank, or zero when no result
// was found.
func ReciprocalRank(rank int) float64 {
	if rank <= 0 {
		return 0
	}
	return 1 / float64(rank)
}

// ReciprocalRankAt applies an explicit rank cutoff, irrespective of TopK.
func ReciprocalRankAt(rank, cutoff int) float64 {
	if rank > cutoff {
		return 0
	}
	return ReciprocalRank(rank)
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
	// Binary callers have no evidence identities. They may credit at most the
	// canonical evidence count. The harness uses graded, identity-aware gains.
	hits = append([]bool(nil), hits...)
	credited := 0
	for i, hit := range hits {
		if hit {
			credited++
			if credited > numRelevant {
				hits[i] = false
			}
		}
	}
	ideal := IDCG(numRelevant, k)
	if ideal == 0 {
		return 0
	}
	return DCG(hits) / ideal
}

// gradedNDCG accepts one gain per rank and canonical evidence gains for IDCG.
// Each evidence identity must be credited at most once by the caller.
func gradedNDCG(gains, evidence []float64, k int) float64 {
	if k <= 0 || len(evidence) == 0 {
		return 0
	}
	ideal := append([]float64(nil), evidence...)
	sort.Sort(sort.Reverse(sort.Float64Slice(ideal)))
	var dcg, idcg float64
	for i, gain := range gains {
		if i >= k {
			break
		}
		dcg += gain / math.Log2(float64(i+2))
	}
	for i, gain := range ideal {
		if i >= k {
			break
		}
		idcg += gain / math.Log2(float64(i+2))
	}
	if idcg == 0 {
		return 0
	}
	return dcg / idcg
}

func distribution(samples []float64) Distribution {
	return Distribution{Samples: samples, Min: Percentile(samples, 0), Median: Percentile(samples, 50), Max: Percentile(samples, 100)}
}

func metricDistributions(values []QualityMetrics) map[string]Distribution {
	metrics := map[string][]float64{"hit_rate_at_k": {}, "recall_at_k": {}, "mrr_at_10": {}, "ndcg_at_k": {}, "distractor_hit_rate_at_k": {}}
	for _, value := range values {
		metrics["hit_rate_at_k"] = append(metrics["hit_rate_at_k"], value.HitRateAtK)
		metrics["recall_at_k"] = append(metrics["recall_at_k"], value.RecallAtK)
		metrics["mrr_at_10"] = append(metrics["mrr_at_10"], value.MRRAt10)
		metrics["ndcg_at_k"] = append(metrics["ndcg_at_k"], value.NDCGAtK)
		metrics["distractor_hit_rate_at_k"] = append(metrics["distractor_hit_rate_at_k"], value.DistractorHitRateAtK)
	}
	result := make(map[string]Distribution, len(metrics))
	for name, samples := range metrics {
		result[name] = distribution(samples)
	}
	return result
}

func medianMetrics(d map[string]Distribution) QualityMetrics {
	return QualityMetrics{HitRateAtK: d["hit_rate_at_k"].Median, RecallAtK: d["recall_at_k"].Median, MRRAt10: d["mrr_at_10"].Median, NDCGAtK: d["ndcg_at_k"].Median, DistractorHitRateAtK: d["distractor_hit_rate_at_k"].Median}
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
	sort.Float64s(sorted)
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
