package pkb

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

// filterProfiles removes profiles not in the EVAL_PROFILES comma-separated allowlist.
// Empty EVAL_PROFILES = run all.
func filterProfiles(profiles []evalProfile) []evalProfile {
	filter := os.Getenv("EVAL_PROFILES")
	if filter == "" {
		return profiles
	}
	want := make(map[string]bool)
	for _, name := range strings.Split(filter, ",") {
		want[strings.TrimSpace(name)] = true
	}
	out := profiles[:0]
	for _, p := range profiles {
		if want[p.Name] {
			out = append(out, p)
		}
	}
	return out
}

func TestSearchEval(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping eval: requires Docker + Ollama")
	}

	cfg := loadConfig(t)
	evalCfg := loadEvalConfig(t)

	ollamaAddr := os.Getenv("OLLAMA_ADDR")
	if ollamaAddr == "" {
		ollamaAddr = cfg.Embedder.OllamaAddr
	}

	ctx := context.Background()
	embedder := NewOllamaEmbedder(ollamaAddr, cfg.Embedder.Model, cfg.Embedder.Dimensions)
	judge, judgeName := buildJudge(t, evalCfg)
	topK := cfg.Qdrant.TopK
	evalCases := loadEvalCases(t)
	profiles := filterProfiles(loadEvalProfiles(t))
	history := newEvalHistoryWriter(t, evalCfg)

	for _, profile := range profiles {
		t.Run(profile.Name, func(t *testing.T) {
			t.Parallel()

			if profile.SparseScorer == "splade" && !checkSPLADESidecar(cfg.SparseScorer.Addr) {
				t.Skipf("splade sidecar not reachable at %s — run: python cmd/splade/main.py", cfg.SparseScorer.Addr)
			}

			store, docsIngested, mode, collection := buildStore(t, ctx, embedder, cfg)
			rerankerName, reranker := buildReranker(t, profile, cfg)

			var sparseEmb SparseEmbedder
			if profile.SparseScorer == "splade" {
				sparseEmb = NewSPLADESparseScorer(cfg.SparseScorer.Addr)
				if qs, ok := store.(*QdrantStore); ok {
					store = qs.WithSparseEmbedder(sparseEmb)
				}
			} else {
				if qs, ok := store.(*QdrantStore); ok {
					store = qs.WithSparseScorer(buildSparseScorer(ctx, profile, cfg))
				}
			}

			var vectorCount int64
			if qs, ok := store.(*QdrantStore); ok {
				if n, err := qs.PointCount(ctx); err == nil {
					vectorCount = n
				} else {
					t.Logf("warn: point count: %v", err)
				}
			}

			hydeSearcher := buildHyDE(t, profile, cfg, embedder, store)

			qrelsTotal, qrelsRelevant := qrelsStats(judge)
			queryResults, metrics := runEval(t, ctx, evalCases, embedder, store, reranker, judge, topK, cfg.Reranker.CandidateMul, hydeSearcher)

			printEvalReport(profile.Name, cfg.Embedder.Model, topK, profile, rerankerName,
				docsIngested, vectorCount, qrelsTotal, qrelsRelevant, queryResults, metrics)

			history.write(t, evalHistoryEntry{
				HyDE:      profile.HyDE || profile.AdaptiveHyDE || profile.MultiHyDE,
				MultiHyDE: profile.MultiHyDE,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				Profile:         profile.Name,
				ProfileTags:     profile.Tags,
				SparseScorer:    profile.SparseScorer,
				Reranker:        rerankerName,
				ChunkSize:       profile.ChunkSize,
				ChunkOverlap:    profile.ChunkOverlap,
				ChunkerProvider: profile.ChunkerProvider,
				Mode:            mode,
				Judge:           judgeName,
				Collection:      collection,
				Model:           cfg.Embedder.Model,
				EmbedderDims:    cfg.Embedder.Dimensions,
				Queries:         len(evalCases),
				TopK:            topK,
				DocsIngested:    docsIngested,
				VectorCount:     vectorCount,
				QrelsTotal:      qrelsTotal,
				QrelsRelevant:   qrelsRelevant,
				CandidateMul:    cfg.Reranker.CandidateMul,
				MRR:             metrics.MRR,
				HitRate:         metrics.HitRate,
				NDCG:            metrics.NDCG,
				Precision:       metrics.Precision,
				FailedQueries:   countMisses(queryResults),
			})
		})
	}
}

func countMisses(results []QueryResult) int {
	n := 0
	for _, r := range results {
		if !r.Hit {
			n++
		}
	}
	return n
}

// printEvalReport prints a structured eval report:
//  1. Aggregate metrics table
//  2. Per-query breakdown sorted worst-first (NDCG ascending)
//  3. Failure list (hit=false queries)
//  4. Category breakdown
func printEvalReport(
	profileName, model string,
	topK int,
	profile evalProfile,
	rerankerName string,
	docsIngested int,
	vectorCount int64,
	qrelsTotal, qrelsRelevant int,
	queryResults []QueryResult,
	metrics EvalMetrics,
) {
	sep := strings.Repeat("─", 72)
	fmt.Printf("\n%s\n", sep)
	fmt.Printf("EVAL: %-40s  model=%-20s topK=%d\n", profileName, model, topK)
	fmt.Printf("%s\n", sep)

	// profile config
	tags := strings.Join(profile.Tags, ",")
	if tags == "" {
		tags = "-"
	}
	fmt.Printf("sparse=%-8s  reranker=%-14s  chunker=%s size=%d overlap=%d\n",
		profile.SparseScorer, rerankerName, orDefault(profile.ChunkerProvider, "recursive"),
		profile.ChunkSize, profile.ChunkOverlap)
	fmt.Printf("tags=%-20s  docs=%d  vectors=%d\n", tags, docsIngested, vectorCount)
	fmt.Printf("qrels: total=%-5d  relevant=%d\n", qrelsTotal, qrelsRelevant)
	fmt.Printf("%s\n", sep)

	// aggregate metrics
	fmt.Printf("%-16s  %-8s  %-8s  %-8s  %-8s  %-8s  %-8s\n",
		"metric", "MRR", "HitRate", "NDCG", "P@K", "Recall", "MAP")
	fmt.Printf("%-16s  %-8.4f  %-8.4f  %-8.4f  %-8.4f  %-8.4f  %-8.4f\n",
		"aggregate",
		metrics.MRR, metrics.HitRate, metrics.NDCG,
		metrics.Precision, metrics.Recall, metrics.MAP)
	fmt.Printf("%s\n", sep)

	// per-query breakdown: worst first
	sorted := make([]QueryResult, len(queryResults))
	copy(sorted, queryResults)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].NDCG < sorted[j].NDCG
	})
	fmt.Printf("PER-QUERY BREAKDOWN (worst first)\n")
	fmt.Printf("%-5s  %-14s  %-8s  %-6s  %-6s  %-6s  %s\n",
		"rank", "category/diff", "NDCG", "MRR", "P@K", "R@K", "query")
	for idx, r := range sorted {
		catDiff := fmt.Sprintf("%s/%s", orDefault(r.Category, "?"), orDefault(r.Difficulty, "?"))
		miss := ""
		if !r.Hit {
			miss = " ✗"
		}
		fmt.Printf("%-5d  %-14s  %-8.4f  %-6.4f  %-6.4f  %-6.4f  %.60s%s\n",
			idx+1, catDiff, r.NDCG, r.ReciprocalRank, r.Precision, r.Recall,
			r.Query, miss)
	}
	fmt.Printf("%s\n", sep)

	// failure list
	var failures []QueryResult
	for _, r := range queryResults {
		if !r.Hit {
			failures = append(failures, r)
		}
	}
	fmt.Printf("MISSES (%d/%d)\n", len(failures), len(queryResults))
	if len(failures) == 0 {
		fmt.Printf("  (none)\n")
	}
	for i, r := range failures {
		fmt.Printf("  %2d. [%s/%s] %s\n", i+1,
			orDefault(r.Category, "?"), orDefault(r.Difficulty, "?"), r.Query)
		if len(r.TopChunks) > 0 {
			fmt.Printf("      top1: %s:%d (score=%.3f)\n",
				r.TopChunks[0].FilePath, r.TopChunks[0].LineStart, r.TopChunks[0].Score)
		}
	}
	fmt.Printf("%s\n", sep)

	// category breakdown
	type catMetrics struct {
		ndcgSum float64
		hitSum  int
		count   int
	}
	cats := make(map[string]*catMetrics)
	for _, r := range queryResults {
		cat := orDefault(r.Category, "unknown")
		if cats[cat] == nil {
			cats[cat] = &catMetrics{}
		}
		cats[cat].ndcgSum += r.NDCG
		if r.Hit {
			cats[cat].hitSum++
		}
		cats[cat].count++
	}
	catNames := make([]string, 0, len(cats))
	for k := range cats {
		catNames = append(catNames, k)
	}
	sort.Strings(catNames)
	fmt.Printf("CATEGORY BREAKDOWN\n")
	fmt.Printf("%-20s  %6s  %8s  %8s\n", "category", "n", "NDCG@K", "HitRate")
	for _, cat := range catNames {
		m := cats[cat]
		fmt.Printf("%-20s  %6d  %8.4f  %8.4f\n",
			cat, m.count,
			m.ndcgSum/float64(m.count),
			float64(m.hitSum)/float64(m.count))
	}
	fmt.Printf("%s\n\n", sep)
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
