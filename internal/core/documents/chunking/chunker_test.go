package chunking

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

func TestRecursiveChunkerCapturesFencedCodeBlocks(t *testing.T) {
	d := NewDependencies(DependenciesConfig{Provider: "recursive", ChunkSize: 512, ChunkOverlap: 64})
	doc := "# Service\n\nRun the reset command:\n\n```bash\ncurl -X POST localhost:8100/api/v1/documents/reset\n```\n\nDone."
	chunks, err := d.Chunk(doc, "svc.md")
	if err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, c := range chunks {
		joined += c.Text + "\n"
	}
	if !strings.Contains(joined, "curl -X POST localhost:8100") {
		t.Fatalf("chunks = %#v, want fenced code block content preserved", chunks)
	}
}

func TestRecursiveChunkerSeparatesListItems(t *testing.T) {
	d := NewDependencies(DependenciesConfig{Provider: "recursive", ChunkSize: 512, ChunkOverlap: 64})
	doc := "# Methods\n\n- Newton's method uses derivatives\n- Secant method avoids derivatives"
	chunks, err := d.Chunk(doc, "methods.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) == 0 {
		t.Fatal("no chunks produced")
	}
	if !strings.Contains(chunks[0].Text, "derivatives\nSecant") {
		t.Fatalf("chunk text = %q, want list items separated by a newline", chunks[0].Text)
	}
}

// Property: no emitted chunk exceeds chunkSize by more than the overlap tail,
// whatever the input — oversized paragraphs, long URLs, or unbroken runes.
func TestRecursiveChunkerNeverEmitsOversizedChunks(t *testing.T) {
	d := NewDependencies(DependenciesConfig{Provider: "recursive", ChunkSize: 120, ChunkOverlap: 20})
	huge := strings.Repeat("word ", 60) // ~300 runes, only spaces inside
	doc := "# Docs\n\n" + huge + "\n\n" +
		strings.Repeat("https://example.com/very/long/path/segment/", 6) + "\n\n" +
		strings.Repeat("x", 400)
	chunks, err := d.Chunk(doc, "big.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) == 0 {
		t.Fatal("no chunks produced")
	}
	for i, c := range chunks {
		// Bound = chunkSize + overlap tail + the packing separator.
		if got := len([]rune(c.Text)); got > 120+20+1 {
			t.Fatalf("chunk %d has %d runes, want <= chunkSize+overlap+sep: %q", i, got, c.Text)
		}
	}
}
