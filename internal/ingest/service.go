package ingest

type Result struct {
	Processed int
	Skipped   int
	Failed    int
}

// UploadFile is one file submitted to POST /ingest as multipart form data.
// Name is used both as the store's FilePath key (for dedup and citations)
// and to derive the chunker's file-extension check.
type UploadFile struct {
	Name string
	Data []byte
}
