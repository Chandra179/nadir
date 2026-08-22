package store

import (
	"context"
	"strconv"
)

type ScoredChunk struct {
	Text       string
	WindowText string
	FilePath   string
	Header     string
	LineStart  int
	ChunkIndex int
	Vector     []float32
	SourceSHA  string
	Score      float32
}

func (s ScoredChunk) Key() string {
	return s.FilePath + ":" + strconv.Itoa(s.LineStart)
}

type SearchFilter struct {
	FilePath  string `json:"file_path,omitempty"`
	Header    string `json:"header,omitempty"`
	SourceSHA string `json:"source_sha,omitempty"`
}

// Stats is a lightweight snapshot of collection size, used for dashboard
// display. Documents counts distinct file_path/source_sha pairs; Chunks is
// the raw point count in the collection.
type Stats struct {
	Documents int
	Chunks    int
}

type Store interface {
	Upsert(ctx context.Context, chunks []ScoredChunk) error
	DeleteByFile(ctx context.Context, filePath string) error
	HybridSearch(ctx context.Context, vector []float32, query string, topK int, filter *SearchFilter) ([]ScoredChunk, error)
	KeywordSearch(ctx context.Context, keyword string, topK int, filter *SearchFilter) ([]ScoredChunk, error)
	GetAllFileSHAs(ctx context.Context) (map[string]string, error)
	Stats(ctx context.Context) (Stats, error)
}
