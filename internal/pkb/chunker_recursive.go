package pkb

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

var (
	reHTMLComment = regexp.MustCompile(`<!--.*?-->`)
	// matches lines like "Some Title  42" or "1.2 Section Name ... 5"
	reTOCLine = regexp.MustCompile(`(?m)^.{1,80}\s+\d+\s*$`)
)

// nodeToPlainText extracts readable text from an AST node, stripping markdown syntax.
func nodeToPlainText(n ast.Node, src []byte) string {
	var sb strings.Builder
	ast.Walk(n, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch v := node.(type) {
		case *ast.Text:
			sb.Write(v.Segment.Value(src))
			if v.SoftLineBreak() || v.HardLineBreak() {
				sb.WriteByte(' ')
			}
		case *ast.String:
			sb.Write(v.Value)
		case *ast.CodeSpan:
			// collect raw bytes inside code span
			for c := v.FirstChild(); c != nil; c = c.NextSibling() {
				if t, ok := c.(*ast.Text); ok {
					sb.Write(t.Segment.Value(src))
				}
			}
			return ast.WalkSkipChildren, nil
		case *ast.FencedCodeBlock:
			for i := 0; i < v.Lines().Len(); i++ {
				line := v.Lines().At(i)
				sb.Write(line.Value(src))
			}
			return ast.WalkSkipChildren, nil
		case *ast.Link:
			// emit link text, skip URL/title
			for c := v.FirstChild(); c != nil; c = c.NextSibling() {
				sb.WriteString(nodeToPlainText(c, src))
			}
			return ast.WalkSkipChildren, nil
		case *ast.Image:
			// emit alt text only
			for c := v.FirstChild(); c != nil; c = c.NextSibling() {
				sb.WriteString(nodeToPlainText(c, src))
			}
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	return sb.String()
}

// RecursiveChunker splits markdown into chunks using a recursive character strategy.
// It first extracts sections by heading, then splits oversized sections by paragraph,
// then by sentence, preserving overlap between adjacent chunks.
type RecursiveChunker struct {
	chunkSize    int
	chunkOverlap int
	tocThreshold float64 // fraction of lines matching TOC pattern to classify as TOC (0 = disabled)
}

func NewRecursiveChunker(chunkSize, chunkOverlap int) *RecursiveChunker {
	return &RecursiveChunker{chunkSize: chunkSize, chunkOverlap: chunkOverlap, tocThreshold: 0.6}
}

// WithTOCThreshold sets the fraction of lines that must match the TOC heuristic
// for a chunk to be dropped. Set to 0 to disable TOC filtering.
func (c *RecursiveChunker) WithTOCThreshold(t float64) *RecursiveChunker {
	c.tocThreshold = t
	return c
}

// isTOCChunk returns true when the chunk looks like a table-of-contents page:
// high ratio of lines ending with a bare page number, low prose density.
func (c *RecursiveChunker) isTOCChunk(text string) bool {
	if c.tocThreshold <= 0 {
		return false
	}
	lines := strings.Split(strings.TrimSpace(text), "\n")
	nonEmpty := 0
	matches := 0
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		nonEmpty++
		if reTOCLine.MatchString(l) {
			matches++
		}
	}
	if nonEmpty == 0 {
		return false
	}
	return float64(matches)/float64(nonEmpty) >= c.tocThreshold
}

type section struct {
	header    string
	lineStart int
	text      string
}

func (c *RecursiveChunker) Chunk(rawText, filePath string) ([]DocumentChunk, error) {
	// Strip HTML comments (e.g. <!-- formula-not-decoded --> from Docling output).
	rawText = reHTMLComment.ReplaceAllString(rawText, "")

	sections := extractSections(rawText)
	var chunks []DocumentChunk
	for _, sec := range sections {
		parts := c.splitText(sec.text)
		idx := 0
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if c.isTOCChunk(part) {
				continue
			}
			chunks = append(chunks, DocumentChunk{
				Text:       part,
				FilePath:   filePath,
				Header:     sec.header,
				LineStart:  sec.lineStart,
				ChunkIndex: idx,
			})
			idx++
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
			currentHeader = strings.TrimSpace(nodeToPlainText(h, src))
		} else if p, ok := n.(*ast.Paragraph); ok {
			currentLines = append(currentLines, nodeToPlainText(p, src))
			return ast.WalkSkipChildren, nil
		} else if list, ok := n.(*ast.List); ok {
			currentLines = append(currentLines, nodeToPlainText(list, src))
			return ast.WalkSkipChildren, nil
		} else if bq, ok := n.(*ast.Blockquote); ok {
			currentLines = append(currentLines, nodeToPlainText(bq, src))
			return ast.WalkSkipChildren, nil
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
	if utf8.RuneCountInString(text) <= c.chunkSize {
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
		if utf8.RuneCountInString(candidate) > c.chunkSize && current != "" {
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
		end := min(start+size, len(runes))
		chunks = append(chunks, string(runes[start:end]))
		if end == len(runes) {
			break
		}
	}
	return chunks
}
