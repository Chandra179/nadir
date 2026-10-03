package chunking

import (
	"strings"
)

func (c *dependencies) chunkSentenceWindow(rawText, filePath string) ([]Chunk, error) {
	sections := extractSections(rawText)
	var chunks []Chunk
	for _, sec := range sections {
		spans := sentenceRanges(sec.text)
		sentences := make([]string, len(spans))
		for i, span := range spans {
			sentences[i] = sec.text[span.start:span.end]
		}
		for i, sent := range sentences {
			lo := max(i-c.windowSize, 0)
			hi := min(i+c.windowSize+1, len(sentences))
			window := strings.TrimSpace(strings.Join(sentences[lo:hi], " "))
			chunks = append(chunks, Chunk{
				Text:         sent,
				WindowText:   window,
				FilePath:     filePath,
				Header:       sec.header,
				SectionPath:  sec.path,
				indexHeading: sec.indexHeading,
				LineStart:    sec.lineAt(spans[i]),
				ChunkIndex:   i,
			})
		}
	}
	return chunks, nil
}

func splitSentences(text string) []string {
	var sentences []string
	for _, span := range sentenceRanges(text) {
		sentences = append(sentences, text[span.start:span.end])
	}
	return sentences
}

func sentenceRanges(text string) []textSpan {
	indices := sentenceRe.FindAllStringIndex(text, -1)
	var spans []textSpan
	prev := 0
	for _, loc := range indices {
		span := trimSpan(text, textSpan{prev, loc[1]})
		if span.start < span.end {
			spans = append(spans, span)
		}
		prev = loc[1]
	}
	span := trimSpan(text, textSpan{prev, len(text)})
	if span.start < span.end {
		spans = append(spans, span)
	}
	return spans
}
