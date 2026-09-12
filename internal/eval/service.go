// Package eval provides a repeatable Retrieval-quality evaluation Module.
// It deliberately uses only the current search Interface and standard Go
// serialization/timing primitives; the running server remains unchanged.
package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"nadir/internal/search"

	"go.uber.org/zap"
)

// DependenciesConfig groups the Retrieval seam exercised by the harness.
type DependenciesConfig struct {
	Searcher search.Retriever
	Log      *zap.Logger
}

// Harness evaluates a GoldenSet through one configured Retrieval Adapter.
type Harness struct {
	searcher search.Retriever
	log      *zap.Logger
}

func NewDependencies(cfg DependenciesConfig) *Harness {
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}
	return &Harness{searcher: cfg.Searcher, log: log}
}

// Run evaluates every golden query. Each query is executed runs times;
// ranking comes from the final run while latency is summarized by its median.
// Cache use is explicitly disabled so a report measures the Retrieval path.
func (h *Harness) Run(ctx context.Context, golden *GoldenSet, topK, runs int) (*Report, error) {
	if h == nil || h.searcher == nil {
		return nil, fmt.Errorf("evaluation searcher is required")
	}
	if golden == nil || len(golden.Queries) == 0 {
		return nil, fmt.Errorf("evaluation golden set is required")
	}
	if topK <= 0 {
		return nil, fmt.Errorf("evaluation top_k must be greater than zero")
	}
	if runs < 1 {
		runs = 1
	}

	report := &Report{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		TopK:      topK,
		PerQuery:  make([]QueryResult, 0, len(golden.Queries)),
	}
	for _, goldenQuery := range golden.Queries {
		result := QueryResult{
			ID:          goldenQuery.ID,
			Query:       goldenQuery.Query,
			NumRelevant: len(goldenQuery.Relevant),
			Latencies:   make([]float64, 0, runs),
		}
		var chunks []search.Chunk
		for run := 0; run < runs; run++ {
			started := time.Now()
			searchResult, err := h.searcher.Query(ctx, search.Request{
				Query:     goldenQuery.Query,
				TopK:      topK,
				SkipCache: true,
			})
			latency := float64(time.Since(started).Microseconds()) / 1000
			if err != nil {
				return nil, fmt.Errorf("query %q: %w", goldenQuery.ID, err)
			}
			result.Latencies = append(result.Latencies, latency)
			chunks = searchResult.Chunks
		}
		result.LatencyMS = Percentile(result.Latencies, 50)
		result.Hits, result.FirstHitRank, result.RelevantFound = scoreResults(chunks, goldenQuery.Relevant)
		report.PerQuery = append(report.PerQuery, result)
		h.log.Debug("evaluation query completed",
			zap.String("id", goldenQuery.ID),
			zap.Int("first_hit_rank", result.FirstHitRank),
			zap.Int("relevant_found", result.RelevantFound))
	}

	report.Aggregate = aggregate(report.PerQuery, topK)
	return report, nil
}

func scoreResults(chunks []search.Chunk, relevant []RelevantChunk) ([]bool, int, int) {
	hits := make([]bool, len(chunks))
	found := make(map[int]struct{}, len(relevant))
	firstRank := 0
	for position, chunk := range chunks {
		matched := MatchedRelevant(chunk, relevant)
		for _, index := range matched {
			found[index] = struct{}{}
		}
		hits[position] = len(matched) > 0
		if firstRank == 0 && hits[position] {
			firstRank = position + 1
		}
	}
	return hits, firstRank, len(found)
}

func aggregate(results []QueryResult, topK int) Aggregate {
	aggregate := Aggregate{Queries: len(results), TopK: topK}
	firstRanks := make([]int, 0, len(results))
	found := make([]int, 0, len(results))
	total := make([]int, 0, len(results))
	hitLists := make([][]bool, 0, len(results))
	latencies := make([]float64, 0, len(results))
	for _, result := range results {
		firstRanks = append(firstRanks, result.FirstHitRank)
		found = append(found, result.RelevantFound)
		total = append(total, result.NumRelevant)
		hitLists = append(hitLists, result.Hits)
		latencies = append(latencies, result.LatencyMS)
	}
	aggregate.HitRateAtK = HitRate(firstRanks)
	aggregate.RecallAtK = Recall(found, total)
	aggregate.MRRAt10 = MRR(firstRanks)
	aggregate.NDCGAtK = MeanNDCG(hitLists, total, topK)
	aggregate.P50LatMS = Percentile(latencies, 50)
	aggregate.P95LatMS = Percentile(latencies, 95)
	return aggregate
}

// WriteReport persists a report as stable, human-readable JSON.
func WriteReport(path string, report *Report) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}
