package pkb

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"nadir/config"
)

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
	Recall          float64 `json:"recall,omitempty"`
	MAP             float64 `json:"map,omitempty"`
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

func (j *qrelsJudge) TotalRelevant(query string) int {
	var n int
	for _, rel := range j.qrels[query] {
		if rel {
			n++
		}
	}
	return n
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
func loadEvalCases(t *testing.T) []EvalCase {
	t.Helper()
	path := filepath.Join("testdata", "eval_queries.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("load eval queries %s: %v", path, err)
	}
	var cases []EvalCase
	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "" {
			continue
		}
		var c EvalCase
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

// tLogger adapts *testing.T to EvalLogger.
type tLogger struct{ t *testing.T }

func (l tLogger) Logf(format string, args ...any)   { l.t.Logf(format, args...) }
func (l tLogger) Errorf(format string, args ...any) { l.t.Errorf(format, args...) }

// runEval is a test-scoped wrapper around RunEval.
func runEval(
	t *testing.T,
	ctx context.Context,
	cases []EvalCase,
	embedder Embedder,
	store Store,
	reranker Reranker,
	judge RelevanceJudge,
	topK int,
	candidateMul int,
	hydeSearcher *HyDESearcher,
) EvalMetrics {
	t.Helper()
	return RunEval(ctx, cases, embedder, store, reranker, judge, topK, candidateMul, hydeSearcher, tLogger{t})
}
