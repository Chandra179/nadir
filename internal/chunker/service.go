package chunker

import "strings"

type Chunk struct {
	Text       string
	WindowText string
	FilePath   string
	Header     string
	LineStart  int
	ChunkIndex int
}

type Chunker interface {
	Chunk(text string, filePath string) ([]Chunk, error)
	// ContextualText returns c's text prefixed with its file path and
	// heading, for embedding.
	ContextualText(c Chunk) string
}

func (d *dependencies) Chunk(rawText, filePath string) ([]Chunk, error) {
	if d.provider == ProviderSentenceWindow {
		return d.chunkSentenceWindow(rawText, filePath)
	}
	return d.chunkRecursive(rawText, filePath)
}

func (d *dependencies) ContextualText(c Chunk) string {
	var sb strings.Builder
	sb.WriteString(c.FilePath)
	if c.Header != "" {
		sb.WriteString(" > ")
		sb.WriteString(c.Header)
	}
	sb.WriteString("\n")
	sb.WriteString(c.Text)
	return sb.String()
}
