package pkb

// DocumentChunk is a single chunk of text with its source pointer.
type DocumentChunk struct {
	Text      string
	FilePath  string
	Header    string
	LineStart int
}

// Chunker splits raw markdown text into DocumentChunks.
type Chunker interface {
	Chunk(text string, filePath string) ([]DocumentChunk, error)
}
