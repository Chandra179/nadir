package pkb

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nadir/config"

	qdrantcontainer "github.com/testcontainers/testcontainers-go/modules/qdrant"
)

// evalCase is one query for search evaluation.
type evalCase struct {
	Query string
}

var evalCases = []evalCase{
	{Query: "How does the Go GPM scheduler work with goroutines and OS threads?"},
	{Query: "What causes goroutine leaks and how do you prevent them?"},
	{Query: "How do Go channels work internally with sendq and recvq?"},
	{Query: "What changed in Go 1.22 loop variable scoping semantics?"},
	{Query: "How does strings.Builder improve concatenation performance?"},
	{Query: "How does consistent hashing minimize data movement when adding servers?"},
	{Query: "What is the Snowflake ID structure and how does it handle clock skew?"},
	{Query: "How does rate limiting work with Redis and what is thundering herd jitter protection?"},
	{Query: "How does cache stampede prevention work with request coalescing?"},
	{Query: "How do virtual nodes in consistent hashing improve load distribution?"},
	{Query: "What is the difference between B-Tree and LSM Tree storage engines for databases?"},
	{Query: "How do ACID transactions handle isolation levels?"},
	{Query: "What is the N+1 query problem and how do you solve it?"},
	{Query: "How does Kafka partition routing work for producers?"},
	{Query: "What is the CPU fetch-execute cycle?"},
	{Query: "How does gradient descent minimize the loss function and how does backpropagation calculate gradients?"},
	{Query: "What are activation functions like sigmoid tanh and ReLU and when should you use each?"},
	{Query: "How does dropout regularization prevent overfitting during neural network training?"},
	{Query: "What is Mixture of Experts MoE and how does the router select expert sub-networks?"},
	{Query: "How do transformers and attention mechanisms work for sequence to sequence tasks?"},
}

// evalMetrics holds computed IR metrics for one eval run.
type evalMetrics struct {
	MRR       float64
	Recall    float64
	NDCG      float64
	Precision float64
}

// evalHistoryEntry is one run appended to the history file.
type evalHistoryEntry struct {
	Timestamp  string  `json:"timestamp"`
	Mode       string  `json:"mode"`
	Judge      string  `json:"judge"`
	Collection string  `json:"collection"`
	Model      string  `json:"model"`
	Queries    int     `json:"queries"`
	TopK       int     `json:"top_k"`
	MRR        float64 `json:"mrr"`
	Recall     float64 `json:"recall"`
	NDCG       float64 `json:"ndcg"`
	Precision  float64 `json:"precision"`
}

// loadConfig loads config/config.yaml relative to the repo root (two levels up from internal/pkb/).
func loadConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load(filepath.Join("..", "..", "config", "config.yaml"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return cfg
}

// qrelsJudge wraps a pre-computed qrels file as a RelevanceJudge.
// Returns false for any chunk not present in the qrels file.
type qrelsJudge struct {
	// qrels[query][chunkID] = true/false
	qrels map[string]map[string]bool
}

func loadQrelsJudge(path string) (*qrelsJudge, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	m := make(map[string]map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var q Qrel
		if err := json.Unmarshal([]byte(line), &q); err != nil {
			return nil, fmt.Errorf("parse qrel line: %w", err)
		}
		if m[q.Query] == nil {
			m[q.Query] = make(map[string]bool)
		}
		m[q.Query][q.ChunkID] = q.Relevant
	}
	return &qrelsJudge{qrels: m}, nil
}

func (j *qrelsJudge) IsRelevant(_ context.Context, query string, chunk ScoredChunk) (bool, error) {
	entries, ok := j.qrels[query]
	if !ok {
		return false, nil
	}
	chunkID := fmt.Sprintf("%s:%d", chunk.FilePath, chunk.LineStart)
	return entries[chunkID], nil
}

// buildJudge selects the relevance judge based on env vars:
//
//	EVAL_JUDGE=llm   → LLMJudge; base URL + model read from config (override: EVAL_LLM_BASE_URL, EVAL_LLM_MODEL)
//	default          → qrels judge from testdata/qrels.jsonl (override: EVAL_QRELS_PATH)
func buildJudge(t *testing.T, cfg *config.Config) (RelevanceJudge, string) {
	t.Helper()

	if os.Getenv("EVAL_JUDGE") == "llm" {
		baseURL := os.Getenv("EVAL_LLM_BASE_URL")
		if baseURL == "" {
			baseURL = cfg.Eval.LLMBaseURL
		}
		model := os.Getenv("EVAL_LLM_MODEL")
		if model == "" {
			model = cfg.Eval.LLMModel
		}
		apiKey := os.Getenv("EVAL_LLM_API_KEY")
		t.Logf("judge=LLM base=%s model=%s", baseURL, model)
		return NewLLMJudge(baseURL, model, apiKey), "llm"
	}

	qrelsPath := os.Getenv("EVAL_QRELS_PATH")
	if qrelsPath == "" {
		qrelsPath = filepath.Join("testdata", "qrels.jsonl")
	}
	j, err := loadQrelsJudge(qrelsPath)
	if err != nil {
		t.Fatalf("load qrels %s: %v", qrelsPath, err)
	}
	t.Logf("judge=qrels path=%s", qrelsPath)
	return j, "qrels"
}

// buildStore returns either a live Qdrant store (EVAL_MODE=live) or spins up a testcontainer.
// Live mode skips ingestion and uses the existing collection.
func buildStore(t *testing.T, ctx context.Context, embedder Embedder, cfg *config.Config) (Store, bool, string, string) {
	t.Helper()

	if os.Getenv("EVAL_MODE") == "live" {
		addr := os.Getenv("EVAL_QDRANT_ADDR")
		if addr == "" {
			addr = cfg.Qdrant.Addr
		}
		collection := os.Getenv("EVAL_QDRANT_COLLECTION")
		if collection == "" {
			collection = cfg.Qdrant.Collection
		}
		store, err := NewQdrantStore(addr, collection)
		if err != nil {
			t.Fatalf("live qdrant store: %v", err)
		}
		t.Logf("mode=live qdrant=%s collection=%s", addr, collection)
		return store, true, "live", collection
	}

	container, err := qdrantcontainer.Run(ctx, "qdrant/qdrant:latest")
	if err != nil {
		t.Fatalf("start qdrant container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	grpcEndpoint, err := container.GRPCEndpoint(ctx)
	if err != nil {
		t.Fatalf("qdrant grpc endpoint: %v", err)
	}

	store, err := NewQdrantStore(grpcEndpoint, "eval")
	if err != nil {
		t.Fatalf("new qdrant store: %v", err)
	}

	if err := store.EnsureCollection(ctx, embedder.Dimensions()); err != nil {
		t.Fatalf("ensure collection: %v", err)
	}

	t.Log("mode=container")
	return store, false, "container", "eval"
}

// saveHistory appends a run result to the history file (JSONL).
func saveHistory(t *testing.T, cfg *config.Config, entry evalHistoryEntry) {
	t.Helper()
	histPath := cfg.Eval.HistoryPath
	if histPath == "" {
		return
	}
	// Resolve relative to repo root (two levels up from internal/pkb/).
	if !filepath.IsAbs(histPath) {
		histPath = filepath.Join("..", "..", histPath)
	}
	f, err := os.OpenFile(histPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Logf("warn: open history file %s: %v", histPath, err)
		return
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(entry); err != nil {
		t.Logf("warn: write history: %v", err)
	}
}

func TestSearchEval(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping eval: requires Docker + Ollama")
	}

	cfg := loadConfig(t)

	ollamaAddr := os.Getenv("OLLAMA_ADDR")
	if ollamaAddr == "" {
		ollamaAddr = cfg.Embedder.OllamaAddr
	}

	ctx := context.Background()

	embedder := NewOllamaEmbedder(ollamaAddr, cfg.Embedder.Model, cfg.Embedder.Dimensions)

	store, skipIngest, mode, collection := buildStore(t, ctx, embedder, cfg)
	judge, judgeName := buildJudge(t, cfg)

	if !skipIngest {
		pipeline := NewPipeline(
			NewRecursiveChunker(cfg.Chunker.ChunkSize, cfg.Chunker.ChunkOverlap),
			embedder,
			store,
			PipelineConfig{
				MaxAttempts:     cfg.Retry.MaxAttempts,
				InitialInterval: cfg.Retry.InitialInterval,
				MaxInterval:     cfg.Retry.MaxInterval,
				Multiplier:      cfg.Retry.Multiplier,
			},
		)

		gitbookRoot := filepath.Join("..", "..", "gitbook")
		ingestedFiles := 0
		err := filepath.Walk(gitbookRoot, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() || !strings.HasSuffix(info.Name(), ".md") {
				return nil
			}
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}
			rel, _ := filepath.Rel(gitbookRoot, path)
			if ingestErr := pipeline.Ingest(ctx, rel, string(content), "eval"); ingestErr != nil {
				t.Logf("warn: ingest %s: %v", rel, ingestErr)
			} else {
				ingestedFiles++
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk gitbook: %v", err)
		}
		t.Logf("ingested %d files", ingestedFiles)
	}

	topK := cfg.Qdrant.TopK
	metrics := runEval(t, ctx, evalCases, embedder, store, judge, topK)

	fmt.Printf("\n=== Search Eval Results (%s, topK=%d) ===\n", cfg.Embedder.Model, topK)
	fmt.Printf("Queries:      %d\n", len(evalCases))
	fmt.Printf("MRR@%d:        %.4f\n", topK, metrics.MRR)
	fmt.Printf("Recall@%d:     %.4f\n", topK, metrics.Recall)
	fmt.Printf("NDCG@%d:       %.4f\n", topK, metrics.NDCG)
	fmt.Printf("Precision@%d:  %.4f\n", topK, metrics.Precision)
	fmt.Printf("Ollama:       %s\n\n", ollamaAddr)

	t.Logf("MRR@%d=%.4f Recall@%d=%.4f NDCG@%d=%.4f Precision@%d=%.4f",
		topK, metrics.MRR, topK, metrics.Recall, topK, metrics.NDCG, topK, metrics.Precision)

	saveHistory(t, cfg, evalHistoryEntry{
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		Mode:       mode,
		Judge:      judgeName,
		Collection: collection,
		Model:      cfg.Embedder.Model,
		Queries:    len(evalCases),
		TopK:       topK,
		MRR:        metrics.MRR,
		Recall:     metrics.Recall,
		NDCG:       metrics.NDCG,
		Precision:  metrics.Precision,
	})
}

func runEval(
	t *testing.T,
	ctx context.Context,
	cases []evalCase,
	embedder Embedder,
	store Store,
	judge RelevanceJudge,
	topK int,
) evalMetrics {
	t.Helper()

	var mrr, recall, ndcg, precision float64

	for _, tc := range cases {
		vec, err := embedder.Embed(ctx, tc.Query)
		if err != nil {
			t.Errorf("embed query %q: %v", tc.Query, err)
			continue
		}
		results, err := store.HybridSearch(ctx, vec, tc.Query, topK)
		if err != nil {
			t.Errorf("search %q: %v", tc.Query, err)
			continue
		}

		firstRank := 0
		anyRelevant := false
		relevantCount := 0
		dcg := 0.0

		for rank, r := range results {
			rel, err := judge.IsRelevant(ctx, tc.Query, r)
			if err != nil {
				t.Logf("warn: judge %q chunk %s:%d: %v", tc.Query, r.FilePath, r.LineStart, err)
			}
			if rel {
				if firstRank == 0 {
					firstRank = rank + 1
				}
				anyRelevant = true
				relevantCount++
				dcg += 1.0 / math.Log2(float64(rank+2))
			}
		}

		if firstRank > 0 {
			mrr += 1.0 / float64(firstRank)
		}
		if anyRelevant {
			recall++
		}
		precision += float64(relevantCount) / float64(topK)

		idcg := 0.0
		for i := 0; i < relevantCount && i < topK; i++ {
			idcg += 1.0 / math.Log2(float64(i+2))
		}
		if idcg > 0 {
			ndcg += dcg / idcg
		}
	}

	n := float64(len(cases))
	return evalMetrics{
		MRR:       mrr / n,
		Recall:    recall / n,
		NDCG:      ndcg / n,
		Precision: precision / n,
	}
}
