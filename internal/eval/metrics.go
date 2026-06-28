package eval

import (
	"math"
	"math/rand"
	"sort"
)

// ---------------------------------------------------------------------------
// Graded relevance helpers
// ---------------------------------------------------------------------------
//
// All metrics use graded relevance: map[string]int where 0 = not relevant,
// 1 = marginally, 2 = relevant, 3 = highly (Järvelin & Kekäläinen 2002).
// Binary relevance is the special case where every relevant item has grade 1.
// An item is "relevant" iff its grade > 0.

func isRelevant(graded map[string]int, id string) bool {
	return graded[id] > 0
}

func countRelevant(graded map[string]int) int {
	n := 0
	for _, g := range graded {
		if g > 0 {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// Binary-friendly metrics (work with graded relevance; grade > 0 = relevant)
// ---------------------------------------------------------------------------

// RecallAtK returns |unique relevant items ∩ retrieved[:k]| / |relevant|.
// When the ranked list contains duplicate identifiers (e.g. multiple chunks from
// the same file at chunk-level granularity), each identifier is counted once.
func RecallAtK(retrieved []string, graded map[string]int, k int) float64 {
	r := countRelevant(graded)
	if r == 0 || k <= 0 {
		return 0
	}
	limit := k
	if limit > len(retrieved) {
		limit = len(retrieved)
	}
	seen := make(map[string]bool)
	for i := 0; i < limit; i++ {
		if isRelevant(graded, retrieved[i]) {
			seen[retrieved[i]] = true
		}
	}
	return float64(len(seen)) / float64(r)
}

// PrecisionAtK returns |relevant ∩ retrieved[:k]| / k.
// Denominator is k (standard IR): systems returning fewer than k results
// are penalized for the missing slots.
func PrecisionAtK(retrieved []string, graded map[string]int, k int) float64 {
	if k <= 0 {
		return 0
	}
	limit := k
	if limit > len(retrieved) {
		limit = len(retrieved)
	}
	hits := 0
	for i := 0; i < limit; i++ {
		if isRelevant(graded, retrieved[i]) {
			hits++
		}
	}
	return float64(hits) / float64(k)
}

// ReciprocalRank returns 1/rank of the first relevant item, or 0 if none.
func ReciprocalRank(retrieved []string, graded map[string]int) float64 {
	for i, id := range retrieved {
		if isRelevant(graded, id) {
			return 1.0 / float64(i+1)
		}
	}
	return 0
}

// SuccessAtK returns 1.0 if any relevant item appears in top-k, 0 otherwise.
func SuccessAtK(retrieved []string, graded map[string]int, k int) float64 {
	limit := k
	if limit > len(retrieved) {
		limit = len(retrieved)
	}
	for i := 0; i < limit; i++ {
		if isRelevant(graded, retrieved[i]) {
			return 1.0
		}
	}
	return 0
}

// AveragePrecision is the area under the precision-recall curve for one query.
// AP = (1/R) * Σ_{k: rel(k)=1} Precision@k, where R = total relevant items.
func AveragePrecision(retrieved []string, graded map[string]int) float64 {
	r := countRelevant(graded)
	if r == 0 {
		return 0
	}
	hits := 0
	var sum float64
	for i, id := range retrieved {
		if isRelevant(graded, id) {
			hits++
			sum += float64(hits) / float64(i+1)
		}
	}
	return sum / float64(r)
}

// ---------------------------------------------------------------------------
// NDCG — graded relevance ranking quality (Järvelin & Kekäläinen 2002)
// ---------------------------------------------------------------------------

// DCG computes Discounted Cumulative Gain using the original formula:
// DCG_p = Σ_{i=1}^{p} rel_i / log2(i+1)
// (Järvelin & Kekäläinen 2002, ACM TOIS 20(4):422–446)
func DCG(grades []int) float64 {
	var dcg float64
	for i, g := range grades {
		if i == 0 {
			dcg += float64(g)
		} else {
			dcg += float64(g) / math.Log2(float64(i+2))
		}
	}
	return dcg
}

// DCGExp computes DCG using the exponential formula:
// DCG_p = Σ_{i=1}^{p} (2^rel_i − 1) / log2(i+1)
// (Burges et al. 2005; used by BEIR, Kaggle, and major web search engines)
func DCGExp(grades []int) float64 {
	var dcg float64
	for i, g := range grades {
		gain := math.Exp2(float64(g)) - 1
		if i == 0 {
			dcg += gain
		} else {
			dcg += gain / math.Log2(float64(i+2))
		}
	}
	return dcg
}

// NDCGAtK computes Normalized DCG@k using the original linear-gain formula.
// Returns DCG@k / IDCG@k where IDCG is the DCG of the ideal ranking
// (all known grades sorted descending, truncated to k).
func NDCGAtK(retrieved []string, graded map[string]int, k int) float64 {
	return ndcgAtK(retrieved, graded, k, DCG)
}

// NDCGAtKExp computes NDCG@k using the exponential-gain formula (BEIR-style).
func NDCGAtKExp(retrieved []string, graded map[string]int, k int) float64 {
	return ndcgAtK(retrieved, graded, k, DCGExp)
}

func ndcgAtK(retrieved []string, graded map[string]int, k int, dcgFn func([]int) float64) float64 {
	if k <= 0 || len(graded) == 0 {
		return 0
	}
	limit := k
	if limit > len(retrieved) {
		limit = len(retrieved)
	}

	// Actual DCG: grades in retrieved order
	actualGrades := make([]int, limit)
	for i := 0; i < limit; i++ {
		actualGrades[i] = graded[retrieved[i]]
	}
	dcg := dcgFn(actualGrades)

	// Ideal DCG: all known grades sorted descending, truncated to k
	allGrades := make([]int, 0, len(graded))
	for _, g := range graded {
		allGrades = append(allGrades, g)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(allGrades)))
	if k > len(allGrades) {
		k = len(allGrades)
	}
	idcg := dcgFn(allGrades[:k])

	if idcg == 0 {
		return 0
	}
	return dcg / idcg
}

// ---------------------------------------------------------------------------
// Bootstrap confidence interval
// ---------------------------------------------------------------------------

// BootstrapCI computes a 95% confidence interval for the mean of values
// using B bootstrap resamples. Returns (mean, lower, upper).
// Uses a fixed seed for reproducibility.
func BootstrapCI(values []float64, B int, seed int64) (mean, low, high float64) {
	if len(values) == 0 {
		return 0, 0, 0
	}
	if B <= 0 {
		B = 1000
	}
	rng := rand.New(rand.NewSource(seed))

	// Observed mean
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	mean = sum / float64(len(values))

	// Bootstrap resamples
	bootMeans := make([]float64, B)
	for b := 0; b < B; b++ {
		s := 0.0
		for j := 0; j < len(values); j++ {
			s += values[rng.Intn(len(values))]
		}
		bootMeans[b] = s / float64(len(values))
	}
	sort.Float64s(bootMeans)

	lowIdx := int(math.Floor(0.025 * float64(B)))
	highIdx := int(math.Ceil(0.975 * float64(B))) - 1
	if lowIdx < 0 {
		lowIdx = 0
	}
	if highIdx >= B {
		highIdx = B - 1
	}
	return mean, bootMeans[lowIdx], bootMeans[highIdx]
}

// ---------------------------------------------------------------------------
// Aggregation
// ---------------------------------------------------------------------------

// Report aggregates per-query metrics over a run.
type Report struct {
	NumQueries   int         `json:"num_queries"`
	RecallAt5    float64     `json:"recall_at_5"`
	RecallAt10   float64     `json:"recall_at_10"`
	PrecisionAt5 float64     `json:"precision_at_5"`
	MRR          float64     `json:"mrr"`
	NDCG10       float64     `json:"ndcg_at_10"`
	NDCG10Exp    float64     `json:"ndcg_at_10_exp"`
	MAP          float64     `json:"map"`
	SuccessAt5   float64     `json:"success_at_5"`

	// 95% bootstrap confidence intervals (lower, upper) for each aggregate.
	RecallAt5CI    [2]float64 `json:"recall_at_5_ci"`
	RecallAt10CI   [2]float64 `json:"recall_at_10_ci"`
	NDCG10CI       [2]float64 `json:"ndcg_at_10_ci"`
	MAPCI          [2]float64 `json:"map_ci"`

	PerQuery []QueryReport `json:"-"`
}

// RetrievedFile is a single retrieved item with its relevance score.
type RetrievedFile struct {
	Path  string  `json:"path"`
	Score float32 `json:"score"`
}

// QueryReport is the per-query breakdown for diagnosis.
type QueryReport struct {
	Query          string
	ExpectedFiles  []string
	Retrieved      []string
	RetrievedFiles []RetrievedFile `json:"retrieved_files,omitempty"`
	LatencyMs      int64           `json:"latency_ms"`
	RecallAt5      float64
	RecallAt10     float64
	PrecisionAt5   float64
	RR             float64
	NDCG10         float64
	NDCG10Exp      float64
	AP             float64
	SuccessAt5     float64
}

// Aggregate builds a Report from per-query retrieved/graded pairs.
// retrievedPerQuery[i] is the ranked list of identifiers for query i.
// gradedPerQuery[i] maps identifier → relevance grade (0=irrelevant, 1-3=graded).
// queries are aligned by index.
func Aggregate(retrievedPerQuery [][]string, gradedPerQuery []map[string]int, queries []string) Report {
	n := len(queries)
	if len(retrievedPerQuery) != n || len(gradedPerQuery) != n {
		panic("eval.Aggregate: mismatched slice lengths")
	}
	rep := Report{NumQueries: n}

	r5Vals := make([]float64, n)
	r10Vals := make([]float64, n)
	p5Vals := make([]float64, n)
	rrVals := make([]float64, n)
	ndcgVals := make([]float64, n)
	ndcgExpVals := make([]float64, n)
	apVals := make([]float64, n)
	successVals := make([]float64, n)

	for i, q := range queries {
		retrieved := retrievedPerQuery[i]
		graded := gradedPerQuery[i]

		r5 := RecallAtK(retrieved, graded, 5)
		r10 := RecallAtK(retrieved, graded, 10)
		p5 := PrecisionAtK(retrieved, graded, 5)
		rr := ReciprocalRank(retrieved, graded)
		ndcg := NDCGAtK(retrieved, graded, 10)
		ndcgExp := NDCGAtKExp(retrieved, graded, 10)
		ap := AveragePrecision(retrieved, graded)
		succ := SuccessAtK(retrieved, graded, 5)

		r5Vals[i] = r5
		r10Vals[i] = r10
		p5Vals[i] = p5
		rrVals[i] = rr
		ndcgVals[i] = ndcg
		ndcgExpVals[i] = ndcgExp
		apVals[i] = ap
		successVals[i] = succ

		expected := make([]string, 0, len(graded))
		for f, g := range graded {
			if g > 0 {
				expected = append(expected, f)
			}
		}
		sort.Strings(expected)

		rep.PerQuery = append(rep.PerQuery, QueryReport{
			Query:         q,
			ExpectedFiles: expected,
			Retrieved:     retrieved,
			RecallAt5:     r5,
			RecallAt10:    r10,
			PrecisionAt5:  p5,
			RR:            rr,
			NDCG10:        ndcg,
			NDCG10Exp:     ndcgExp,
			AP:            ap,
			SuccessAt5:    succ,
		})
	}

	if n > 0 {
		rep.RecallAt5 = mean(r5Vals)
		rep.RecallAt10 = mean(r10Vals)
		rep.PrecisionAt5 = mean(p5Vals)
		rep.MRR = mean(rrVals)
		rep.NDCG10 = mean(ndcgVals)
		rep.NDCG10Exp = mean(ndcgExpVals)
		rep.MAP = mean(apVals)
		rep.SuccessAt5 = mean(successVals)

		_, rep.RecallAt5CI[0], rep.RecallAt5CI[1] = BootstrapCI(r5Vals, 1000, 42)
		_, rep.RecallAt10CI[0], rep.RecallAt10CI[1] = BootstrapCI(r10Vals, 1000, 42)
		_, rep.NDCG10CI[0], rep.NDCG10CI[1] = BootstrapCI(ndcgVals, 1000, 42)
		_, rep.MAPCI[0], rep.MAPCI[1] = BootstrapCI(apVals, 1000, 42)
	}
	return rep
}

func mean(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	s := 0.0
	for _, v := range vals {
		s += v
	}
	return s / float64(len(vals))
}
