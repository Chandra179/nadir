package pkb

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"nadir/config"
)

// evalCase is one query in the evaluation set.
type evalCase struct {
	Query string `json:"query"`
}

// evalMetrics holds IR metrics for one eval run.
type evalMetrics struct {
	MRR       float64
	HitRate   float64 // Success@K: fraction of queries with ≥1 relevant result
	NDCG      float64
	Precision float64
}

// evalProfile defines one retrieval configuration to benchmark.
// Fields not set in the JSONL file inherit config.yaml defaults at runtime.
type evalProfile struct {
	Name            string `json:"name"`
	SparseScorer    string `json:"sparse_scorer"` // "tf" | "splade"
	Reranker        string `json:"reranker"`      // "" | "cross-encoder"
	ChunkSize       int    `json:"chunk_size"`
	ChunkOverlap    int    `json:"chunk_overlap"`
	ChunkerProvider string `json:"chunker_provider"` // "recursive" | "sentence-window"; default from config
	HyDE            bool   `json:"hyde"`             // true = use HyDE search (LLM generates hypothetical doc per query)
	HyDENumDocs     int    `json:"hyde_num_docs"`    // 0 → default from config (1)
}

// hydeSearchFn wraps HyDESearcher.Search to match the runEval search hook signature.
type hydeSearchFn func(ctx context.Context, query string, topK int) ([]ScoredChunk, error)

// evalHistoryEntry is one run record appended to the JSONL history file.
type evalHistoryEntry struct {
	Timestamp       string  `json:"timestamp"`
	Profile         string  `json:"profile"`
	SparseScorer    string  `json:"sparse_scorer"`
	ChunkSize       int     `json:"chunk_size"`
	ChunkOverlap    int     `json:"chunk_overlap"`
	ChunkerProvider string  `json:"chunker_provider"`
	Mode            string  `json:"mode"`
	Judge           string  `json:"judge"`
	Collection      string  `json:"collection"`
	Model           string  `json:"model"`
	EmbedderDims    int     `json:"embedder_dims"`
	Queries         int     `json:"queries"`
	TopK            int     `json:"top_k"`
	DocsIngested    int     `json:"docs_ingested"`
	VectorCount     int64   `json:"vector_count"`
	QrelsTotal      int     `json:"qrels_total,omitempty"`
	QrelsRelevant   int     `json:"qrels_relevant,omitempty"`
	Reranker        string  `json:"reranker,omitempty"`
	CandidateMul    int     `json:"candidate_mul,omitempty"`
	HyDE            bool    `json:"hyde,omitempty"`
	MRR             float64 `json:"mrr"`
	HitRate         float64 `json:"hit_rate"`
	NDCG            float64 `json:"ndcg"`
	Precision       float64 `json:"precision"`
}

// evalHistoryWriter serializes history entries to a JSONL file, safe for concurrent subtests.
type evalHistoryWriter struct {
	mu   sync.Mutex
	file *os.File
}

func newEvalHistoryWriter(t *testing.T, evalCfg *config.EvalConfig) *evalHistoryWriter {
	t.Helper()
	histPath := evalCfg.HistoryPath
	if histPath == "" {
		return &evalHistoryWriter{}
	}
	if !filepath.IsAbs(histPath) {
		histPath = filepath.Join("..", "..", histPath)
	}
	f, err := os.OpenFile(histPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Logf("warn: open history file %s: %v", histPath, err)
		return &evalHistoryWriter{}
	}
	t.Cleanup(func() { _ = f.Close() })
	return &evalHistoryWriter{file: f}
}

func (w *evalHistoryWriter) write(t *testing.T, entry evalHistoryEntry) {
	t.Helper()
	if w.file == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := json.NewEncoder(w.file).Encode(entry); err != nil {
		t.Logf("warn: write history: %v", err)
	}
}

// qrelsJudge wraps a pre-computed qrels file as a RelevanceJudge.
// Returns false for any chunk not present in the file.
type qrelsJudge struct {
	// qrels[query][chunkID] = relevant
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
			return nil, err
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
	return entries[chunk.Key()], nil
}

// qrelsStats returns (total, relevant) counts from a qrelsJudge.
func qrelsStats(judge RelevanceJudge) (total, relevant int) {
	qj, ok := judge.(*qrelsJudge)
	if !ok {
		return
	}
	for _, entries := range qj.qrels {
		for _, rel := range entries {
			total++
			if rel {
				relevant++
			}
		}
	}
	return
}

// loadEvalCases reads eval queries from testdata/eval_queries.jsonl.
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
			t.Fatalf("parse eval query: %v", err)
		}
		cases = append(cases, c)
	}
	if len(cases) == 0 {
		t.Fatalf("no eval queries in %s", path)
	}
	return cases
}

// loadEvalProfiles reads retrieval profiles from testdata/eval_profiles.jsonl.
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
			t.Fatalf("parse eval profile: %v", err)
		}
		profiles = append(profiles, p)
	}
	if len(profiles) == 0 {
		t.Fatalf("no eval profiles in %s", path)
	}
	return profiles
}

// loadConfig loads config/config.yaml relative to the repo root (two dirs up from internal/pkb/).
func loadConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load(filepath.Join("..", "..", "config", "config.yaml"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return cfg
}

// loadEvalConfig loads only the eval section of config.yaml.
func loadEvalConfig(t *testing.T) *config.EvalConfig {
	t.Helper()
	evalCfg, err := config.LoadEval(filepath.Join("..", "..", "config", "config.yaml"))
	if err != nil {
		t.Fatalf("load eval config: %v", err)
	}
	return evalCfg
}

// runEval executes all queries concurrently (up to evalWorkers goroutines) and
// returns aggregated IR metrics. candidateMul controls oversampling when reranking.
const evalWorkers = 8

func runEval(
	t *testing.T,
	ctx context.Context,
	cases []evalCase,
	embedder Embedder,
	store Store,
	reranker Reranker,
	judge RelevanceJudge,
	topK int,
	candidateMul int,
	hydeSearcher *HyDESearcher,
) evalMetrics {
	t.Helper()

	if candidateMul <= 0 {
		candidateMul = 3
	}

	type result struct {
		reciprocalRank float64
		hit            bool
		precisionAtK   float64
		ndcgAtK        float64
	}

	results := make([]result, len(cases))
	sem := make(chan struct{}, evalWorkers)
	var wg sync.WaitGroup

	for i, tc := range cases {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, tc evalCase) {
			defer wg.Done()
			defer func() { <-sem }()

			fetchK := topK
			if reranker != nil {
				fetchK = topK * candidateMul
			}

			var hits []ScoredChunk
			var err error
			if hydeSearcher != nil {
				hits, err = hydeSearcher.Search(ctx, tc.Query, fetchK)
				if err != nil {
					t.Logf("hyde search %q failed, falling back: %v", tc.Query, err)
					hits, err = nil, nil
				}
			}
			if hits == nil {
				var vec []float32
				vec, err = embedder.Embed(ctx, tc.Query)
				if err != nil {
					t.Errorf("embed query %q: %v", tc.Query, err)
					return
				}
				hits, err = store.HybridSearch(ctx, vec, tc.Query, fetchK)
				if err != nil {
					t.Errorf("search %q: %v", tc.Query, err)
					return
				}
			}

			if reranker != nil {
				hits, err = reranker.Rerank(ctx, tc.Query, hits)
				if err != nil {
					t.Logf("warn: rerank %q: %v", tc.Query, err)
				}
				if len(hits) > topK {
					hits = hits[:topK]
				}
			}

			var firstRank, relevantCount int
			dcg := 0.0
			for rank, r := range hits {
				rel, err := judge.IsRelevant(ctx, tc.Query, r)
				if err != nil {
					t.Logf("warn: judge %q chunk %s:%d: %v", tc.Query, r.FilePath, r.LineStart, err)
				}
				if rel {
					if firstRank == 0 {
						firstRank = rank + 1
					}
					relevantCount++
					dcg += 1.0 / math.Log2(float64(rank+2))
				}
			}

			res := result{precisionAtK: float64(relevantCount) / float64(topK)}
			if firstRank > 0 {
				res.reciprocalRank = 1.0 / float64(firstRank)
				res.hit = true
			}
			idcg := 0.0
			for i := 0; i < relevantCount && i < topK; i++ {
				idcg += 1.0 / math.Log2(float64(i+2))
			}
			if idcg > 0 {
				res.ndcgAtK = dcg / idcg
			}
			results[i] = res
		}(i, tc)
	}
	wg.Wait()

	var mrr, hitRate, ndcg, precision float64
	for _, r := range results {
		mrr += r.reciprocalRank
		if r.hit {
			hitRate++
		}
		ndcg += r.ndcgAtK
		precision += r.precisionAtK
	}
	n := float64(len(cases))
	return evalMetrics{
		MRR:       mrr / n,
		HitRate:   hitRate / n,
		NDCG:      ndcg / n,
		Precision: precision / n,
	}
}
