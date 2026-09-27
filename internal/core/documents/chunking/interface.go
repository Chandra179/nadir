// Package chunking turns source documents into bounded retrieval chunks.
package chunking

// Chunker splits a document and can derive the contextual text used for
// indexing and retrieval.
type Chunker interface {
	Chunk(text string, filePath string) ([]Chunk, error)
	// ContextualText returns c's text prefixed with its file path and
	// heading, for embedding.
	ContextualText(c Chunk) string
}
