package chunking

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestRecursiveOverlapRetainsAcknowledgementModeScope(t *testing.T) {
	const raw = `# Consumer

## Acknowledgement Modes

**Auto acknowledgement (autoAck: true)**
The broker considers a message delivered as soon as it sends it over the socket.
* Fastest, no risk of forgetting to ack.
* Messages can be lost if the consumer crashes before processing.

**Manual acknowledgement (autoAck: false)**
The application must explicitly call channel.ack(deliveryTag) after successful processing.
* nack (or reject) can use requeue=true to return the message to the queue.
* Gives full control over at-least-once delivery semantics.
`
	d := NewDependencies(DependenciesConfig{ChunkSize: 512, ChunkOverlap: 64})
	chunks, err := d.Chunk(raw, "acknowledgements.md")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, chunk := range chunks {
		if chunk.Header != "Acknowledgement Modes" || !strings.Contains(chunk.Text, "Manual acknowledgement") {
			continue
		}
		found = true
		evidence := chunk.WindowText
		if evidence == "" {
			evidence = chunk.Text
		}
		if strings.Contains(evidence, "Messages can be lost") && !strings.Contains(evidence, "Auto acknowledgement (autoAck: true)") {
			t.Fatalf("automatic-mode warning lost its qualifier in manual evidence: %q", evidence)
		}
		if strings.Contains(evidence, "Auto acknowledgement") || !strings.Contains(chunk.SectionPath, "Manual acknowledgement") {
			t.Fatalf("manual evidence mixes distinct labelled modes: %+v", chunk)
		}
	}
	if !found {
		t.Fatal("manual acknowledgement evidence missing")
	}
}

func TestStandaloneBoldLabelsKeepEvidenceScopesAndSourceAnchors(t *testing.T) {
	d := NewDependencies(DependenciesConfig{ChunkSize: 512, ChunkOverlap: 64})
	chunks, err := d.Chunk("# Modes\n\n**First mode**\n\nFirst applies here.\n\n**Second mode**\n\nSecond applies elsewhere.\n\n## Next\n\nAn **emphasized** word is ordinary paragraph text.", "modes.md")
	if err != nil || len(chunks) != 3 {
		t.Fatalf("chunks=%+v err=%v", chunks, err)
	}
	for i, label := range []string{"First mode", "Second mode"} {
		if chunks[i].Header != "Modes" || chunks[i].SectionPath != "Modes > "+label || chunks[i].LineStart != 3+4*i || strings.Contains(chunks[i].Text, []string{"Second", "First"}[i]) {
			t.Fatalf("label scope changed its filter, source anchor or evidence: %+v", chunks[i])
		}
	}
	if chunks[2].SectionPath != "Modes > Next" || !strings.Contains(chunks[2].Text, "An emphasized word") {
		t.Fatalf("inline emphasis became a heading or previous label leaked: %+v", chunks[2])
	}
}

func TestLabelsUnderOneHeadingKeepItsPreciseIndexPrefix(t *testing.T) {
	d := NewDependencies(DependenciesConfig{ChunkSize: 512, ChunkOverlap: 64})
	chunks, err := d.Chunk("# Broad topic\n\n## Specific section\n\n**First label**\nFirst body.\n\n**Second label**\nSecond body.", "labels.md")
	if err != nil || len(chunks) != 2 {
		t.Fatalf("chunks=%+v err=%v", chunks, err)
	}
	for i, label := range []string{"First label", "Second label"} {
		if !strings.HasPrefix(d.ContextualText(chunks[i]), "labels.md > Specific section > "+label+"\n") || chunks[i].SectionPath != "Broad topic > Specific section > "+label {
			t.Fatalf("label scopes turned a unique leaf into broad ancestry indexing: %+v indexed=%q", chunks[i], d.ContextualText(chunks[i]))
		}
	}
	inline, err := d.Chunk("## Section\n\n**Important** text on the same line.\n\nOrdinary body.", "inline.md")
	if err != nil || len(inline) != 1 || inline[0].SectionPath != "Section" {
		t.Fatalf("leading inline emphasis became a label scope: %+v err=%v", inline, err)
	}
}

func TestRecursiveSectionWindowIsBoundedAndPreservesCentralLocation(t *testing.T) {
	d := NewDependencies(DependenciesConfig{ChunkSize: 20, ChunkOverlap: 4})
	for _, body := range []string{"αβγδεζηθικλμνξοπρστυφχψωαβγδεζηθ", strings.Repeat("long words ", 30)} {
		chunks, err := d.Chunk("# Heading\n\n"+body, "bounded.md")
		if err != nil || len(chunks) < 2 {
			t.Fatalf("chunks=%+v err=%v", chunks, err)
		}
		for _, chunk := range chunks {
			if !utf8.ValidString(chunk.WindowText) || utf8.RuneCountInString(chunk.WindowText) > 40 || chunk.LineStart != 3 {
				t.Fatalf("unbounded or misplaced section window: %+v", chunk)
			}
			if utf8.RuneCountInString(body) > 40 && chunk.WindowText != "" {
				t.Fatalf("long section expanded into answer context: %+v", chunk)
			}
		}
	}
}

func TestRepeatedLeafHeadingsRetainTheirParentSubject(t *testing.T) {
	d := NewDependencies(DependenciesConfig{ChunkSize: 512, ChunkOverlap: 64})
	chunks, err := d.Chunk("# Methods\n\n## First method\n\n### Properties\n\nRequires a derivative.\n\n## Second method\n\n### Properties\n\nAvoids derivative computation.", "methods.md")
	if err != nil || len(chunks) != 2 {
		t.Fatalf("chunks=%+v err=%v", chunks, err)
	}
	for i, subject := range []string{"First method", "Second method"} {
		if chunks[i].Header != "Properties" || !strings.Contains(d.ContextualText(chunks[i]), subject+" > Properties") {
			t.Fatalf("leaf heading lost its subject or changed the header filter: %+v, indexed=%q", chunks[i], d.ContextualText(chunks[i]))
		}
	}
}

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
	if got := d.ContextualText(chunks[0]); !strings.HasPrefix(got, "math.md > Derivatives\n") || chunks[0].SectionPath != "Calculus > Derivatives" {
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
