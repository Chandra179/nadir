// Command evalbench measures Retrieval quality over a golden query set
// against the configured Qdrant collection and optional reranker.
//
// Usage:
//
//	go run ./cmd/evalbench [--config config/config.yaml] [--golden tests/eval/golden.json]
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

	"nadir/config"
	"nadir/internal/chunker"
	"nadir/internal/embedder"
	"nadir/internal/enrichment"
	"nadir/internal/eval"
	"nadir/internal/ingest"
	"nadir/internal/logger"
	"nadir/internal/qdrantutil"
	"nadir/internal/reranker"
	"nadir/internal/search"
	"nadir/internal/store"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// ingestStore is the narrow Document Store capability needed by the
// development indexing pass. Retrieval and reset are separate concerns.
type ingestStore interface {
	GetAllFileSHAs(context.Context) (map[string]string, error)
	ReplaceDocument(context.Context, string, string, []store.ScoredChunk) error
}

func main() {
	configPath := flag.String("config", "config/config.yaml", "path to config file")
	goldenPath := flag.String("golden", "tests/eval/golden.json", "path to golden query set")
	topK := flag.Int("top-k", 0, "results per query (0 uses qdrant.top_k)")
	noRerank := flag.Bool("no-rerank", false, "bypass the configured reranker")
	runs := flag.Int("runs", 3, "runs per query; latency is reported as the median")
	reportPath := flag.String("report", "", "output report path (default: tests/eval/reports/<unix_ts>.json)")
	ensureIngest := flag.Bool("ensure-ingest", false, "run an ingest pass over source.paths before evaluating")
	flag.Parse()

	if err := run(*configPath, *goldenPath, *topK, *noRerank, *runs, *reportPath, *ensureIngest); err != nil {
		fmt.Fprintln(os.Stderr, "evalbench:", err)
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
	conn, err := grpc.NewClient(cfg.Qdrant.Addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("qdrant dial: %w", err)
	}
	defer conn.Close()
	clients := qdrantutil.NewClients(conn)

	st, err := store.NewDependencies(store.DependenciesConfig{
		Clients:     clients,
		Collection:  cfg.Qdrant.Collection,
		PrefetchMul: cfg.Qdrant.PrefetchMul,
	})
	if err != nil {
		return fmt.Errorf("qdrant init: %w", err)
	}
	emb := embedder.NewDependencies(embedder.DependenciesConfig{
		Addr:           cfg.Embedder.OllamaAddr,
		Model:          cfg.Embedder.Model,
		Dimensions:     cfg.Embedder.Dimensions,
		RequestTimeout: cfg.Embedder.RequestTimeout,
	})
	if err := st.EnsureCollection(ctx, emb.Dimensions()); err != nil {
		return fmt.Errorf("ensure collection: %w", err)
	}

	stats, err := st.Stats(ctx)
	if err != nil {
		return fmt.Errorf("collection stats: %w", err)
	}
	if stats.Chunks == 0 || ensureIngest {
		if err := ensureIngested(ctx, cfg, st, emb, log); err != nil {
			return err
		}
	}

	var searchReranker reranker.Reranker
	rerankEnabled := cfg.Reranker.Enabled && !noRerank
	if rerankEnabled {
		searchReranker = reranker.NewDependencies(reranker.DependenciesConfig{
			Addr:           cfg.Reranker.Addr,
			MaxConcurrent:  cfg.Reranker.MaxConcurrent,
			RequestTimeout: cfg.Reranker.RequestTimeout,
			Log:            log,
		})
	}

	searcher := search.NewDependencies(search.DependenciesConfig{
		Embedder:               emb,
		Store:                  st,
		Reranker:               searchReranker,
		CandidateMul:           cfg.Reranker.CandidateMul,
		QueryPrefix:            cfg.Embedder.QueryPrefix,
		MaxQueryChars:          cfg.Search.MaxQueryChars,
		MaxFragments:           cfg.Search.MaxFragments,
		MaxConcurrentFragments: cfg.Search.MaxConcurrentFragments,
		MaxTopK:                cfg.Search.MaxTopK,
		MaxChunksPerFile:       cfg.Search.MaxChunksPerFile,
		Log:                    log,
	})

	golden, err := eval.LoadGoldenSet(goldenPath)
	if err != nil {
		return err
	}
	if topK <= 0 {
		topK = cfg.Qdrant.TopK
	}
	if topK > cfg.Search.MaxTopK {
		topK = cfg.Search.MaxTopK
	}

	report, err := eval.NewDependencies(eval.DependenciesConfig{
		Searcher: searcher,
		Log:      log,
	}).Run(ctx, golden, topK, runs)
	if err != nil {
		return err
	}
	report.Rerank = rerankEnabled
	printReport(report)

	if reportPath == "" {
		reportPath = filepath.Join("tests", "eval", "reports", fmt.Sprintf("%d.json", time.Now().Unix()))
	}
	if dir := filepath.Dir(reportPath); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create report directory: %w", err)
		}
	}
	if err := eval.WriteReport(reportPath, report); err != nil {
		return err
	}
	fmt.Println("\nreport written to", reportPath)
	return nil
}

func ensureIngested(ctx context.Context, cfg *config.Config, st ingestStore, emb embedder.Embedder, log *zap.Logger) error {
	if len(cfg.Source.Paths) == 0 {
		return fmt.Errorf("collection is empty or --ensure-ingest was requested, but source.paths has no directories")
	}
	files, err := ingest.DiscoverFiles(cfg.Source.Paths, cfg.Source.IgnorePatterns, cfg.Ingest.MaxFileBytes)
	if err != nil {
		return fmt.Errorf("discover source files: %w", err)
	}
	if len(files) == 0 {
		return fmt.Errorf("no supported source files found under %v", cfg.Source.Paths)
	}

	chunkr := chunker.NewDependencies(chunker.DependenciesConfig{
		Provider:     cfg.Chunker.Provider,
		ChunkSize:    cfg.Chunker.ChunkSize,
		ChunkOverlap: cfg.Chunker.ChunkOverlap,
		WindowSize:   cfg.Chunker.WindowSize,
	})
	var enricher enrichment.Enricher
	if cfg.Enrichment.Hype.Enabled || cfg.Enrichment.Contextual.Enabled {
		enricher = enrichment.NewDependencies(enrichment.DependenciesConfig{
			HypeAddr:        cfg.Enrichment.Hype.OllamaAddr,
			HypeModel:       cfg.Enrichment.Hype.Model,
			ContextualAddr:  cfg.Enrichment.Contextual.OllamaAddr,
			ContextualModel: cfg.Enrichment.Contextual.Model,
			RequestTimeout:  cfg.Enrichment.RequestTimeout,
		})
	}
	var converter ingest.DocumentConverter
	if cfg.Docling.Enabled {
		converter = ingest.NewDoclingConverter(cfg.Docling.Addr, cfg.Docling.RequestTimeout, log)
	}
	deps := ingest.NewDependencies(ingest.DependenciesConfig{
		Chunker:           chunkr,
		Embedder:          emb,
		Store:             st,
		Enricher:          enricher,
		DocumentConverter: converter,
		HypeEnabled:       cfg.Enrichment.Hype.Enabled,
		HypeQuestions:     cfg.Enrichment.Hype.QuestionsPerChunk,
		ContextualEnabled: cfg.Enrichment.Contextual.Enabled,
		Retry: ingest.RetryConfig{
			MaxAttempts:     cfg.Ingest.MaxAttempts,
			InitialInterval: cfg.Ingest.InitialInterval,
			MaxInterval:     cfg.Ingest.MaxInterval,
			Multiplier:      cfg.Ingest.Multiplier,
		},
		Workers:          cfg.Ingest.Workers,
		MaxFileBytes:     cfg.Ingest.MaxFileBytes,
		EmbedBatchSize:   cfg.Ingest.EmbedBatchSize,
		MaxChunksPerFile: cfg.Ingest.MaxChunksPerFile,
		DocumentPrefix:   cfg.Embedder.DocumentPrefix,
		Log:              log,
	})
	log.Info("ingesting source files before evaluation", zap.Int("files", len(files)), zap.Strings("roots", cfg.Source.Paths))
	result, err := deps.Run(ctx, files)
	if err != nil {
		return fmt.Errorf("ingest: %w", err)
	}
	log.Info("ingest finished", zap.Int("processed", result.Processed), zap.Int("skipped", result.Skipped), zap.Int("failed", result.Failed))
	return nil
}

func printReport(report *eval.Report) {
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
