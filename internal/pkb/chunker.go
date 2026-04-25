package pkb

import "strconv"

// DocumentChunk is a single chunk of text with its source pointer.
type DocumentChunk struct {
	Text       string
	WindowText string // surrounding context; non-empty when produced by SentenceWindowChunker
	FilePath   string
	Header     string
	LineStart  int
	ChunkIndex int // ordinal within section; used to produce unique point IDs
}

// Key returns a stable string identifier for deduplication: "filePath:lineStart".
func (c DocumentChunk) Key() string {
	return c.FilePath + ":" + strconv.Itoa(c.LineStart)
}

// Chunker splits raw markdown text into DocumentChunks.
type Chunker interface {
	Chunk(text string, filePath string) ([]DocumentChunk, error)
}
