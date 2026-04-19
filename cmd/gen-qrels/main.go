// gen-qrels bootstraps testdata/qrels.jsonl by running all eval queries against live Qdrant,
// judging each result with an LLM, and writing the labeled output for human review.
//
// Usage:
//
//	go run ./cmd/gen-qrels [flags]
//
// Flags:
//
//	-config   path to config.yaml (default: config/config.yaml)
//	-out      output qrels file   (default: internal/pkb/testdata/qrels.jsonl)
//	-top-k    results per query   (default: from config)
//	-base-url LLM base URL        (overrides config eval.llm_base_url)
//	-model    LLM model           (overrides config eval.llm_model)
//	-api-key  LLM API key         (overrides EVAL_LLM_API_KEY env)
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"nadir/config"
	"nadir/internal/pkb"
)

var queries = []string{
	"How does the Go GPM scheduler work with goroutines and OS threads?",
	"What causes goroutine leaks and how do you prevent them?",
	"How do Go channels work internally with sendq and recvq?",
	"What changed in Go 1.22 loop variable scoping semantics?",
	"How does strings.Builder improve concatenation performance?",
	"How does consistent hashing minimize data movement when adding servers?",
	"What is the Snowflake ID structure and how does it handle clock skew?",
	"How does rate limiting use Redis with thundering herd protection?",
	"How does cache stampede prevention work with request coalescing?",
	"How do virtual nodes in consistent hashing improve load distribution?",
	"What is the difference between B-Tree and LSM Tree for database storage?",
	"How do ACID transactions handle isolation levels?",
	"What is the N+1 query problem and how do you solve it?",
	"How does Kafka partition routing work for producers?",
	"What is the CPU fetch-execute cycle?",
	"How does gradient descent work with backpropagation?",
	"What activation functions should you use and when?",
	"How does dropout prevent overfitting in neural networks?",
	"What is Mixture of Experts architecture in large language models?",
	"How does the transformer attention mechanism work?",
}

func main() {
	configPath := flag.String("config", "config/config.yaml", "path to config.yaml")
	outPath := flag.String("out", "internal/pkb/testdata/qrels.jsonl", "output qrels file")
	topKFlag := flag.Int("top-k", 0, "results per query (0 = use config value)")
	baseURL := flag.String("base-url", "", "LLM base URL (overrides config)")
	model := flag.String("model", "", "LLM model (overrides config)")
	apiKey := flag.String("api-key", "", "LLM API key (overrides EVAL_LLM_API_KEY env)")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	llmBaseURL := cfg.Eval.LLMBaseURL
	if *baseURL != "" {
		llmBaseURL = *baseURL
	}
	llmModel := cfg.Eval.LLMModel
	if *model != "" {
		llmModel = *model
	}
	llmAPIKey := os.Getenv("EVAL_LLM_API_KEY")
	if *apiKey != "" {
		llmAPIKey = *apiKey
	}

	topK := cfg.Qdrant.TopK
	if *topKFlag > 0 {
		topK = *topKFlag
	}

	ctx := context.Background()

	embedder := pkb.NewOllamaEmbedder(cfg.Embedder.OllamaAddr, cfg.Embedder.Model, cfg.Embedder.Dimensions)

	store, err := pkb.NewQdrantStore(cfg.Qdrant.Addr, cfg.Qdrant.Collection)
	if err != nil {
		log.Fatalf("qdrant store: %v", err)
	}

	judge := pkb.NewLLMJudge(llmBaseURL, llmModel, llmAPIKey)

	f, err := os.OpenFile(*outPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("open output %s: %v", *outPath, err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	total, relevant := 0, 0

	for i, q := range queries {
		fmt.Printf("[%d/%d] %s\n", i+1, len(queries), q)

		vec, err := embedder.Embed(ctx, q)
		if err != nil {
			log.Printf("  embed error: %v — skip", err)
			continue
		}

		results, err := store.HybridSearch(ctx, vec, q, topK)
		if err != nil {
			log.Printf("  search error: %v — skip", err)
			continue
		}

		for rank, chunk := range results {
			rel, err := judge.IsRelevant(ctx, q, chunk)
			if err != nil {
				log.Printf("  judge error rank %d: %v", rank+1, err)
			}

			chunkID := fmt.Sprintf("%s:%d", chunk.FilePath, chunk.LineStart)
			qrel := pkb.Qrel{
				Query:    q,
				ChunkID:  chunkID,
				FilePath: chunk.FilePath,
				Relevant: rel,
			}
			if err := enc.Encode(qrel); err != nil {
				log.Fatalf("write qrel: %v", err)
			}

			total++
			if rel {
				relevant++
			}
			fmt.Printf("  rank %d  %-6v  %s\n", rank+1, rel, chunkID)
		}
	}

	fmt.Printf("\nDone. %d judgments, %d relevant (%.1f%%). Review %s then run make eval-qrels.\n",
		total, relevant, 100*float64(relevant)/float64(total), *outPath)
}
