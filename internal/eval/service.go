package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"nadir/internal/store"

	"go.uber.org/zap"
)

// RelevantChunk identifies one golden-relevant chunk: the stored file path
// must end in File (suffix match, so "samples/x.md" and "x.md" both match)
// and its text must contain Contains (case-insensitive substring).
// An empty field is treated as always-matching for that criterion.
type RelevantChunk struct {
	File     string `json:"file"`
	Contains string `json:"contains"`
}

type GoldenQuery struct {
	ID       string          `json:"id"`
	Query    string          `json:"query"`
	Relevant []RelevantChunk `json:"relevant"`
}

type GoldenSet struct {
	Queries []GoldenQuery `json:"queries"`
}

// LoadGoldenSet reads a golden query set from a JSON file.
func LoadGoldenSet(path string) (*GoldenSet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read golden set: %w", err)
	}
	var gs GoldenSet
	if err := json.Unmarshal(data, &gs); err != nil {
		return nil, fmt.Errorf("parse golden set %s: %w", path, err)
	}
	if len(gs.Queries) == 0 {
		return nil, fmt.Errorf("golden set %s has no queries", path)
	}
	for i, q := range gs.Queries {
		if q.Query == "" || len(q.Relevant) == 0 {
			return nil, fmt.Errorf("golden set query #%d (%q) needs a query and at least one relevant entry", i+1, q.ID)
		}
	}
	return &gs, nil
}

// MatchedRelevant returns the indices of rel that chunk c satisfies.
func MatchedRelevant(c store.ScoredChunk, rel []RelevantChunk) []int {
	var matched []int
	hay := strings.ToLower(c.Text + "\n" + c.WindowText)
	fp := strings.ToLower(c.FilePath)
	for i, r := range rel {
		if r.File != "" && !strings.HasSuffix(fp, strings.ToLower(r.File)) {
			continue
		}
		if r.Contains != "" && !strings.Contains(hay, strings.ToLower(r.Contains)) {
			continue
		}
		matched = append(matched, i)
	}
	return matched
}

type QueryResult struct {
	ID            string    `json:"id"`
	Query         string    `json:"query"`
	NumRelevant   int       `json:"num_relevant"`
	Hits          []bool    `json:"hits"`
	FirstHitRank  int       `json:"first_hit_rank"`
	RelevantFound int       `json:"relevant_found"`
	LatencyMS     float64   `json:"latency_ms"`
	Latencies     []float64 `json:"latencies"`
}

type Aggregate struct {
	Queries    int     `json:"queries"`
	TopK       int     `json:"top_k"`
	HitRateAtK float64 `json:"hit_rate_at_k"`
	RecallAtK  float64 `json:"recall_at_k"`
	MRRAt10    float64 `json:"mrr_at_10"`
	NDCGAtK    float64 `json:"ndcg_at_k"`
	P50LatMS   float64 `json:"p50_latency_ms"`
	P95LatMS   float64 `json:"p95_latency_ms"`
}

type Report struct {
	Timestamp string        `json:"timestamp"`
	TopK      int           `json:"top_k"`
	Rerank    bool          `json:"reranker_enabled"`
	PerQuery  []QueryResult `json:"per_query"`
	Aggregate Aggregate     `json:"aggregate"`
}

// Run evaluates every golden query, repeating each runs times and keeping
// the median latency plus the final run's ranking (retrieval is assumed
// deterministic enough that only timing varies between runs).
func (h *Harness) Run(ctx context.Context, gs *GoldenSet, topK, runs int) (*Report, error) {
	if runs < 1 {
		runs = 1
	}
	report := &Report{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		TopK:      topK,
		PerQuery:  make([]QueryResult, 0, len(gs.Queries)),
	}

	for _, gq := range gs.Queries {
		qr := QueryResult{
			ID:          gq.ID,
			Query:       gq.Query,
			NumRelevant: len(gq.Relevant),
			Latencies:   make([]float64, 0, runs),
		}
		var chunks []store.ScoredChunk
		for run := 0; run < runs; run++ {
			start := time.Now()
			res, _, err := h.searcher.Query(ctx, gq.Query, "", topK, nil, true)
			ms := float64(time.Since(start).Microseconds()) / 1000.0
			if err != nil {
				return nil, fmt.Errorf("query %q: %w", gq.ID, err)
			}
			qr.Latencies = append(qr.Latencies, ms)
			chunks = res
		}
		qr.LatencyMS = median(qr.Latencies)

		matchedSet := make(map[int]bool)
		qr.Hits = make([]bool, len(chunks))
		for pos, c := range chunks {
			m := MatchedRelevant(c, gq.Relevant)
			for _, idx := range m {
				matchedSet[idx] = true
			}
			qr.Hits[pos] = len(m) > 0
			if qr.FirstHitRank == 0 && qr.Hits[pos] {
				qr.FirstHitRank = pos + 1
			}
		}
		qr.RelevantFound = len(matchedSet)

		report.PerQuery = append(report.PerQuery, qr)
		h.log.Debug("eval query done",
			zap.String("id", gq.ID),
			zap.Int("first_hit_rank", qr.FirstHitRank),
			zap.Int("relevant_found", qr.RelevantFound))
	}

	report.Rerank = h.rerankerEnabled()
	report.Aggregate = aggregate(report.PerQuery, topK)
	return report, nil
}

func (h *Harness) rerankerEnabled() bool {
	type rerankerProbe interface{ RerankerEnabled() bool }
	if p, ok := h.searcher.(rerankerProbe); ok {
		return p.RerankerEnabled()
	}
	return false
}

func aggregate(perQuery []QueryResult, topK int) Aggregate {
	agg := Aggregate{Queries: len(perQuery), TopK: topK}
	firstRanks := make([]int, 0, len(perQuery))
	found := make([]int, 0, len(perQuery))
	total := make([]int, 0, len(perQuery))
	hitLists := make([][]bool, 0, len(perQuery))
	var latencies []float64
	for _, qr := range perQuery {
		firstRanks = append(firstRanks, qr.FirstHitRank)
		found = append(found, qr.RelevantFound)
		total = append(total, qr.NumRelevant)
		hitLists = append(hitLists, qr.Hits)
		latencies = append(latencies, qr.LatencyMS)
	}
	agg.HitRateAtK = HitRate(firstRanks)
	agg.RecallAtK = Recall(found, total)
	agg.MRRAt10 = MRR(firstRanks)
	agg.NDCGAtK = MeanNDCG(hitLists, total, topK)
	agg.P50LatMS = Percentile(latencies, 50)
	agg.P95LatMS = Percentile(latencies, 95)
	return agg
}

func median(vals []float64) float64 {
	return Percentile(vals, 50)
}

// WriteReport persists a report as pretty-printed JSON.
func WriteReport(path string, r *Report) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}
