package pkb

import "context"

// ScoredChunk is a retrieved chunk with its similarity score.
type ScoredChunk struct {
	DocumentChunk
	Vector        []float32  // dense vector; populated during ingest
	SparseIndices []uint32   // sparse vector indices; populated during ingest when SparseEmbedder is set
	SparseValues  []float32  // sparse vector values; parallel to SparseIndices
	SourceSHA     string
	Score         float32 // populated by Store.Search; zero during ingest
}

// SearchFilter restricts retrieval to chunks matching payload fields.
// All non-empty fields are ANDed together.
type SearchFilter struct {
	FilePath  string `json:"file_path,omitempty"`
	Header    string `json:"header,omitempty"`
	SourceSHA string `json:"source_sha,omitempty"`
}

// Store persists and retrieves chunk vectors.
type Store interface {
	// Upsert inserts or replaces chunks. Each ScoredChunk must have Vector set.
	Upsert(ctx context.Context, chunks []ScoredChunk) error
	// DeleteByFile removes all chunks belonging to a file path.
	DeleteByFile(ctx context.Context, filePath string) error
	// Search returns the top-k most similar chunks for a query vector.
	Search(ctx context.Context, vector []float32, topK int) ([]ScoredChunk, error)
	// HybridSearch combines dense vector search with BM25 full-text search via RRF.
	// filter may be nil (no restriction).
	HybridSearch(ctx context.Context, vector []float32, query string, topK int, filter *SearchFilter) ([]ScoredChunk, error)
	// KeywordSearch filters by full-text match on the text payload field. No vector required.
	// filter may be nil (no restriction).
	KeywordSearch(ctx context.Context, keyword string, topK int, filter *SearchFilter) ([]ScoredChunk, error)
	// GetFileSHA returns the stored source_sha for a file, or "" if not found.
	GetFileSHA(ctx context.Context, filePath string) (string, error)
	// GetAllFileSHAs returns a map of filePath → source_sha for all indexed files.
	// Single RPC; use at ingest time instead of N GetFileSHA calls.
	GetAllFileSHAs(ctx context.Context) (map[string]string, error)
}

// StoreAdmin handles collection lifecycle. Separate from Store so handlers
// only receive query/write capability, not schema mutation.
type StoreAdmin interface {
	EnsureCollection(ctx context.Context, dimensions int) error
}
