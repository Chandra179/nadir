package chunking

import (
	"strings"
	"unicode/utf8"
)

func (c *dependencies) isTOCChunk(text string) bool {
	if c.tocThreshold <= 0 {
		return false
	}
	lines := strings.Split(strings.TrimSpace(text), "\n")
	nonEmpty, matches := 0, 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		nonEmpty++
		if reTOCLine.MatchString(line) {
			matches++
		}
	}
	return nonEmpty > 0 && float64(matches)/float64(nonEmpty) >= c.tocThreshold
}

func (c *dependencies) chunkRecursive(rawText, filePath string) ([]Chunk, error) {
	var chunks []Chunk
	for _, sec := range extractSections(rawText) {
		idx := 0
		// Overlap can carry the end of one qualified statement into another
		// chunk without its subject. Preserve short sections as answer context;
		// the central text remains the bounded embedding/retrieval unit.
		window := ""
		if utf8.RuneCountInString(sec.text) <= 2*max(1, c.chunkSize) {
			window = strings.TrimSpace(sec.text)
		}
		for _, span := range c.splitRanges(sec.text) {
			span = trimSpan(sec.text, span)
			part := sec.text[span.start:span.end]
			if part == "" || c.isTOCChunk(part) {
				continue
			}
			context := window
			if context == part {
				context = ""
			}
			chunks = append(chunks, Chunk{Text: part, WindowText: context, FilePath: filePath,
				Header: sec.header, SectionPath: sec.path, indexHeading: sec.indexHeading, LineStart: sec.lineAt(span), ChunkIndex: idx})
			idx++
		}
	}
	return chunks, nil
}
