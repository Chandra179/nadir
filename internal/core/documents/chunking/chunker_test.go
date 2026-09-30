package chunking

import (
	"strings"
	"testing"
	"unicode/utf8"
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

func TestRepeatedChunksKeepTheirActualSourceLines(t *testing.T) {
	d := NewDependencies(DependenciesConfig{ChunkSize: 12})
	chunks, err := d.Chunk("# Heading\n\nRepeat line.\n\nRepeat line.\n\nRepeat line.", "repeat.md")
	if err != nil || len(chunks) != 3 {
		t.Fatalf("chunks=%+v err=%v", chunks, err)
	}
	for i, chunk := range chunks {
		if chunk.LineStart != 3+i*2 || chunk.ChunkIndex != i || chunk.Text != "Repeat line." {
			t.Fatalf("chunk %d has fabricated location: %+v", i, chunk)
		}
	}
}

func TestMarkdownSourceMapSurvivesFormattingAndComments(t *testing.T) {
	doc := "# Heading\n<!-- hidden\ncomment -->\n\n**Alpha** text\ncontinues here.\n\n- Beta item\n- Gamma item\n\n```go\nfirst()\nsecond()\n```"
	sections := extractSections(doc)
	if len(sections) != 1 {
		t.Fatalf("sections=%+v", sections)
	}
	section := sections[0]
	for text, wantLine := range map[string]int{"Alpha": 5, "continues": 6, "Beta": 8, "Gamma": 9, "first()": 12, "second()": 13} {
		offset := strings.Index(section.text, text)
		if offset < 0 {
			t.Fatalf("extraction dropped %q: %q", text, section.text)
		}
		if got := section.lineAt(textSpan{offset, offset + len(text)}); got != wantLine {
			t.Fatalf("%q starts on source line %d, want %d", text, got, wantLine)
		}
	}
	if strings.Contains(section.text, "hidden") || strings.Contains(section.text, "comment") {
		t.Fatalf("comment leaked into evidence: %q", section.text)
	}
}

func TestOverlappingCodeChunksKeepStartLine(t *testing.T) {
	d := NewDependencies(DependenciesConfig{ChunkSize: 8, ChunkOverlap: 3})
	chunks, err := d.Chunk("# Code\n\n```\nαβγδεζηθ\nικλμνξοπ\n```", "unicode.md")
	if err != nil || len(chunks) != 2 {
		t.Fatalf("chunks=%+v err=%v", chunks, err)
	}
	// The second chunk includes a suffix of line 4, so its source starts on
	// line 4 too, even though the new non-overlap evidence comes from line 5.
	if chunks[0].LineStart != 4 || chunks[1].LineStart != 4 || !strings.HasPrefix(chunks[1].Text, "ζηθ") {
		t.Fatalf("overlap lost its source position: %+v", chunks)
	}
}

func TestSentenceWindowTracksCentralSentenceSourceLine(t *testing.T) {
	d := NewDependencies(DependenciesConfig{Provider: ProviderSentenceWindow, WindowSize: 1})
	chunks, err := d.Chunk("# Heading\n\nSame.\nSame.\nSame.", "sentences.md")
	if err != nil || len(chunks) != 3 {
		t.Fatalf("chunks=%+v err=%v", chunks, err)
	}
	for i, chunk := range chunks {
		if chunk.LineStart != i+3 {
			t.Fatalf("sentence %d source line=%d, want %d", i, chunk.LineStart, i+3)
		}
	}
}

func TestRecursiveSubdivisionRetainsRepeatedParagraphLocations(t *testing.T) {
	d := NewDependencies(DependenciesConfig{ChunkSize: 8, ChunkOverlap: 3})
	chunks, err := d.Chunk("# Heading\n\nabcdefghijklmno\n\nabcdefghijklmno\n\nabcdefghijklmno", "repeated-long.md")
	if err != nil || len(chunks) != 9 {
		t.Fatalf("chunks=%+v err=%v", chunks, err)
	}
	for i, chunk := range chunks {
		if chunk.LineStart != 3+2*(i/3) || chunk.ChunkIndex != i {
			t.Fatalf("recursive subdivision lost source occurrence: chunk %d=%+v", i, chunk)
		}
		if !utf8.ValidString(chunk.Text) || len([]rune(chunk.Text)) > 8 {
			t.Fatalf("hard subdivision exceeded its bound: %+v", chunk)
		}
	}
}

func TestHardSplitRangesMakeProgressWithInvalidDirectConfiguration(t *testing.T) {
	for _, config := range []struct{ size, overlap int }{{0, 0}, {3, 3}, {3, 9}, {3, -1}} {
		spans := hardSplitRanges("αβγδεζη", textSpan{0, len("αβγδεζη")}, config.size, config.overlap)
		previous := -1
		for _, span := range spans {
			if span.start <= previous || span.start >= span.end || !utf8.ValidString("αβγδεζη"[span.start:span.end]) {
				t.Fatalf("non-advancing or invalid rune span with %+v: %+v", config, spans)
			}
			previous = span.start
		}
	}
}
