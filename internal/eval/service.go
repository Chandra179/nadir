// Package evaluation provides repeatable Retrieval-quality measurement.
package evaluation

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"nadir/internal/core/retrieval/search"
	"slices"
	"sort"
	"strings"
	"time"
)

// Run records every ranking. Quality is the median of dataset-level metrics
// across runs; latency percentiles cover every actual request, at the recorded
// RetrievalDepth. Requests bypass the semantic cache.
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
	depth := max(topK, 10)
	report := &Report{ReportSchemaVersion: 2, Timestamp: time.Now().UTC().Format(time.RFC3339), Dataset: golden.Metadata.Dataset, DatasetSchemaVersion: golden.SchemaVersion, DatasetReleaseGate: golden.Metadata.ReleaseGate, TopK: topK, RetrievalDepth: depth, Runs: runs, Aggregation: "quality=median of dataset run metrics; latency=pooled request samples", PerQuery: make([]QueryResult, 0, len(golden.Queries))}
	for _, query := range golden.Queries {
		relevant := canonicalEvidence(query.Relevant)
		result := QueryResult{ID: query.ID, Query: query.Query, Type: query.Type, FaithfulnessLabel: query.FaithfulnessLabel, NumRelevant: len(relevant), Latencies: make([]float64, 0, runs), Runs: make([]QueryRunResult, 0, runs)}
		metrics := make([]QualityMetrics, 0, runs)
		for run := 1; run <= runs; run++ {
			started := time.Now()
			found, err := h.searcher.Query(ctx, search.Request{Query: query.Query, TopK: depth, SkipCache: true, QueryType: search.QueryType(query.Type)})
			latency := float64(time.Since(started).Microseconds()) / 1000
			if err != nil {
				report.PerQuery = append(report.PerQuery, result)
				return report, fmt.Errorf("query %q run %d: %w", query.ID, run, err)
			}
			observed := scoreRun(found.Chunks, relevant, query.Distractors, topK)
			observed.Run = run
			observed.LatencyMS = latency
			observed.Rerank = found.Rerank
			result.Runs = append(result.Runs, observed)
			result.Latencies = append(result.Latencies, latency)
			metrics = append(metrics, observed.Metrics)
		}
		result.Distributions = metricDistributions(metrics)
		result.Metrics = medianMetrics(result.Distributions)
		result.LatencyMS = Percentile(result.Latencies, 50)
		// Preserve one concrete ranking for legacy consumers without presenting the
		// final run as an aggregate. The complete rankings remain in Runs.
		ordered := append([]QueryRunResult(nil), result.Runs...)
		sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Metrics.NDCGAtK < ordered[j].Metrics.NDCGAtK })
		representative := ordered[(len(ordered)-1)/2]
		result.RepresentativeRun = representative.Run
		result.Hits = representative.Hits
		result.FirstHitRank = representative.FirstHitRank
		result.RelevantFound = representative.RelevantFound
		result.DistractorHits = representative.DistractorHits
		result.Rerank = representative.Rerank
		report.PerQuery = append(report.PerQuery, result)
		h.log.Debug("evaluation query completed", slog.String("id", query.ID), slog.Int("runs", runs))
	}
	runMetrics := make([]QualityMetrics, 0, runs)
	all := make([]QueryResult, 0, len(golden.Queries)*runs)
	for run := 0; run < runs; run++ {
		observations := make([]QueryResult, 0, len(golden.Queries))
		for _, result := range report.PerQuery {
			observed := result.Runs[run]
			one := QueryResult{NumRelevant: result.NumRelevant, Metrics: observed.Metrics, LatencyMS: observed.LatencyMS, Rerank: observed.Rerank}
			observations = append(observations, one)
			all = append(all, one)
		}
		a := aggregate(observations, topK)
		report.RunAggregates = append(report.RunAggregates, RunAggregate{Run: run + 1, Aggregate: a})
		runMetrics = append(runMetrics, qualityFromAggregate(a))
	}
	report.Distributions = metricDistributions(runMetrics)
	report.Aggregate = aggregate(all, topK)
	report.Aggregate.Queries = len(golden.Queries)
	report.Aggregate.AnswerableQueries /= runs
	report.Aggregate.AbstentionQueries /= runs
	medians := medianMetrics(report.Distributions)
	report.Aggregate.HitRateAtK = medians.HitRateAtK
	report.Aggregate.RecallAtK = medians.RecallAtK
	report.Aggregate.MRRAt10 = medians.MRRAt10
	report.Aggregate.NDCGAtK = medians.NDCGAtK
	report.Aggregate.DistractorHitRateAtK = medians.DistractorHitRateAtK
	return report, nil
}

// Canonical identity is normalized file+contains. Repeated annotations collapse
// to one evidence item, retaining the strongest grade (omitted grade means 1).
func canonicalEvidence(entries []RelevantChunk) []RelevantChunk {
	out := make([]RelevantChunk, 0, len(entries))
	indices := map[string]int{}
	for _, entry := range entries {
		key := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(entry.File, "\\", "/"))) + "\x00" + strings.ToLower(strings.TrimSpace(entry.Contains))
		if entry.Grade == 0 {
			entry.Grade = 1
		}
		if index, ok := indices[key]; ok {
			if entry.Grade > out[index].Grade {
				out[index].Grade = entry.Grade
			}
			continue
		}
		indices[key] = len(out)
		out = append(out, entry)
	}
	return out
}

func scoreRun(chunks []search.Chunk, relevant, distractors []RelevantChunk, k int) QueryRunResult {
	out := QueryRunResult{Ranking: make([]RankedChunk, 0, len(chunks)), Hits: make([]bool, min(k, len(chunks)))}
	found := map[int]bool{}
	credited := map[int]bool{}
	gains := make([]float64, 0, len(chunks))
	ideal := make([]float64, len(relevant))
	for i, evidence := range relevant {
		ideal[i] = math.Pow(2, float64(evidence.Grade)) - 1
	}
	for position, chunk := range chunks {
		matches := MatchedRelevant(chunk, relevant)
		gain := 0.0
		selected := -1
		for _, index := range matches {
			if !credited[index] && ideal[index] > gain {
				gain = ideal[index]
				selected = index
			}
		}
		if selected >= 0 {
			credited[selected] = true
		}
		gains = append(gains, gain)
		out.Ranking = append(out.Ranking, RankedChunk{Rank: position + 1, FilePath: chunk.FilePath, SourceSHA: chunk.SourceSHA, LineStart: chunk.LineStart, ChunkIndex: chunk.ChunkIndex, Score: chunk.Score, Evidence: matches, Gain: gain})
		if position < 10 && out.FirstHitRank10 == 0 && len(matches) > 0 {
			out.FirstHitRank10 = position + 1
		}
		if position >= k {
			continue
		}
		for _, index := range matches {
			found[index] = true
		}
		out.Hits[position] = len(matches) > 0
		if out.FirstHitRank == 0 && len(matches) > 0 {
			out.FirstHitRank = position + 1
		}
		if len(matches) == 0 && len(MatchedDistractors(chunk, distractors)) > 0 {
			out.DistractorHits++
		}
	}
	out.RelevantFound = len(found)
	out.Metrics.MRRAt10 = ReciprocalRankAt(out.FirstHitRank10, 10)
	out.Metrics.NDCGAtK = gradedNDCG(gains, ideal, k)
	if out.FirstHitRank > 0 {
		out.Metrics.HitRateAtK = 1
	}
	if len(relevant) > 0 {
		out.Metrics.RecallAtK = float64(out.RelevantFound) / float64(len(relevant))
	}
	if out.DistractorHits > 0 {
		out.Metrics.DistractorHitRateAtK = 1
	}
	return out
}

func scoreResults(chunks []search.Chunk, relevant, distractors []RelevantChunk) ([]bool, int, int, int) {
	result := scoreRun(chunks, canonicalEvidence(relevant), distractors, len(chunks))
	return result.Hits, result.FirstHitRank, result.RelevantFound, result.DistractorHits
}

func qualityFromAggregate(a Aggregate) QualityMetrics {
	return QualityMetrics{HitRateAtK: a.HitRateAtK, RecallAtK: a.RecallAtK, MRRAt10: a.MRRAt10, NDCGAtK: a.NDCGAtK, DistractorHitRateAtK: a.DistractorHitRateAtK}
}

func aggregate(results []QueryResult, topK int) Aggregate {
	out := Aggregate{Queries: len(results), Requests: len(results), TopK: topK, RerankReasons: map[string]int{}}
	latencies := []float64{}
	rerankLatencies := []float64{}
	for _, result := range results {
		latencies = append(latencies, result.LatencyMS)
		if result.NumRelevant > 0 {
			out.AnswerableQueries++
			out.HitRateAtK += result.Metrics.HitRateAtK
			out.RecallAtK += result.Metrics.RecallAtK
			out.MRRAt10 += result.Metrics.MRRAt10
			out.NDCGAtK += result.Metrics.NDCGAtK
		} else {
			out.AbstentionQueries++
		}
		out.DistractorHitRateAtK += result.Metrics.DistractorHitRateAtK
		if result.Rerank.Enabled {
			out.RerankReasons[result.Rerank.Reason]++
		}
		if result.Rerank.Attempted {
			out.RerankDependencyCalls++
			out.RerankCandidateTotal += result.Rerank.Candidates
			rerankLatencies = append(rerankLatencies, result.Rerank.LatencyMS)
			if result.Rerank.DependencyErr {
				out.RerankErrors++
			}
		}
	}
	if out.AnswerableQueries > 0 {
		n := float64(out.AnswerableQueries)
		out.HitRateAtK /= n
		out.RecallAtK /= n
		out.MRRAt10 /= n
		out.NDCGAtK /= n
	}
	if len(results) > 0 {
		out.DistractorHitRateAtK /= float64(len(results))
		out.RerankCoverage = float64(out.RerankDependencyCalls) / float64(len(results))
	}
	slices.Sort(latencies)
	slices.Sort(rerankLatencies)
	out.P50LatMS = percentileSorted(latencies, 50)
	out.P95LatMS = percentileSorted(latencies, 95)
	out.RerankP50LatMS = percentileSorted(rerankLatencies, 50)
	out.RerankP95LatMS = percentileSorted(rerankLatencies, 95)
	if len(out.RerankReasons) == 0 {
		out.RerankReasons = nil
	}
	return out
}
