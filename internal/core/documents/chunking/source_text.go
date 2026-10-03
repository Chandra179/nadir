package chunking

import (
	"strings"
	"unicode/utf8"

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
	header       string
	path         string
	headingPath  string
	label        string
	indexHeading string
	text         string
	lines        []int
}

// A short, fully bold first line is a conventional Markdown label even when
// its body starts on the following line of the same paragraph. Inline emphasis
// stays ordinary body text. Keep the label itself in the evidence, too.
func paragraphLabel(n ast.Node, src []byte, sourceLines []int) string {
	paragraph, ok := n.(*ast.Paragraph)
	if !ok {
		return ""
	}
	label, ok := paragraph.FirstChild().(*ast.Emphasis)
	if !ok || label.Level != 2 {
		return ""
	}
	title := nodeSourceText(label, src, sourceLines)
	value := strings.TrimSpace(title.text.String())
	if value == "" || utf8.RuneCountInString(value) > 120 {
		return ""
	}
	line := 0
	for _, at := range title.lines {
		if at > 0 {
			if line > 0 && at != line {
				return ""
			}
			line = at
		}
	}
	if line == 0 {
		return ""
	}
	for sibling := label.NextSibling(); sibling != nil; sibling = sibling.NextSibling() {
		tail := nodeSourceText(sibling, src, sourceLines)
		for _, at := range tail.lines {
			if at > 0 {
				if at <= line {
					return ""
				}
				return value
			}
		}
	}
	return value
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
	currentPath := ""
	currentHeadingPath := ""
	currentLabel := ""
	var ancestors [6]string
	var current sourceText
	flush := func() {
		if current.text.Len() > 0 {
			sections = append(sections, section{header: currentHeader, path: currentPath,
				headingPath: currentHeadingPath, label: currentLabel,
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
			level := n.(*ast.Heading).Level
			ancestors[level-1] = currentHeader
			for i := level; i < len(ancestors); i++ {
				ancestors[i] = ""
			}
			var path []string
			for _, title := range ancestors {
				if title != "" {
					path = append(path, title)
				}
			}
			currentPath = strings.Join(path, " > ")
			currentHeadingPath = currentPath
			currentLabel = ""
			return ast.WalkSkipChildren, nil
		case *ast.Paragraph, *ast.List, *ast.Blockquote, *ast.FencedCodeBlock, *ast.CodeBlock:
			if label := paragraphLabel(n, src, sourceLines); label != "" {
				flush()
				currentLabel = label
				currentPath = label
				if currentHeadingPath != "" {
					currentPath = currentHeadingPath + " > " + label
				}
			}
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
	// Qualify repeated leaf headings for indexing. Unique headings keep the
	// established embedding input so a broad document title does not overwhelm
	// a precise section heading.
	paths := make(map[string]map[string]bool)
	for _, sec := range sections {
		if paths[sec.header] == nil {
			paths[sec.header] = make(map[string]bool)
		}
		paths[sec.header][sec.headingPath] = true
	}
	for i := range sections {
		sections[i].indexHeading = sections[i].header
		if sections[i].label != "" {
			sections[i].indexHeading = strings.TrimPrefix(sections[i].header+" > "+sections[i].label, " > ")
		}
		if sections[i].header != "" && len(paths[sections[i].header]) > 1 {
			sections[i].indexHeading = sections[i].path
		}
	}
	return sections
}
