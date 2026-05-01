package evalops

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

// TraceRecord is one sampled search event persisted to JSONL.
type TraceRecord struct {
	Timestamp        time.Time   `json:"ts"`
	Query            string      `json:"query"`
	Chunks           []ChunkSnap `json:"chunks"`
	ContextRelevance float64     `json:"context_relevance"`
}

// ChunkSnap is a minimal chunk snapshot stored per trace (avoids large text blobs).
type ChunkSnap struct {
	FilePath  string  `json:"file_path"`
	Header    string  `json:"header"`
	LineStart int     `json:"line_start"`
	Score     float32 `json:"score"`
	Relevant  *bool   `json:"relevant,omitempty"` // nil until judged
}

// TraceStore appends TraceRecord entries to a JSONL file.
// All writes are serialized by an internal mutex.
type TraceStore struct {
	path string
	mu   sync.Mutex
}

func NewTraceStore(path string) *TraceStore {
	return &TraceStore{path: path}
}

func (ts *TraceStore) Append(rec TraceRecord) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	f, err := os.OpenFile(ts.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	return enc.Encode(rec)
}
