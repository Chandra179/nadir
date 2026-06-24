package chunker

import (
	"strconv"
	"strings"
)

type Chunk struct {
	Text       string
	WindowText string
	FilePath   string
	Header     string
	LineStart  int
	ChunkIndex int
}

func (c Chunk) Key() string {
	return c.FilePath + ":" + strconv.Itoa(c.LineStart)
}

type Chunker interface {
	Chunk(text string, filePath string) ([]Chunk, error)
}

func ContextualText(c Chunk) string {
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
