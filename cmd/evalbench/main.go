// Command evalbench measures retrieval quality over a golden query set
// against a live Qdrant (+ optional Ollama/reranker sidecars), so retrieval
// changes can be A/B'd with real numbers.
//
// Usage:
//
//	go run ./cmd/evalbench [--config config/config.yaml] [--golden eval/golden.json]
//	                      [--top-k N] [--no-rerank] [--runs 1] [--report path]
//	                      [--ensure-ingest]
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
	"nadir/internal/reranker"
	"nadir/internal/search"
	"nadir/internal/store"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	configPath := flag.String("config", "config/config.yaml", "path to config file")
	goldenPath := flag.String("golden", "tests/eval/golden.json", "path to golden query set")
	topKFlag := flag.Int("top-k", 0, "results per query (0 = use qdrant.top_k from config)")
	noRerank := flag.Bool("no-rerank", false, "bypass the cross-encoder reranker")
	runs := flag.Int("runs", 1, "runs per query; latency reported as median")
	reportPath := flag.String("report", "", "output report path (default: tests/eval/reports/<unix_ts>.json)")
	ensureIngest := flag.Bool("ensure-ingest", false, "run an ingest pass over source.paths before evaluating (SHA dedup makes repeat runs cheap)")
	flag.Parse()

	if err := run(*configPath, *goldenPath, *topKFlag, *noRerank, *runs, *reportPath, *ensureIngest); err != nil {
		fmt.Fprintln(os.Stderr, "evalbench:", err)
		os.Exit(1)
	}
}

func run(configPath, goldenPath string, topKFlag int, noRerank bool, runs int, reportPath string, ensureIngest bool) error {
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

	st, err := store.NewDependencies(store.DependenciesConfig{
		Conn:        conn,
		Collection:  cfg.Qdrant.Collection,
		PrefetchMul: cfg.Qdrant.PrefetchMul,
		Log:         log,
	})
	if err != nil {
		return fmt.Errorf("qdrant init: %w", err)
	}

	e := embedder.NewDependencies(embedder.DependenciesConfig{
		Addr:       cfg.Embedder.OllamaAddr,
		Model:      cfg.Embedder.Model,
		Dimensions: cfg.Embedder.Dimensions,
	})

	if err := st.EnsureCollection(ctx, e.Dimensions()); err != nil {
		return fmt.Errorf("ensure collection: %w", err)
	}

	stats, err := st.Stats(ctx)
	if err != nil {
		return fmt.Errorf("collection stats: %w", err)
	}
	if stats.Chunks == 0 || ensureIngest {
		if len(cfg.Source.Paths) == 0 {
			return fmt.Errorf("collection is empty and source.paths has no directories to ingest")
		}
		files, err := collectSourceFiles(cfg.Source.Paths)
		if err != nil {
			return err
		}
		log.Info("ingesting source files before eval",
			zap.Int("files", len(files)),
			zap.Strings("roots", cfg.Source.Paths))
		chunkr := chunker.NewDependencies(chunker.DependenciesConfig{
			Provider:     cfg.Chunker.Provider,
			ChunkSize:    cfg.Chunker.ChunkSize,
			ChunkOverlap: cfg.Chunker.ChunkOverlap,
			WindowSize:   cfg.Chunker.WindowSize,
		})
		ingestDeps := ingest.NewDependencies(ingest.DependenciesConfig{
			Chunker:  chunkr,
			Embedder: e,
			Store:    st,
			Retry: ingest.RetryConfig{
				MaxAttempts:     cfg.Ingest.MaxAttempts,
				InitialInterval: cfg.Ingest.InitialInterval,
				MaxInterval:     cfg.Ingest.MaxInterval,
				Multiplier:      cfg.Ingest.Multiplier,
			},
			DocumentPrefix: cfg.Embedder.DocumentPrefix,
			Log:            log,
		})
		if cfg.Enrichment.Hype.Enabled || cfg.Enrichment.Contextual.Enabled {
			enrichAddr := cfg.Enrichment.Hype.OllamaAddr
			enrichModel := cfg.Enrichment.Hype.Model
			if cfg.Enrichment.Contextual.Enabled {
				if enrichAddr == "" {
					enrichAddr = cfg.Enrichment.Contextual.OllamaAddr
				}
				if enrichModel == "" {
					enrichModel = cfg.Enrichment.Contextual.Model
				}
			}
			if enrichAddr == "" {
				enrichAddr = cfg.Generator.OllamaAddr
			}
			if enrichAddr == "" {
				enrichAddr = cfg.Embedder.OllamaAddr
			}
			if enrichModel == "" {
				enrichModel = cfg.Generator.Model
			}
			ingestDeps.WithEnrichment(
				enrichment.NewDependencies(enrichment.DependenciesConfig{Addr: enrichAddr, Model: enrichModel}),
				cfg.Enrichment.Hype.QuestionsPerChunk,
				cfg.Enrichment.Contextual.Enabled,
			)
			log.Info("enrichment enabled for ingest",
				zap.Bool("hype", cfg.Enrichment.Hype.Enabled),
				zap.Int("questions_per_chunk", cfg.Enrichment.Hype.QuestionsPerChunk),
				zap.Bool("contextual", cfg.Enrichment.Contextual.Enabled))
		}
		result, err := ingestDeps.Run(ctx, files)
		if err != nil {
			return fmt.Errorf("ingest: %w", err)
		}
		log.Info("ingest finished",
			zap.Int("processed", result.Processed),
			zap.Int("skipped", result.Skipped),
			zap.Int("failed", result.Failed))
	}

	searchService := search.NewDependencies(search.DependenciesConfig{Embedder: e, Store: st, QueryPrefix: cfg.Embedder.QueryPrefix, Log: log})
	rerankOn := false
	if cfg.Reranker.Enabled && !noRerank {
		mul := cfg.Reranker.CandidateMul
		if mul < 1 {
			mul = 3
		}
		searchService.WithReranker(reranker.NewDependencies(reranker.DependenciesConfig{
			Addr:          cfg.Reranker.Addr,
			MaxConcurrent: cfg.Reranker.MaxConcurrent,
		}), mul)
		rerankOn = true
	}

	gs, err := eval.LoadGoldenSet(goldenPath)
	if err != nil {
		return err
	}

	topK := topKFlag
	if topK <= 0 {
		topK = cfg.Qdrant.TopK
	}

	h := eval.NewHarness(searchService, log)
	report, err := h.Run(ctx, gs, topK, runs)
	if err != nil {
		return err
	}
	report.Rerank = rerankOn

	printReport(report)

	if reportPath == "" {
		if err := os.MkdirAll(filepath.Join("tests", "eval", "reports"), 0o755); err != nil {
			return fmt.Errorf("create reports dir: %w", err)
		}
		reportPath = filepath.Join("tests", "eval", "reports", fmt.Sprintf("%d.json", time.Now().Unix()))
	} else {
		if dir := filepath.Dir(reportPath); dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("create report dir: %w", err)
			}
		}
	}
	if err := eval.WriteReport(reportPath, report); err != nil {
		return err
	}
	fmt.Println("\nreport written to", reportPath)
	return nil
}

// collectSourceFiles walks roots and returns every .md file as an upload,
// named by its base name to match how scripts/local.sh ingests via curl.
func collectSourceFiles(roots []string) ([]ingest.UploadFile, error) {
	var files []ingest.UploadFile
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".md") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read %s: %w", path, err)
			}
			files = append(files, ingest.UploadFile{Name: filepath.Base(path), Data: data})
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walk %s: %w", root, err)
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no .md files found under %v", roots)
	}
	return files, nil
}

func printReport(r *eval.Report) {
	fmt.Printf("\n%-28s %-6s %-10s %-8s\n", "QUERY", "RANK", "FOUND", "MS")
	fmt.Println(strings.Repeat("-", 56))
	for _, qr := range r.PerQuery {
		rank := "-"
		if qr.FirstHitRank > 0 {
			rank = fmt.Sprintf("%d", qr.FirstHitRank)
		}
		fmt.Printf("%-28s %-6s %d/%-8d %.1f\n", truncate(qr.ID, 28), rank, qr.RelevantFound, qr.NumRelevant, qr.LatencyMS)
	}
	a := r.Aggregate
	fmt.Println(strings.Repeat("-", 56))
	fmt.Printf("HitRate@%d      %.3f\n", a.TopK, a.HitRateAtK)
	fmt.Printf("Recall@%d       %.3f\n", a.TopK, a.RecallAtK)
	fmt.Printf("MRR@10         %.3f\n", a.MRRAt10)
	fmt.Printf("nDCG@%d         %.3f\n", a.TopK, a.NDCGAtK)
	fmt.Printf("latency p50/p95  %.1fms / %.1fms\n", a.P50LatMS, a.P95LatMS)
	fmt.Printf("queries=%d reranker=%v top_k=%d\n", a.Queries, r.Rerank, a.TopK)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "~"
}
