package search

import (
	"testing"
)

func TestFromCandidatesKeepsRetrievalFields(t *testing.T) {
	got := fromStoreChunks([]SearchCandidate{{
		Text:        "body",
		WindowText:  "window",
		FilePath:    "notes.md",
		Header:      "Limits",
		SectionPath: "Service > Limits",
		LineStart:   12,
		ChunkIndex:  2,
		SourceSHA:   "sha",
		Score:       0.8,
	}})

	if len(got) != 1 || got[0].FilePath != "notes.md" || got[0].WindowText != "window" || got[0].SectionPath != "Service > Limits" || got[0].Score != 0.8 {
		t.Fatalf("fromStoreChunks() = %+v, missing retrieval fields", got)
	}
}

func TestSemanticCacheKeepsSectionAncestry(t *testing.T) {
	want := []SearchCandidate{{FilePath: "methods.md", Header: "Properties", SectionPath: "Methods > First method > Properties", Text: "Requires a derivative.", SourceSHA: "version"}}
	got := fromCacheCandidates(toCacheCandidates(want))
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("cached evidence lost its subject: got=%+v want=%+v", got, want)
	}
}
