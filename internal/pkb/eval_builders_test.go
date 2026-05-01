package pkb

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"nadir/config"
)

// buildJudge selects the relevance judge from env:
//
//	EVAL_JUDGE=llm   → LLMJudge (base URL + model from config; override: EVAL_LLM_BASE_URL, EVAL_LLM_MODEL)
//	default          → qrelsJudge from testdata/qrels.jsonl (override: EVAL_QRELS_PATH)
func buildJudge(t *testing.T, evalCfg *config.EvalConfig) (RelevanceJudge, string) {
	t.Helper()

	if os.Getenv("EVAL_JUDGE") == "llm" {
		baseURL := os.Getenv("EVAL_LLM_BASE_URL")
		if baseURL == "" {
			baseURL = evalCfg.LLMBaseURL
		}
		model := os.Getenv("EVAL_LLM_MODEL")
		if model == "" {
			model = evalCfg.LLMModel
		}
		apiKey := os.Getenv("EVAL_LLM_API_KEY")
		t.Logf("judge=llm base=%s model=%s", baseURL, model)
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

// buildStore selects the Qdrant backend from EVAL_STORE:
//
//	live      → connect to running Qdrant; skip ingest (data already there)
//	container → shared ephemeral testcontainer; ingest runs once across all profiles
//
// Override addr/collection via EVAL_QDRANT_ADDR / EVAL_QDRANT_COLLECTION.
// Returns (store, docsIngested, modeName, collectionName).
func buildStore(t *testing.T, ctx context.Context, embedder Embedder, cfg *config.Config) (Store, int, string, string) {
	t.Helper()

	mode := os.Getenv("EVAL_STORE")
	if mode == "" {
		mode = "live"
	}

	switch mode {
	case "live":
		addr := os.Getenv("EVAL_QDRANT_ADDR")
		if addr == "" {
			addr = cfg.Qdrant.Addr
		}
		collection := os.Getenv("EVAL_QDRANT_COLLECTION")
		if collection == "" {
			collection = cfg.Qdrant.Collection
		}
		store, err := NewQdrantStore(addr, collection, 0)
		if err != nil {
			t.Fatalf("live qdrant store: %v", err)
		}
		t.Logf("store=live qdrant=%s collection=%s", addr, collection)
		return store, 0, "live", collection

	case "container":
		store, docsIngested, err := getSharedContainerStore(embedder, cfg)
		if err != nil {
			t.Fatalf("shared container store: %v", err)
		}
		t.Logf("store=container (shared) docs=%d", docsIngested)
		return store, docsIngested, "container", "eval"

	default:
		t.Fatalf("unknown EVAL_STORE=%q — valid: live, container", mode)
		return nil, 0, "", ""
	}
}

// buildChunker returns the Chunker for a profile, falling back to config.yaml defaults.
//
//	"recursive"       → RecursiveChunker (chunk_size / chunk_overlap from profile)
//	"sentence-window" → SentenceWindowChunker (window_size from config)
func buildChunker(profile evalProfile, cfg *config.Config) Chunker {
	provider := profile.ChunkerProvider
	if provider == "" {
		provider = cfg.Chunker.Provider
	}
	switch provider {
	case "sentence-window":
		return NewSentenceWindowChunker(cfg.Chunker.WindowSize)
	default: // "recursive" or unset
		return NewRecursiveChunker(profile.ChunkSize, profile.ChunkOverlap)
	}
}

// buildSparseScorer returns the TFSparseScorer for the client-side BM25 leg.
// SPLADE profiles use SparseEmbedder (server-side) and never call this.
func buildSparseScorer(_ context.Context, _ evalProfile, _ *config.Config) SparseScorer {
	return TFSparseScorer{}
}

// buildReranker returns (name, Reranker) for the profile.
// Returns ("", nil) when profile.Reranker is empty. Skips the subtest when the sidecar is unreachable.
func buildReranker(t *testing.T, profile evalProfile, cfg *config.Config) (string, Reranker) {
	t.Helper()
	if profile.Reranker == "" {
		return "", nil
	}
	addr := cfg.Reranker.Addr
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(addr + "/health")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Skipf("reranker sidecar not reachable at %s — run: python cmd/reranker/main.py", addr)
	}
	resp.Body.Close()
	t.Logf("reranker=cross-encoder addr=%s", addr)
	return "cross-encoder", NewHTTPReranker(addr)
}

// buildHyDE returns a HyDESearchInterface for the profile, or nil when HyDE is not enabled.
// Skips the subtest when the Ollama LLM is not reachable.
func buildHyDE(t *testing.T, profile evalProfile, cfg *config.Config, embedder Embedder, store Store) HyDESearchInterface {
	t.Helper()
	if !profile.HyDE && !profile.AdaptiveHyDE && !profile.MultiHyDE {
		return nil
	}
	ollamaAddr := cfg.HyDE.OllamaAddr
	if ollamaAddr == "" {
		ollamaAddr = cfg.Embedder.OllamaAddr
	}
	model := cfg.HyDE.Model
	numDocs := profile.HyDENumDocs
	if numDocs <= 0 {
		numDocs = cfg.HyDE.NumDocs
	}
	if numDocs <= 0 {
		numDocs = 1
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(ollamaAddr + "/api/tags")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Skipf("ollama not reachable at %s — required for hyde profile", ollamaAddr)
	}
	resp.Body.Close()

	baseGen := NewOllamaHyDEGenerator(ollamaAddr, model)
	var gen HyDEGenerator = baseGen
	if profile.MultiHyDE {
		gen = NewMultiPromptHyDEGenerator(baseGen)
		t.Logf("multi-hyde=enabled model=%s num_docs=%d", model, numDocs)
	}

	hydeSearcher := NewHyDESearcher(gen, embedder, store, numDocs)
	if profile.AdaptiveHyDE {
		thresh := profile.AdaptiveThresh
		if thresh <= 0 {
			thresh = cfg.HyDE.AdaptiveThresh
		}
		t.Logf("adaptive-hyde=enabled model=%s threshold=%.2f", model, thresh)
		return NewAdaptiveHyDESearcher(hydeSearcher, embedder, store, thresh)
	}
	if !profile.MultiHyDE {
		t.Logf("hyde=enabled model=%s num_docs=%d", model, numDocs)
	}
	return hydeSearcher
}

// checkSPLADESidecar returns false when the SPLADE sidecar is unreachable.
func checkSPLADESidecar(addr string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(addr + "/health")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
