package pkb

import (
	"context"
	"math"
	"sync"
)

// EvalLogger is the logging surface required by runEval.
type EvalLogger interface {
	Logf(format string, args ...any)
	Errorf(format string, args ...any)
}

// EvalCase is one query in the evaluation set.
type EvalCase struct {
	Query string `json:"query"`
}

// EvalMetrics holds IR metrics for one eval run.
type EvalMetrics struct {
	MRR       float64
	HitRate   float64 // Success@K: fraction of queries with ≥1 relevant result
	NDCG      float64
	Precision float64
	Recall    float64 // Recall@K: fraction of all relevant docs retrieved; 0 if judge has no total counts
	MAP       float64 // Mean Average Precision: AUC over precision-recall curve
}

// RelevanceCounter is an optional extension of RelevanceJudge.
// When implemented, RunEval uses TotalRelevant to compute Recall@K and MAP.
type RelevanceCounter interface {
	TotalRelevant(query string) int
}

// RunEval executes all queries concurrently (up to evalWorkers goroutines) and
// returns aggregated IR metrics. candidateMul controls oversampling when reranking.
const evalWorkers = 8

func RunEval(
	ctx context.Context,
	cases []EvalCase,
	embedder Embedder,
	store Store,
	reranker Reranker,
	judge RelevanceJudge,
	topK int,
	candidateMul int,
	hydeSearcher *HyDESearcher,
	log EvalLogger,
) EvalMetrics {
	if candidateMul <= 0 {
		candidateMul = 3
	}

	counter, hasCounter := judge.(RelevanceCounter)

	type result struct {
		reciprocalRank float64
		hit            bool
		precisionAtK   float64
		ndcgAtK        float64
		recallAtK      float64
		avgPrecision   float64
	}

	results := make([]result, len(cases))
	sem := make(chan struct{}, evalWorkers)
	var wg sync.WaitGroup

	for i, tc := range cases {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, tc EvalCase) {
			defer wg.Done()
			defer func() { <-sem }()

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
					return
				}
				hits, err = store.HybridSearch(ctx, vec, tc.Query, fetchK, nil)
				if err != nil {
					log.Errorf("search %q: %v", tc.Query, err)
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

			var firstRank, relevantCount int
			dcg := 0.0
			apSum := 0.0 // running sum for Average Precision
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
					// precision at this rank * binary relevance = P@r for MAP
					apSum += float64(relevantCount) / float64(rank+1)
				}
			}

			res := result{precisionAtK: float64(relevantCount) / float64(topK)}
			if firstRank > 0 {
				res.reciprocalRank = 1.0 / float64(firstRank)
				res.hit = true
			}
			idcg := 0.0
			for i := 0; i < relevantCount && i < topK; i++ {
				idcg += 1.0 / math.Log2(float64(i+2))
			}
			if idcg > 0 {
				res.ndcgAtK = dcg / idcg
			}
			if hasCounter {
				if total := counter.TotalRelevant(tc.Query); total > 0 {
					res.recallAtK = float64(relevantCount) / float64(total)
					res.avgPrecision = apSum / float64(total)
				}
			} else if relevantCount > 0 {
				// no total known: AP normalized by found relevant (optimistic but comparable across runs)
				res.avgPrecision = apSum / float64(relevantCount)
			}
			results[i] = res
		}(i, tc)
	}
	wg.Wait()

	var mrr, hitRate, ndcg, precision, recall, mapScore float64
	for _, r := range results {
		mrr += r.reciprocalRank
		if r.hit {
			hitRate++
		}
		ndcg += r.ndcgAtK
		precision += r.precisionAtK
		recall += r.recallAtK
		mapScore += r.avgPrecision
	}
	n := float64(len(cases))
	return EvalMetrics{
		MRR:       mrr / n,
		HitRate:   hitRate / n,
		NDCG:      ndcg / n,
		Precision: precision / n,
		Recall:    recall / n,
		MAP:       mapScore / n,
	}
}
