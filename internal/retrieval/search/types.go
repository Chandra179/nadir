package search

import "strconv"

// Filter limits document searches to indexed document fields.
type Filter struct {
	FilePath  string
	Header    string
	SourceSHA string
}

// SearchCandidate is the Retrieval-owned candidate exchanged with the
// document store, reranker, and semantic cache boundaries.
type SearchCandidate struct {
	Text       string
	WindowText string
	FilePath   string
	Header     string
	LineStart  int
	ChunkIndex int
	SourceSHA  string
	Score      float32
}

// Key identifies a logical source chunk across dense, lexical, and HyPE
// results. HyPE siblings intentionally collapse onto their parent chunk.
func (c SearchCandidate) Key() string {
	return c.FilePath + ":" + strconv.Itoa(c.LineStart)
}

// Chunk is the stable Retrieval result shape used by chat and the HTTP
// transport. Storage-specific vectors and sparse-index fields stay behind the
// store Adapter.
type Chunk struct {
	Text       string
	WindowText string
	FilePath   string
	Header     string
	LineStart  int
	ChunkIndex int
	SourceSHA  string
	Score      float32
}

// Request is one bounded retrieval request.
type Request struct {
	Query     string
	Keyword   string
	TopK      int
	Filter    *Filter
	SkipCache bool
}

// Result is the provider-neutral retrieval response.
type Result struct {
	Chunks    []Chunk
	FromCache bool
}
