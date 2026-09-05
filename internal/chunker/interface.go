package chunker

type Chunker interface {
	Chunk(text string, filePath string) ([]Chunk, error)
	// ContextualText returns c's text prefixed with its file path and
	// heading, for embedding.
	ContextualText(c Chunk) string
}
