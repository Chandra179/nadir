package store

// Stats is a lightweight snapshot of collection size, used for dashboard
// display. Documents counts distinct file_path/source_sha pairs; Chunks is
// the raw point count in the collection.
type Stats struct {
	Documents int
	Chunks    int
}
