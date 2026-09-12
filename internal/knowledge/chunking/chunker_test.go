package chunker

import (
	"strings"
	"testing"
)

func TestRecursiveChunkerPreservesDocumentContextAndSkipsTOC(t *testing.T) {
	d := NewDependencies(DependenciesConfig{Provider: "recursive", ChunkSize: 80, ChunkOverlap: 10})
	chunks, err := d.Chunk("# Calculus\n\nSee 1\n\n## Derivatives\n\nThe power rule describes derivatives of powers.", "math.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 {
		t.Fatalf("got %d chunks, want one non-TOC chunk: %+v", len(chunks), chunks)
	}
	if chunks[0].Header != "Derivatives" || chunks[0].FilePath != "math.md" {
		t.Fatalf("chunk metadata = %+v, want Derivatives/math.md", chunks[0])
	}
	if got := d.ContextualText(chunks[0]); !strings.HasPrefix(got, "math.md > Derivatives\n") {
		t.Fatalf("contextual text = %q, want path and heading prefix", got)
	}
}

func TestSentenceWindowChunkerBuildsBoundedWindows(t *testing.T) {
	d := NewDependencies(DependenciesConfig{Provider: ProviderSentenceWindow, WindowSize: 1})
	chunks, err := d.Chunk("One. Two. Three. Four.", "notes.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 4 {
		t.Fatalf("got %d sentence chunks, want 4", len(chunks))
	}
	if chunks[0].WindowText != "One. Two." || chunks[1].WindowText != "One. Two. Three." {
		t.Fatalf("windows = %#v, want adjacent sentence context", []string{chunks[0].WindowText, chunks[1].WindowText})
	}
}

func TestHardSplitHandlesUnicodeAndOverlap(t *testing.T) {
	got := hardSplit("αβγδεζη", 3, 1)
	if len(got) != 3 || got[0] != "αβγ" || got[1] != "γδε" || got[2] != "εζη" {
		t.Fatalf("hardSplit() = %#v, want rune-safe overlapping chunks", got)
	}
}
