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
//	-queries  path to eval queries JSONL (default: internal/pkb/testdata/eval_queries.jsonl)
//	-out      output qrels file   (default: internal/pkb/testdata/qrels.jsonl)
//	-top-k    results per query   (default: from config)
//	-base-url LLM base URL        (overrides config eval.llm_base_url)
//	-model    LLM model           (overrides config eval.llm_model)
//	-api-key  LLM API key         (overrides EVAL_LLM_API_KEY env)
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"nadir/config"
	"nadir/internal/pkb"
)

func loadQueries(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var queries []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry struct {
			Query string `json:"query"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return nil, fmt.Errorf("parse query line %q: %w", line, err)
		}
		queries = append(queries, entry.Query)
	}
	return queries, scanner.Err()
}

func main() {
	configPath := flag.String("config", "config/config.yaml", "path to config.yaml")
	queriesPath := flag.String("queries", "internal/pkb/testdata/eval_queries.jsonl", "path to eval queries JSONL")
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

	queries, err := loadQueries(*queriesPath)
	if err != nil {
		log.Fatalf("load queries %s: %v", *queriesPath, err)
	}
	log.Printf("loaded %d queries from %s", len(queries), *queriesPath)

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
