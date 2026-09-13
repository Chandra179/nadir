// Command evaluator measures Retrieval quality over a golden query set
// against the configured Qdrant collection and optional reranker.
//
// Usage:
//
//	go run ./cmd/evaluator [--config config/config.yaml] [--golden test/evaluation/golden.json]
//	                         [--top-k N] [--no-rerank] [--runs N]
//	                         [--report path] [--ensure-ingest]
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"nadir/internal/evaluation"
	"nadir/internal/knowledge/indexing"
	"nadir/internal/platform/configuration"
	"nadir/internal/platform/logging"
	"nadir/internal/platform/runtime"

	"go.uber.org/zap"
)

func main() {
	configPath := flag.String("config", "config/config.yaml", "path to config file")
	goldenPath := flag.String("golden", "test/evaluation/golden.json", "path to golden query set")
	topK := flag.Int("top-k", 0, "results per query (0 uses qdrant.top_k)")
	noRerank := flag.Bool("no-rerank", false, "bypass the configured reranker")
	runs := flag.Int("runs", 3, "runs per query; latency is reported as the median")
	reportPath := flag.String("report", "", "output report path (default: test/evaluation/reports/<unix_ts>.json)")
	ensureIngest := flag.Bool("ensure-ingest", false, "run an ingest pass over source.paths before evaluating")
	flag.Parse()

	if err := run(*configPath, *goldenPath, *topK, *noRerank, *runs, *reportPath, *ensureIngest); err != nil {
		fmt.Fprintln(os.Stderr, "evaluator:", err)
		os.Exit(1)
	}
}

func run(configPath, goldenPath string, topK int, noRerank bool, runs int, reportPath string, ensureIngest bool) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	log, err := logger.New("info")
	if err != nil {
		return fmt.Errorf("init logger: %w", err)
	}
	defer log.Sync()

	ctx := context.Background()
	graph, err := runtime.NewDependencies(ctx, cfg, log, runtime.Options{
		DisableReranker:      noRerank,
		DisableSemanticCache: true,
	})
	if err != nil {
		return fmt.Errorf("shared runtime: %w", err)
	}
	defer func() { _ = graph.Close() }()

	stats, err := graph.Stats(ctx)
	if err != nil {
		return fmt.Errorf("collection stats: %w", err)
	}
	if stats.Chunks == 0 || ensureIngest {
		if err := ensureIngested(ctx, cfg, graph.Ingest, log); err != nil {
			return err
		}
	}

	rerankEnabled := cfg.Reranker.Enabled && !noRerank

	golden, err := evaluation.LoadGoldenSet(goldenPath)
	if err != nil {
		return err
	}
	if topK <= 0 {
		topK = cfg.Qdrant.TopK
	}
	if topK > cfg.Search.MaxTopK {
		topK = cfg.Search.MaxTopK
	}

	report, err := evaluation.NewDependencies(evaluation.DependenciesConfig{
		Searcher: graph.Searcher,
		Log:      log,
	}).Run(ctx, golden, topK, runs)
	if err != nil {
		return err
	}
	report.Rerank = rerankEnabled
	printReport(report)

	if reportPath == "" {
		reportPath = filepath.Join("test", "evaluation", "reports", fmt.Sprintf("%d.json", time.Now().Unix()))
	}
	if dir := filepath.Dir(reportPath); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create report directory: %w", err)
		}
	}
	if err := evaluation.WriteReport(reportPath, report); err != nil {
		return err
	}
	fmt.Println("\nreport written to", reportPath)
	return nil
}

func ensureIngested(ctx context.Context, cfg *config.Config, ing indexing.Ingest, log *zap.Logger) error {
	if len(cfg.Source.Paths) == 0 {
		return fmt.Errorf("collection is empty or --ensure-ingest was requested, but source.paths has no directories")
	}
	files, err := indexing.DiscoverFiles(cfg.Source.Paths, cfg.Source.IgnorePatterns, cfg.Ingest.MaxFileBytes)
	if err != nil {
		return fmt.Errorf("discover source files: %w", err)
	}
	if len(files) == 0 {
		return fmt.Errorf("no supported source files found under %v", cfg.Source.Paths)
	}

	log.Info("ingesting source files before evaluation", zap.Int("files", len(files)), zap.Strings("roots", cfg.Source.Paths))
	result, err := ing.Run(ctx, files)
	if err != nil {
		return fmt.Errorf("ingest: %w", err)
	}
	log.Info("ingest finished", zap.Int("processed", result.Processed), zap.Int("skipped", result.Skipped), zap.Int("failed", result.Failed))
	return nil
}

func printReport(report *evaluation.Report) {
	fmt.Printf("\n%-28s %-6s %-10s %-8s\n", "QUERY", "RANK", "FOUND", "MS")
	fmt.Println(strings.Repeat("-", 56))
	for _, query := range report.PerQuery {
		rank := "-"
		if query.FirstHitRank > 0 {
			rank = fmt.Sprintf("%d", query.FirstHitRank)
		}
		fmt.Printf("%-28s %-6s %d/%-8d %.1f\n", truncate(query.ID, 28), rank, query.RelevantFound, query.NumRelevant, query.LatencyMS)
	}
	aggregate := report.Aggregate
	fmt.Println(strings.Repeat("-", 56))
	fmt.Printf("HitRate@%d      %.3f\n", aggregate.TopK, aggregate.HitRateAtK)
	fmt.Printf("Recall@%d       %.3f\n", aggregate.TopK, aggregate.RecallAtK)
	fmt.Printf("MRR@10         %.3f\n", aggregate.MRRAt10)
	fmt.Printf("nDCG@%d         %.3f\n", aggregate.TopK, aggregate.NDCGAtK)
	fmt.Printf("latency p50/p95  %.1fms / %.1fms\n", aggregate.P50LatMS, aggregate.P95LatMS)
	fmt.Printf("queries=%d reranker=%v top_k=%d\n", aggregate.Queries, report.Rerank, aggregate.TopK)
}

func truncate(value string, width int) string {
	if len(value) <= width {
		return value
	}
	return value[:width-1] + "~"
}
