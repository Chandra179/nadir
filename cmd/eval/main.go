package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"nadir/config"
	"nadir/internal/embedder"
	"nadir/internal/eval"
	"nadir/internal/generator"
	"nadir/internal/reranker"
	"nadir/internal/search"
	"nadir/internal/store"

	"github.com/Chandra179/gosdk/logger"
)

const (
	defaultRerankerCandidate = 3
)

func main() {
	goldenPath := flag.String("golden", "", "path to golden set YAML (required)")
	fetchK := flag.Int("fetch-k", 10, "candidates to retrieve per query")
	configPath := flag.String("config", "config/config.yaml", "path to config.yaml")
	mode := flag.String("mode", "retrieval", "eval mode: retrieval | rag | both")
	granularity := flag.String("granularity", "file", "scoring unit: file | chunk")
	judgeModel := flag.String("judge-model", "", "LLM model for RAGAS judge (defaults to generator.model)")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	if *goldenPath == "" {
		log.Fatal("-golden is required")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	gs, err := eval.LoadGolden(*goldenPath)
	if err != nil {
		log.Fatalf("load golden set: %v", err)
	}
	if len(gs.Queries) == 0 {
		log.Fatalf("golden set %s has no queries", *goldenPath)
	}

	if len(gs.Queries) < 50 {
		fmt.Fprintf(os.Stderr, "WARNING: n=%d queries is below the n>=50 floor for defensible claims. Treat numbers as directional, not statistically significant.\n", len(gs.Queries))
	}

	searcher := buildSearcher(ctx, cfg)

	switch *mode {
	case "retrieval":
		runRetrieval(ctx, gs, searcher, *fetchK, *granularity, *goldenPath, cfg)
	case "rag":
		runRAG(ctx, gs, searcher, cfg, *fetchK, *judgeModel, *goldenPath)
	case "both":
		runRetrieval(ctx, gs, searcher, *fetchK, *granularity, *goldenPath, cfg)
		fmt.Fprintln(os.Stdout)
		fmt.Fprintln(os.Stdout, "=== RAG Evaluation (RAGAS) ===")
		runRAG(ctx, gs, searcher, cfg, *fetchK, *judgeModel, *goldenPath)
	default:
		log.Fatalf("unknown mode %q (use: retrieval, rag, both)", *mode)
	}
}

func runRetrieval(ctx context.Context, gs *eval.GoldenSet, searcher eval.Retriever, fetchK int, gran string, goldenPath string, cfg *config.Config) {
	g := eval.FileLevel
	if gran == "chunk" {
		g = eval.ChunkLevel
	}
	runner := &eval.Runner{Searcher: searcher, TopK: fetchK, Granularity: g}

	rep, err := runner.Run(ctx, gs, fetchK)
	if err != nil {
		log.Fatalf("retrieval eval: %v", err)
	}
	eval.PrintReport(os.Stdout, rep)
	saveRetrievalResults(rep, goldenPath, fetchK, gran, cfg)
}

func runRAG(ctx context.Context, gs *eval.GoldenSet, searcher eval.Retriever, cfg *config.Config, fetchK int, judgeModel string, goldenPath string) {
	ollamaAddr := cfg.Generator.OllamaAddr
	if ollamaAddr == "" {
		ollamaAddr = cfg.Embedder.OllamaAddr
	}
	if judgeModel == "" {
		judgeModel = cfg.Generator.Model
	}

	gen := generator.NewOllamaGenerator(ollamaAddr, cfg.Generator.Model, cfg.Generator.MaxContextTokens)
	judge := eval.NewOllamaJudge(ollamaAddr+"/v1", judgeModel, "")

	evaluator := &eval.RAGASEvaluator{
		Judge:     judge,
		Generator: &eval.GeneratorAdapter{Gen: gen},
	}

	rep, err := evaluator.Evaluate(ctx, gs, searcher, fetchK)
	if err != nil {
		log.Fatalf("RAG eval: %v", err)
	}
	eval.PrintRAGASReport(os.Stdout, rep)
	saveRAGResults(rep, goldenPath, fetchK, cfg)
}

func buildSearcher(ctx context.Context, cfg *config.Config) *search.Service {
	appLog := logger.NewLogger(cfg.Middleware.Logger.Level)

	s, err := store.NewQdrantStore(cfg.Qdrant.Addr, cfg.Qdrant.Collection, cfg.Qdrant.PrefetchMul, appLog)
	if err != nil {
		log.Fatalf("qdrant init: %v", err)
	}

	e := embedder.NewOllamaEmbedder(cfg.Embedder.OllamaAddr, cfg.Embedder.Model, cfg.Embedder.Dimensions)
	if err := s.EnsureCollection(ctx, e.Dimensions()); err != nil {
		log.Fatalf("ensure collection: %v", err)
	}

	searchService := search.NewService(e, s, appLog)

	if cfg.Reranker.Enabled {
		mul := cfg.Reranker.CandidateMul
		if mul < 1 {
			mul = defaultRerankerCandidate
		}
		searchService.WithReranker(reranker.NewHTTPReranker(cfg.Reranker.Addr, cfg.Reranker.MaxConcurrent), mul)
	}

	return searchService
}

// ---------------------------------------------------------------------------
// Results persistence
// ---------------------------------------------------------------------------

type runMeta struct {
	Timestamp   string `json:"timestamp"`
	Golden      string `json:"golden"`
	Mode        string `json:"mode"`
	FetchK      int    `json:"fetch_k"`
	Granularity string `json:"granularity,omitempty"`
	Embedder    string `json:"embedder"`
	Reranker    bool   `json:"reranker"`
	Generator   string `json:"generator,omitempty"`
}

type retrievalOutput struct {
	Meta      runMeta            `json:"meta"`
	Aggregate eval.Report        `json:"aggregate"`
	PerQuery  []eval.QueryReport `json:"queries"`
}

type ragasOutput struct {
	Meta      runMeta                `json:"meta"`
	Aggregate eval.RAGASReport       `json:"aggregate"`
	PerQuery  []eval.RAGASQueryReport `json:"queries"`
}

func saveRetrievalResults(rep eval.Report, goldenPath string, fetchK int, gran string, cfg *config.Config) {
	meta := runMeta{
		Timestamp:   time.Now().Format(time.RFC3339),
		Golden:      goldenPath,
		Mode:        "retrieval",
		FetchK:      fetchK,
		Granularity: gran,
		Embedder:    cfg.Embedder.Model,
		Reranker:    cfg.Reranker.Enabled,
	}
	out := retrievalOutput{Meta: meta, Aggregate: rep, PerQuery: rep.PerQuery}
	saveJSON("retrieval", out)
}

func saveRAGResults(rep eval.RAGASReport, goldenPath string, fetchK int, cfg *config.Config) {
	meta := runMeta{
		Timestamp: time.Now().Format(time.RFC3339),
		Golden:    goldenPath,
		Mode:      "rag",
		FetchK:    fetchK,
		Embedder:  cfg.Embedder.Model,
		Reranker:  cfg.Reranker.Enabled,
		Generator: cfg.Generator.Model,
	}
	out := ragasOutput{Meta: meta, Aggregate: rep, PerQuery: rep.PerQuery}
	saveJSON("rag", out)
}

func saveJSON(suffix string, v any) {
	dir := "results"
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("WARN: create results dir: %v", err)
		return
	}
	ts := time.Now().Format("2006-01-02T15-04-05")
	name := fmt.Sprintf("%s_%s.json", ts, suffix)
	path := filepath.Join(dir, name)

	f, err := os.Create(path)
	if err != nil {
		log.Printf("WARN: create result file: %v", err)
		return
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		log.Printf("WARN: encode results: %v", err)
	}
	fmt.Fprintf(os.Stderr, "results written to %s\n", path)
}
