package chunking

import (
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// sourceText retains a source line for every extracted byte. Markdown strips
// formatting and joins some line breaks, so counting newlines in plain text
// cannot recover source locations. Synthetic separators have no location (0).
type sourceText struct {
	text  strings.Builder
	lines []int
}

func (s *sourceText) generated(value string) {
	s.text.WriteString(value)
	s.lines = append(s.lines, make([]int, len(value))...)
}

func (s *sourceText) segment(segment text.Segment, src []byte, sourceLines []int) {
	value := segment.Value(src)
	s.text.Write(value)
	for i := range value {
		offset := segment.Start + i - segment.Padding
		if i < segment.Padding || offset >= segment.Stop {
			s.lines = append(s.lines, 0)
		} else {
			s.lines = append(s.lines, sourceLines[offset])
		}
	}
}

func nodeSourceText(n ast.Node, src []byte, sourceLines []int) sourceText {
	var out sourceText
	_ = ast.Walk(n, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			if _, ok := node.(*ast.ListItem); ok {
				out.generated("\n")
			}
			return ast.WalkContinue, nil
		}
		switch v := node.(type) {
		case *ast.Text:
			out.segment(v.Segment, src, sourceLines)
			if v.SoftLineBreak() || v.HardLineBreak() {
				out.generated(" ")
			}
		case *ast.String:
			out.generated(string(v.Value))
		case *ast.CodeSpan:
			for child := v.FirstChild(); child != nil; child = child.NextSibling() {
				if t, ok := child.(*ast.Text); ok {
					out.segment(t.Segment, src, sourceLines)
				}
			}
			return ast.WalkSkipChildren, nil
		case *ast.FencedCodeBlock, *ast.CodeBlock:
			for i := 0; i < v.Lines().Len(); i++ {
				out.segment(v.Lines().At(i), src, sourceLines)
			}
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	return out
}

type section struct {
	header string
	text   string
	lines  []int
}

func (s section) lineAt(span textSpan) int {
	for _, line := range s.lines[span.start:span.end] {
		if line > 0 {
			return line
		}
	}
	// Generated text without a corresponding source segment has no precise
	// source location. Do not invent a line by counting normalized text.
	return 0
}

func extractSections(rawText string) []section {
	// Remove comments without shifting byte offsets or original line numbers.
	clean := reHTMLComment.ReplaceAllStringFunc(rawText, func(comment string) string {
		masked := []byte(comment)
		for i, b := range masked {
			if b != '\n' && b != '\r' {
				masked[i] = ' '
			}
		}
		return string(masked)
	})
	src := []byte(clean)
	sourceLines := make([]int, len(src))
	line := 1
	for i, b := range src {
		sourceLines[i] = line
		if b == '\n' {
			line++
		}
	}
	doc := goldmark.DefaultParser().Parse(text.NewReader(src))
	var sections []section
	currentHeader := ""
	var current sourceText
	flush := func() {
		if current.text.Len() > 0 {
			sections = append(sections, section{header: currentHeader,
				text: current.text.String(), lines: current.lines})
		}
		current = sourceText{}
	}
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n.(type) {
		case *ast.Heading:
			flush()
			heading := nodeSourceText(n, src, sourceLines)
			currentHeader = strings.TrimSpace(heading.text.String())
			return ast.WalkSkipChildren, nil
		case *ast.Paragraph, *ast.List, *ast.Blockquote, *ast.FencedCodeBlock, *ast.CodeBlock:
			block := nodeSourceText(n, src, sourceLines)
			if current.text.Len() > 0 {
				current.generated("\n")
			}
			current.text.WriteString(block.text.String())
			current.lines = append(current.lines, block.lines...)
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	flush()
	return sections
}
