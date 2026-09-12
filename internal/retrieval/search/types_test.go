package search

import (
	"testing"

	"nadir/internal/adapters/qdrant/documents"
)

func TestFromStoreChunksKeepsRetrievalFieldsOnly(t *testing.T) {
	got := fromStoreChunks([]store.ScoredChunk{{
		Text:         "body",
		WindowText:   "window",
		FilePath:     "notes.md",
		Header:       "Limits",
		LineStart:    12,
		ChunkIndex:   2,
		SourceSHA:    "sha",
		Score:        0.8,
		Vector:       []float32{1, 2, 3},
		SparseText:   "storage-only",
		HypeQuestion: "storage-only",
	}})

	if len(got) != 1 || got[0].FilePath != "notes.md" || got[0].WindowText != "window" || got[0].Score != 0.8 {
		t.Fatalf("fromStoreChunks() = %+v, missing retrieval fields", got)
	}
}

func TestToStoreFilterCopiesOnlyFilterValues(t *testing.T) {
	if got := toStoreFilter(nil); got != nil {
		t.Fatalf("toStoreFilter(nil) = %+v, want nil", got)
	}

	got := toStoreFilter(&Filter{FilePath: "notes.md", Header: "Limits", SourceSHA: "sha"})
	if got.FilePath != "notes.md" || got.Header != "Limits" || got.SourceSHA != "sha" {
		t.Fatalf("toStoreFilter() = %+v, values were not copied", got)
	}
}
