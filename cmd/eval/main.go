package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"nadir/config"
	"nadir/internal/embedder"
	"nadir/internal/eval"
	"nadir/internal/generator"
	"nadir/internal/reranker"
	"nadir/internal/search"
	"nadir/internal/store"
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
		runRetrieval(ctx, gs, searcher, *fetchK, *granularity)
	case "rag":
		runRAG(ctx, gs, searcher, cfg, *fetchK, *judgeModel)
	case "both":
		runRetrieval(ctx, gs, searcher, *fetchK, *granularity)
		fmt.Fprintln(os.Stdout)
		fmt.Fprintln(os.Stdout, "=== RAG Evaluation (RAGAS) ===")
		runRAG(ctx, gs, searcher, cfg, *fetchK, *judgeModel)
	default:
		log.Fatalf("unknown mode %q (use: retrieval, rag, both)", *mode)
	}
}

func runRetrieval(ctx context.Context, gs *eval.GoldenSet, searcher eval.Retriever, fetchK int, gran string) {
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
}

func runRAG(ctx context.Context, gs *eval.GoldenSet, searcher eval.Retriever, cfg *config.Config, fetchK int, judgeModel string) {
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
}

func buildSearcher(ctx context.Context, cfg *config.Config) *search.Service {
	s, err := store.NewQdrantStore(cfg.Qdrant.Addr, cfg.Qdrant.Collection, cfg.Qdrant.PrefetchMul)
	if err != nil {
		log.Fatalf("qdrant init: %v", err)
	}

	e := embedder.NewOllamaEmbedder(cfg.Embedder.OllamaAddr, cfg.Embedder.Model, cfg.Embedder.Dimensions)
	if err := s.EnsureCollection(ctx, e.Dimensions()); err != nil {
		log.Fatalf("ensure collection: %v", err)
	}

	searchService := search.NewService(e, s)

	if cfg.Reranker.Enabled {
		mul := cfg.Reranker.CandidateMul
		if mul < 1 {
			mul = defaultRerankerCandidate
		}
		searchService.WithReranker(reranker.NewHTTPReranker(cfg.Reranker.Addr), mul)
	}

	return searchService
}
