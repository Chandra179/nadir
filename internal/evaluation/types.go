package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"nadir/internal/retrieval/search"
)

// RelevantChunk identifies one golden-relevant Retrieval result. File is a
// case-insensitive path-suffix match, with a base-name fallback so the same
// golden set works for host paths and container mounts; Contains is a
// case-insensitive substring match over the chunk and its contextual window.
type RelevantChunk struct {
	File     string `json:"file"`
	Contains string `json:"contains"`
}

type GoldenQuery struct {
	ID       string          `json:"id"`
	Query    string          `json:"query"`
	Relevant []RelevantChunk `json:"relevant"`
}

type GoldenSet struct {
	Queries []GoldenQuery `json:"queries"`
}

// LoadGoldenSet reads and validates a golden query set from JSON.
func LoadGoldenSet(path string) (*GoldenSet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read golden set: %w", err)
	}
	var gs GoldenSet
	if err := json.Unmarshal(data, &gs); err != nil {
		return nil, fmt.Errorf("parse golden set %s: %w", path, err)
	}
	if len(gs.Queries) == 0 {
		return nil, fmt.Errorf("golden set %s has no queries", path)
	}
	seenIDs := make(map[string]struct{}, len(gs.Queries))
	for i, q := range gs.Queries {
		if strings.TrimSpace(q.ID) == "" {
			return nil, fmt.Errorf("golden set query #%d has no id", i+1)
		}
		if _, exists := seenIDs[q.ID]; exists {
			return nil, fmt.Errorf("golden set query #%d (%q) duplicates an id", i+1, q.ID)
		}
		seenIDs[q.ID] = struct{}{}
		if strings.TrimSpace(q.Query) == "" || len(q.Relevant) == 0 {
			return nil, fmt.Errorf("golden set query #%d (%q) needs a query and at least one relevant entry", i+1, q.ID)
		}
		for j, relevant := range q.Relevant {
			if strings.TrimSpace(relevant.File) == "" && strings.TrimSpace(relevant.Contains) == "" {
				return nil, fmt.Errorf("golden set query %q relevant entry #%d needs file or contains", q.ID, j+1)
			}
		}
	}
	return &gs, nil
}

// MatchedRelevant returns the indices of golden entries matched by chunk.
func MatchedRelevant(chunk search.Chunk, relevant []RelevantChunk) []int {
	haystack := strings.ToLower(chunk.Text + "\n" + chunk.WindowText)
	filePath := strings.ToLower(chunk.FilePath)
	matched := make([]int, 0, len(relevant))
	for i, want := range relevant {
		if want.File != "" && !matchesFile(filePath, want.File) {
			continue
		}
		if want.Contains != "" && !strings.Contains(haystack, strings.ToLower(want.Contains)) {
			continue
		}
		matched = append(matched, i)
	}
	return matched
}

func matchesFile(actual, expected string) bool {
	actual = strings.ToLower(filepath.ToSlash(actual))
	expected = strings.ToLower(filepath.ToSlash(expected))
	return strings.HasSuffix(actual, expected) || filepath.Base(actual) == filepath.Base(expected)
}

type QueryResult struct {
	ID            string    `json:"id"`
	Query         string    `json:"query"`
	NumRelevant   int       `json:"num_relevant"`
	Hits          []bool    `json:"hits"`
	FirstHitRank  int       `json:"first_hit_rank"`
	RelevantFound int       `json:"relevant_found"`
	LatencyMS     float64   `json:"latency_ms"`
	Latencies     []float64 `json:"latencies"`
}

type Aggregate struct {
	Queries    int     `json:"queries"`
	TopK       int     `json:"top_k"`
	HitRateAtK float64 `json:"hit_rate_at_k"`
	RecallAtK  float64 `json:"recall_at_k"`
	MRRAt10    float64 `json:"mrr_at_10"`
	NDCGAtK    float64 `json:"ndcg_at_k"`
	P50LatMS   float64 `json:"p50_latency_ms"`
	P95LatMS   float64 `json:"p95_latency_ms"`
}

type Report struct {
	Timestamp string        `json:"timestamp"`
	TopK      int           `json:"top_k"`
	Rerank    bool          `json:"reranker_enabled"`
	PerQuery  []QueryResult `json:"per_query"`
	Aggregate Aggregate     `json:"aggregate"`
}
