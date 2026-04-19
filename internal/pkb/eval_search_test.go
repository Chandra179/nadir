package pkb

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
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
	Query string `json:"query"`
}

func loadEvalCases(t *testing.T) []evalCase {
	t.Helper()
	path := filepath.Join("testdata", "eval_queries.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("load eval queries %s: %v", path, err)
	}
	var cases []evalCase
	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "" {
			continue
		}
		var c evalCase
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			t.Fatalf("parse eval query line: %v", err)
		}
		cases = append(cases, c)
	}
	if len(cases) == 0 {
		t.Fatalf("no eval queries found in %s", path)
	}
	return cases
}

// evalMetrics holds computed IR metrics for one eval run.
type evalMetrics struct {
	MRR       float64
	HitRate   float64 // fraction of queries with at least one relevant result in top-K (Success@K)
	NDCG      float64
	Precision float64
}

// evalProfile defines one retrieval configuration to benchmark.
type evalProfile struct {
	Name         string `json:"name"`
	SparseScorer string `json:"sparse_scorer"` // "tf" | "splade"
	ChunkSize    int    `json:"chunk_size"`
	ChunkOverlap int    `json:"chunk_overlap"`
}

func loadEvalProfiles(t *testing.T) []evalProfile {
	t.Helper()
	path := filepath.Join("testdata", "eval_profiles.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("load eval profiles %s: %v", path, err)
	}
	var profiles []evalProfile
	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "" {
			continue
		}
		var p evalProfile
		if err := json.Unmarshal([]byte(line), &p); err != nil {
			t.Fatalf("parse eval profile line: %v", err)
		}
		profiles = append(profiles, p)
	}
	if len(profiles) == 0 {
		t.Fatalf("no eval profiles found in %s", path)
	}
	return profiles
}

// evalHistoryEntry is one run appended to the history file.
type evalHistoryEntry struct {
	Timestamp    string  `json:"timestamp"`
	Profile      string  `json:"profile"`
	SparseScorer string  `json:"sparse_scorer"`
	ChunkSize    int     `json:"chunk_size"`
	ChunkOverlap int     `json:"chunk_overlap"`
	Mode         string  `json:"mode"`
	Judge        string  `json:"judge"`
	Collection   string  `json:"collection"`
	Model        string  `json:"model"`
	Queries      int     `json:"queries"`
	TopK         int     `json:"top_k"`
	MRR          float64 `json:"mrr"`
	HitRate      float64 `json:"hit_rate"`
	NDCG         float64 `json:"ndcg"`
	Precision    float64 `json:"precision"`
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
	for line := range strings.SplitSeq(string(data), "\n") {
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

// buildStore selects the Qdrant backend based on EVAL_STORE:
//
//	EVAL_STORE=live      → connect to running Qdrant; skip ingest (data already there)
//	EVAL_STORE=container → spin up ephemeral testcontainer; ingest required
//
// Override addr/collection via EVAL_QDRANT_ADDR / EVAL_QDRANT_COLLECTION.
// Returns (store, skipIngest, modeName, collectionName).
func buildStore(t *testing.T, ctx context.Context, embedder Embedder, cfg *config.Config) (Store, bool, string, string) {
	t.Helper()

	evalStore := os.Getenv("EVAL_STORE")
	if evalStore == "" {
		evalStore = "live" // default: fast path, no Docker pull
	}

	switch evalStore {
	case "live":
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
		t.Logf("store=live qdrant=%s collection=%s", addr, collection)
		return store, true, "live", collection

	case "container":
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

		t.Log("store=container")
		return store, false, "container", "eval"

	default:
		t.Fatalf("unknown EVAL_STORE=%q — valid values: live, container", evalStore)
		return nil, false, "", ""
	}
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
	judge, judgeName := buildJudge(t, cfg)
	topK := cfg.Qdrant.TopK
	evalCases := loadEvalCases(t)
	profiles := loadEvalProfiles(t)

	for _, profile := range profiles {
		t.Run(profile.Name, func(t *testing.T) {
			if profile.SparseScorer == "splade" && !checkSPLADESidecar(cfg.SparseScorer.Addr) {
				t.Skipf("splade sidecar not reachable at %s — run: python cmd/splade/main.py", cfg.SparseScorer.Addr)
			}

			store, skipIngest, mode, collection := buildStore(t, ctx, embedder, cfg)

			var sparseEmb SparseEmbedder
			if profile.SparseScorer == "splade" {
				sparseEmb = NewSPLADESparseScorer(cfg.SparseScorer.Addr)
				if qs, ok := store.(*QdrantStore); ok {
					store = qs.WithSparseEmbedder(sparseEmb)
				}
			} else {
				scorer := buildSparseScorer(ctx, profile, cfg)
				if qs, ok := store.(*QdrantStore); ok {
					store = qs.WithSparseScorer(scorer)
				}
			}

			if !skipIngest {
				pipeline := NewPipeline(
					NewRecursiveChunker(profile.ChunkSize, profile.ChunkOverlap),
					embedder,
					store,
					PipelineConfig{
						MaxAttempts:     cfg.Retry.MaxAttempts,
						InitialInterval: cfg.Retry.InitialInterval,
						MaxInterval:     cfg.Retry.MaxInterval,
						Multiplier:      cfg.Retry.Multiplier,
					},
				)
				if sparseEmb != nil {
					pipeline.WithSparseEmbedder(sparseEmb)
				}

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

			metrics := runEval(t, ctx, evalCases, embedder, store, judge, topK)

			fmt.Printf("\n=== Search Eval: %s (model=%s topK=%d) ===\n", profile.Name, cfg.Embedder.Model, topK)
			fmt.Printf("SparseScorer:  %s\n", profile.SparseScorer)
			fmt.Printf("ChunkSize:     %d  ChunkOverlap: %d\n", profile.ChunkSize, profile.ChunkOverlap)
			fmt.Printf("Queries:       %d\n", len(evalCases))
			fmt.Printf("MRR@%d:          %.4f\n", topK, metrics.MRR)
			fmt.Printf("HitRate@%d:      %.4f\n", topK, metrics.HitRate)
			fmt.Printf("NDCG@%d:         %.4f\n", topK, metrics.NDCG)
			fmt.Printf("Precision@%d:    %.4f\n\n", topK, metrics.Precision)

			saveHistory(t, cfg, evalHistoryEntry{
				Timestamp:    time.Now().UTC().Format(time.RFC3339),
				Profile:      profile.Name,
				SparseScorer: profile.SparseScorer,
				ChunkSize:    profile.ChunkSize,
				ChunkOverlap: profile.ChunkOverlap,
				Mode:         mode,
				Judge:        judgeName,
				Collection:   collection,
				Model:        cfg.Embedder.Model,
				Queries:      len(evalCases),
				TopK:         topK,
				MRR:          metrics.MRR,
				HitRate:      metrics.HitRate,
				NDCG:         metrics.NDCG,
				Precision:    metrics.Precision,
			})
		})
	}
}

// buildSparseScorer returns TFSparseScorer for the client-side BM25 leg.
// SPLADE profiles use SparseEmbedder (server-side) and do not call this.
func buildSparseScorer(_ context.Context, profile evalProfile, cfg *config.Config) SparseScorer {
	_ = profile
	_ = cfg
	return TFSparseScorer{}
}

// checkSPLADESidecar returns false if sidecar is unreachable.
func checkSPLADESidecar(addr string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(addr + "/health")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
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

	var mrr, hitRate, ndcg, precision float64

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
			hitRate++
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
		HitRate:   hitRate / n,
		NDCG:      ndcg / n,
		Precision: precision / n,
	}
}
