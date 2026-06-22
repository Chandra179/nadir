package engine

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
	Upsert(ctx context.Context, chunks []ScoredChunk) error
	DeleteByFile(ctx context.Context, filePath string) error
	Search(ctx context.Context, vector []float32, topK int) ([]ScoredChunk, error)
	HybridSearch(ctx context.Context, vector []float32, query string, topK int, filter *SearchFilter) ([]ScoredChunk, error)
	KeywordSearch(ctx context.Context, keyword string, topK int, filter *SearchFilter) ([]ScoredChunk, error)
	GetAllFileSHAs(ctx context.Context) (map[string]string, error)
}
