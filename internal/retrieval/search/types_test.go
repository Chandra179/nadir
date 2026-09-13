package search

import (
	"testing"
)

func TestFromCandidatesKeepsRetrievalFields(t *testing.T) {
	got := fromStoreChunks([]SearchCandidate{{
		Text:       "body",
		WindowText: "window",
		FilePath:   "notes.md",
		Header:     "Limits",
		LineStart:  12,
		ChunkIndex: 2,
		SourceSHA:  "sha",
		Score:      0.8,
	}})

	if len(got) != 1 || got[0].FilePath != "notes.md" || got[0].WindowText != "window" || got[0].Score != 0.8 {
		t.Fatalf("fromStoreChunks() = %+v, missing retrieval fields", got)
	}
}
