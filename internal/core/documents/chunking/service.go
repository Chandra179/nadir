package chunking

import "strings"

// Chunk is one bounded source segment with enough location and window context
// for indexing and citation rendering.
type Chunk struct {
	Text         string
	WindowText   string
	FilePath     string
	Header       string
	SectionPath  string // full heading ancestry; Header remains the filterable leaf
	indexHeading string
	LineStart    int
	ChunkIndex   int
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
	heading := c.indexHeading
	if heading == "" {
		heading = c.Header
	}
	if heading != "" {
		sb.WriteString(" > ")
		sb.WriteString(heading)
	}
	sb.WriteString("\n")
	sb.WriteString(c.Text)
	return sb.String()
}
