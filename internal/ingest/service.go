package ingest

type Result struct {
	Processed int
	Skipped   int
	Failed    int
}

// UploadFile is one source file submitted to POST /ingest as multipart form
// data or discovered under source.paths. Markdown enters the indexing pass
// directly; PDFs require the optional document-intake Adapter. Name is the
// source identity used for deduplication and citations.
type UploadFile struct {
	Name string
	Data []byte
}
