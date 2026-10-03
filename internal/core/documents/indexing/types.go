package indexing

// Result summarizes one indexing pass.
type Result struct {
	OperationID string
	Processed   int
	Skipped     int
	Failed      int
	Removed     int
	Files       []FileResult
}

// FileResult records the outcome for each submitted source, in input order.
// Published marks a visible update whose cleanup still needs a retry.
type FileResult struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
	Published bool   `json:"published,omitempty"`
}

// UploadFile is one source file submitted to POST /api/v1/documents as multipart form
// data or discovered under source.paths. Markdown enters the indexing pass
// directly; PDFs require the optional document-intake Adapter. Name is the
// source identity used for deduplication and citations.
type UploadFile struct {
	Name string
	Data []byte
}

// IndexedChunk is the indexing value sent to a document persistence Adapter.
// It contains the vector and enrichment metadata required to publish one
// version, while storage protocol types remain inside the Adapter.
type IndexedChunk struct {
	Text        string
	WindowText  string
	FilePath    string
	Header      string
	SectionPath string
	LineStart   int
	ChunkIndex  int
	Vector      []float32
	SourceSHA   string
	IngestedAt  string
	SparseText  string
}
