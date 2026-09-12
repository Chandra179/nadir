package chunker

import (
	"strings"
)

func (c *dependencies) chunkSentenceWindow(rawText, filePath string) ([]Chunk, error) {
	sections := extractSections(rawText)
	var chunks []Chunk
	for _, sec := range sections {
		sentences := splitSentences(sec.text)
		for i, sent := range sentences {
			sent = strings.TrimSpace(sent)
			if sent == "" {
				continue
			}
			lo := max(i-c.windowSize, 0)
			hi := min(i+c.windowSize+1, len(sentences))
			window := strings.TrimSpace(strings.Join(sentences[lo:hi], " "))
			chunks = append(chunks, Chunk{
				Text:       sent,
				WindowText: window,
				FilePath:   filePath,
				Header:     sec.header,
				LineStart:  sec.lineStart,
				ChunkIndex: i,
			})
		}
	}
	return chunks, nil
}

func splitSentences(text string) []string {
	indices := sentenceRe.FindAllStringIndex(text, -1)
	if len(indices) == 0 {
		if t := strings.TrimSpace(text); t != "" {
			return []string{t}
		}
		return nil
	}
	var sentences []string
	prev := 0
	for _, loc := range indices {
		s := strings.TrimSpace(text[prev:loc[1]])
		if s != "" {
			sentences = append(sentences, s)
		}
		prev = loc[1]
	}
	if tail := strings.TrimSpace(text[prev:]); tail != "" {
		sentences = append(sentences, tail)
	}
	return sentences
}
