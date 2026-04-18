package pkb

import (
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// RecursiveChunker splits markdown into chunks using a recursive character strategy.
// It first extracts sections by heading, then splits oversized sections by paragraph,
// then by sentence, preserving overlap between adjacent chunks.
type RecursiveChunker struct {
	chunkSize    int
	chunkOverlap int
}

func NewRecursiveChunker(chunkSize, chunkOverlap int) *RecursiveChunker {
	return &RecursiveChunker{chunkSize: chunkSize, chunkOverlap: chunkOverlap}
}

type section struct {
	header    string
	lineStart int
	text      string
}

func (c *RecursiveChunker) Chunk(rawText, filePath string) ([]DocumentChunk, error) {
	sections := extractSections(rawText)
	var chunks []DocumentChunk
	for _, sec := range sections {
		parts := c.splitText(sec.text)
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			chunks = append(chunks, DocumentChunk{
				Text:      part,
				FilePath:  filePath,
				Header:    sec.header,
				LineStart: sec.lineStart,
			})
		}
	}
	return chunks, nil
}

// extractSections parses markdown headings and groups text under each heading.
func extractSections(rawText string) []section {
	src := []byte(rawText)
	parser := goldmark.DefaultParser()
	reader := text.NewReader(src)
	doc := parser.Parse(reader)

	var sections []section
	currentHeader := ""
	currentLine := 1
	var currentLines []string

	lines := strings.Split(rawText, "\n")

	flush := func() {
		if len(currentLines) > 0 {
			sections = append(sections, section{
				header:    currentHeader,
				lineStart: currentLine,
				text:      strings.Join(currentLines, "\n"),
			})
		}
	}

	lineOf := func(offset int) int {
		return strings.Count(rawText[:offset], "\n") + 1
	}

	_ = lines
	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if h, ok := n.(*ast.Heading); ok {
			flush()
			currentLines = nil
			seg := h.Lines()
			if seg != nil && seg.Len() > 0 {
				currentLine = lineOf(seg.At(0).Start)
			}
			var hb strings.Builder
			for child := h.FirstChild(); child != nil; child = child.NextSibling() {
				if t, ok := child.(*ast.Text); ok {
					hb.Write(t.Segment.Value(src))
				}
			}
			currentHeader = hb.String()
		} else if p, ok := n.(*ast.Paragraph); ok {
			var sb strings.Builder
			segs := p.Lines()
			for i := 0; i < segs.Len(); i++ {
				seg := segs.At(i)
				sb.Write(seg.Value(src))
			}
			currentLines = append(currentLines, sb.String())
		}
		return ast.WalkContinue, nil
	})
	flush()

	if len(sections) == 0 {
		sections = []section{{header: "", lineStart: 1, text: rawText}}
	}
	return sections
}

// splitText recursively splits text into chunks of at most chunkSize runes with overlap.
func (c *RecursiveChunker) splitText(text string) []string {
	if len([]rune(text)) <= c.chunkSize {
		return []string{text}
	}
	separators := []string{"\n\n", "\n", ". ", " "}
	for _, sep := range separators {
		parts := strings.Split(text, sep)
		if len(parts) > 1 {
			return c.mergeSplits(parts, sep)
		}
	}
	// hard split
	return hardSplit(text, c.chunkSize, c.chunkOverlap)
}

func (c *RecursiveChunker) mergeSplits(parts []string, sep string) []string {
	var chunks []string
	current := ""
	for _, p := range parts {
		candidate := current
		if candidate != "" {
			candidate += sep
		}
		candidate += p
		if len([]rune(candidate)) > c.chunkSize && current != "" {
			chunks = append(chunks, current)
			// start next chunk with overlap from end of current
			overlap := overlapSuffix(current, c.chunkOverlap)
			current = overlap + sep + p
		} else {
			current = candidate
		}
	}
	if current != "" {
		chunks = append(chunks, current)
	}
	return chunks
}

func overlapSuffix(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[len(runes)-n:])
}

func hardSplit(s string, size, overlap int) []string {
	runes := []rune(s)
	var chunks []string
	for start := 0; start < len(runes); start += size - overlap {
		end := start + size
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[start:end]))
		if end == len(runes) {
			break
		}
	}
	return chunks
}
