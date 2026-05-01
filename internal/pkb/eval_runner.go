package pkb

import (
	"context"
	"math"
	"os"
	"strconv"
	"sync"
)

// EvalLogger is the logging surface required by runEval.
type EvalLogger interface {
	Logf(format string, args ...any)
	Errorf(format string, args ...any)
}

// EvalCase is one query in the evaluation set.
type EvalCase struct {
	Query      string   `json:"query"`
	Category   string   `json:"category"`   // e.g. "golang" | "system-design" | "math"
	Difficulty string   `json:"difficulty"` // "easy" | "medium" | "hard"
	Tags       []string `json:"tags"`
}

// QueryResult holds per-query retrieval metrics for one search call.
// Enables failure analysis: sort by NDCG ascending to surface worst queries.
type QueryResult struct {
	Query            string
	Category         string
	Difficulty       string
	Hit              bool
	FirstRank        int     // 1-based rank of first relevant result; 0 if miss
	ReciprocalRank   float64
	NDCG             float64
	Precision        float64
	Recall           float64
	AvgPrecision     float64
	ContextRelevance float64
	TopChunks        []ScoredChunk // retrieved chunks for manual inspection
}

// EvalMetrics holds aggregated IR metrics for one eval run.
type EvalMetrics struct {
	MRR              float64
	HitRate          float64 // Success@K: fraction of queries with ≥1 relevant result
	NDCG             float64
	Precision        float64
	Recall           float64 // Recall@K: fraction of all relevant docs retrieved; 0 if judge has no total counts
	MAP              float64 // Mean Average Precision: AUC over precision-recall curve
	ContextRelevance float64 // RAGAS-style avg chunk relevance score 0–1; 0 if judge doesn't implement ContextScorer
}

// RelevanceCounter is an optional extension of RelevanceJudge.
// When implemented, RunEval uses TotalRelevant to compute Recall@K and MAP.
type RelevanceCounter interface {
	TotalRelevant(query string) int
}

// RunEval executes all queries concurrently (up to evalWorkers goroutines) and
// returns per-query results and aggregated IR metrics.
// candidateMul controls oversampling when reranking.
const defaultEvalWorkers = 8

func evalWorkerCount() int {
	if s := os.Getenv("EVAL_WORKERS"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return n
		}
	}
	return defaultEvalWorkers
}

func RunEval(
	ctx context.Context,
	cases []EvalCase,
	embedder Embedder,
	store Store,
	reranker Reranker,
	judge RelevanceJudge,
	topK int,
	candidateMul int,
	hydeSearcher HyDESearchInterface,
	log EvalLogger,
) ([]QueryResult, EvalMetrics) {
	if candidateMul <= 0 {
		candidateMul = 3
	}

	counter, hasCounter := judge.(RelevanceCounter)
	scorer, hasScorer := judge.(ContextScorer)

	queryResults := make([]QueryResult, len(cases))
	sem := make(chan struct{}, evalWorkerCount())
	var wg sync.WaitGroup

	for i, tc := range cases {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, tc EvalCase) {
			defer wg.Done()
			defer func() { <-sem }()

			qr := QueryResult{
				Query:      tc.Query,
				Category:   tc.Category,
				Difficulty: tc.Difficulty,
			}

			fetchK := topK
			if reranker != nil {
				fetchK = topK * candidateMul
			}

			var hits []ScoredChunk
			var err error
			if hydeSearcher != nil {
				hits, err = hydeSearcher.Search(ctx, tc.Query, fetchK)
				if err != nil {
					log.Logf("hyde search %q failed, falling back: %v", tc.Query, err)
					hits, err = nil, nil
				}
			}
			if hits == nil {
				var vec []float32
				vec, err = embedder.Embed(ctx, tc.Query)
				if err != nil {
					log.Errorf("embed query %q: %v", tc.Query, err)
					queryResults[i] = qr
					return
				}
				hits, err = store.HybridSearch(ctx, vec, tc.Query, fetchK, nil)
				if err != nil {
					log.Errorf("search %q: %v", tc.Query, err)
					queryResults[i] = qr
					return
				}
			}

			if reranker != nil {
				hits, err = reranker.Rerank(ctx, tc.Query, hits)
				if err != nil {
					log.Logf("warn: rerank %q: %v", tc.Query, err)
				}
				if len(hits) > topK {
					hits = hits[:topK]
				}
			}

			qr.TopChunks = hits

			var firstRank, relevantCount int
			dcg := 0.0
			apSum := 0.0
			ctxScoreSum := 0.0
			for rank, r := range hits {
				rel, err := judge.IsRelevant(ctx, tc.Query, r)
				if err != nil {
					log.Logf("warn: judge %q chunk %s:%d: %v", tc.Query, r.FilePath, r.LineStart, err)
				}
				if rel {
					if firstRank == 0 {
						firstRank = rank + 1
					}
					relevantCount++
					dcg += 1.0 / math.Log2(float64(rank+2))
					apSum += float64(relevantCount) / float64(rank+1)
				}
				if hasScorer {
					s, err := scorer.ScoreContext(ctx, tc.Query, r)
					if err != nil {
						log.Logf("warn: context score %q chunk %s:%d: %v", tc.Query, r.FilePath, r.LineStart, err)
					} else {
						ctxScoreSum += s
					}
				}
			}

			qr.Precision = float64(relevantCount) / float64(topK)
			if hasScorer && len(hits) > 0 {
				qr.ContextRelevance = ctxScoreSum / float64(len(hits))
			}
			if firstRank > 0 {
				qr.Hit = true
				qr.FirstRank = firstRank
				qr.ReciprocalRank = 1.0 / float64(firstRank)
			}
			idcg := 0.0
			for j := 0; j < relevantCount && j < topK; j++ {
				idcg += 1.0 / math.Log2(float64(j+2))
			}
			if idcg > 0 {
				qr.NDCG = dcg / idcg
			}
			if hasCounter {
				if total := counter.TotalRelevant(tc.Query); total > 0 {
					qr.Recall = float64(relevantCount) / float64(total)
					qr.AvgPrecision = apSum / float64(total)
				}
			} else if relevantCount > 0 {
				qr.AvgPrecision = apSum / float64(relevantCount)
			}
			queryResults[i] = qr
		}(i, tc)
	}
	wg.Wait()

	var mrr, hitRate, ndcg, precision, recall, mapScore, ctxRelevance float64
	for _, r := range queryResults {
		mrr += r.ReciprocalRank
		if r.Hit {
			hitRate++
		}
		ndcg += r.NDCG
		precision += r.Precision
		recall += r.Recall
		mapScore += r.AvgPrecision
		ctxRelevance += r.ContextRelevance
	}
	n := float64(len(cases))
	metrics := EvalMetrics{
		MRR:              mrr / n,
		HitRate:          hitRate / n,
		NDCG:             ndcg / n,
		Precision:        precision / n,
		Recall:           recall / n,
		MAP:              mapScore / n,
		ContextRelevance: ctxRelevance / n,
	}
	return queryResults, metrics
}
