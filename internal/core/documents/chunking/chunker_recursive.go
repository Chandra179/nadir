package chunking

import (
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

func nodeToPlainText(n ast.Node, src []byte) string {
	var sb strings.Builder
	_ = ast.Walk(n, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
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
			for c := v.FirstChild(); c != nil; c = c.NextSibling() {
				if t, ok := c.(*ast.Text); ok {
					sb.Write(t.Segment.Value(src))
				}
			}
			return ast.WalkSkipChildren, nil
		case *ast.FencedCodeBlock, *ast.CodeBlock:
			for i := 0; i < v.Lines().Len(); i++ {
				line := v.Lines().At(i)
				sb.Write(line.Value(src))
			}
			return ast.WalkSkipChildren, nil
		case *ast.ListItem:
			for c := v.FirstChild(); c != nil; c = c.NextSibling() {
				sb.WriteString(nodeToPlainText(c, src))
			}
			// Items are block-level siblings; without a separator the next
			// item's text fuses onto this one's last word.
			sb.WriteByte('\n')
			return ast.WalkSkipChildren, nil
		case *ast.Link, *ast.Image:
			for c := v.FirstChild(); c != nil; c = c.NextSibling() {
				sb.WriteString(nodeToPlainText(c, src))
			}
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	return sb.String()
}

func (c *dependencies) isTOCChunk(text string) bool {
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

func (c *dependencies) chunkRecursive(rawText, filePath string) ([]Chunk, error) {
	rawText = reHTMLComment.ReplaceAllString(rawText, "")

	sections := extractSections(rawText)
	var chunks []Chunk
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
			chunks = append(chunks, Chunk{
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

	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch v := n.(type) {
		case *ast.Heading:
			flush()
			currentLines = nil
			seg := v.Lines()
			if seg != nil && seg.Len() > 0 {
				currentLine = lineOf(seg.At(0).Start)
			}
			currentHeader = strings.TrimSpace(nodeToPlainText(v, src))
		case *ast.Paragraph:
			currentLines = append(currentLines, nodeToPlainText(v, src))
			return ast.WalkSkipChildren, nil
		case *ast.List:
			currentLines = append(currentLines, nodeToPlainText(v, src))
			return ast.WalkSkipChildren, nil
		case *ast.Blockquote:
			currentLines = append(currentLines, nodeToPlainText(v, src))
			return ast.WalkSkipChildren, nil
		case *ast.FencedCodeBlock, *ast.CodeBlock:
			currentLines = append(currentLines, nodeToPlainText(v, src))
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

func (c *dependencies) splitText(text string) []string {
	if utf8.RuneCountInString(text) <= c.chunkSize {
		return []string{text}
	}
	separators := []string{"\n\n", "\n", ". ", " "}
	for i, sep := range separators {
		parts := strings.Split(text, sep)
		if len(parts) > 1 {
			return c.mergeSplits(parts, sep, separators[i+1:])
		}
	}
	return hardSplit(text, c.chunkSize, c.chunkOverlap)
}

// mergeSplits packs parts into chunks up to chunkSize, carrying an overlap
// suffix into the next chunk. A part longer than chunkSize can never fit, so
// it is pre-split with the remaining finer separators and emitted as
// standalone chunks instead of being accepted whole.
func (c *dependencies) mergeSplits(parts []string, sep string, finer []string) []string {
	var chunks []string
	current := ""
	for _, p := range parts {
		if utf8.RuneCountInString(p) > c.chunkSize {
			if current != "" {
				chunks = append(chunks, current)
				current = ""
			}
			chunks = append(chunks, c.subSplit(p, finer)...)
			continue
		}
		candidate := current
		if candidate != "" {
			candidate += sep
		}
		candidate += p

		if utf8.RuneCountInString(candidate) <= c.chunkSize {
			current = candidate
			continue
		}

		chunks = append(chunks, current)
		current = overlapSuffix(current, c.chunkOverlap) + sep + p
	}
	if current != "" {
		chunks = append(chunks, current)
	}
	return chunks
}

// subSplit breaks one oversized part with the remaining separators — most
// graceful first — packing each level with the separator that produced it so
// re-splitting never rewrites content characters. hardSplit bounds the result
// when no separator applies.
func (c *dependencies) subSplit(part string, separators []string) []string {
	for i, sep := range separators {
		parts := strings.Split(part, sep)
		if len(parts) > 1 {
			return c.mergeSplits(parts, sep, separators[i+1:])
		}
	}
	return hardSplit(part, c.chunkSize, c.chunkOverlap)
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
