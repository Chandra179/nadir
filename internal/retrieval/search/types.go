package search

// Filter narrows Retrieval to a source file, heading, or content version.
type Filter struct {
	FilePath  string
	Header    string
	SourceSHA string
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

type Request struct {
	Query     string
	Keyword   string
	TopK      int
	Filter    *Filter
	SkipCache bool
}

type Result struct {
	Chunks    []Chunk
	FromCache bool
}
