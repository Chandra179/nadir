package pkb

import (
	"regexp"
	"strings"
)

// sentenceRe splits on sentence-ending punctuation followed by whitespace or end of string.
var sentenceRe = regexp.MustCompile(`[.!?]+[\s]+`)

// SentenceWindowChunker indexes at sentence granularity but stores a surrounding window as
// retrieval context. The sentence is embedded (precise matching); WindowText (surrounding
// sentences) is returned to the caller for richer context.
type SentenceWindowChunker struct {
	windowSize int // number of sentences before and after each sentence
}

func NewSentenceWindowChunker(windowSize int) *SentenceWindowChunker {
	return &SentenceWindowChunker{windowSize: windowSize}
}

func (c *SentenceWindowChunker) Chunk(rawText, filePath string) ([]DocumentChunk, error) {
	sections := extractSections(rawText)
	var chunks []DocumentChunk
	for _, sec := range sections {
		sentences := splitSentences(sec.text)
		for i, sent := range sentences {
			sent = strings.TrimSpace(sent)
			if sent == "" {
				continue
			}
			lo := i - c.windowSize
			if lo < 0 {
				lo = 0
			}
			hi := i + c.windowSize + 1
			if hi > len(sentences) {
				hi = len(sentences)
			}
			window := strings.TrimSpace(strings.Join(sentences[lo:hi], " "))
			chunks = append(chunks, DocumentChunk{
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
