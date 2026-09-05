package store

import "strconv"

type ScoredChunk struct {
	Text       string
	WindowText string
	FilePath   string
	Header     string
	LineStart  int
	ChunkIndex int
	Vector     []float32
	SourceSHA  string
	IngestedAt string
	Score      float32

	// SparseText overrides the text used to build the BM25 sparse vector.
	// Empty falls back to the contextual "path > header\ntext" form. Ingest
	// sets it so both retrieval legs index identical context.
	SparseText string

	// HypeQuestion/HypeIndex mark a HyPE sibling point: an embedded
	// hypothetical question sharing its parent chunk's identity fields, so
	// search-side Key() dedup collapses siblings onto the parent while the
	// point ID stays unique.
	HypeQuestion string
	HypeIndex    int
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
