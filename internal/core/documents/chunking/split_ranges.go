package chunking

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Splits are spans of the extracted text, preserving their source map through
// recursive subdivision and overlap. Repeated text never requires searching
// for a matching occurrence to guess its location.
type textSpan struct{ start, end int }

func trimSpan(value string, span textSpan) textSpan {
	part := value[span.start:span.end]
	left := strings.TrimLeftFunc(part, unicode.IsSpace)
	span.start += len(part) - len(left)
	span.end = span.start + len(strings.TrimRightFunc(left, unicode.IsSpace))
	return span
}

func (c *dependencies) splitRanges(value string) []textSpan {
	return c.subRanges(value, textSpan{0, len(value)}, []string{"\n\n", "\n", ". ", " "})
}

func (c *dependencies) subRanges(value string, span textSpan, separators []string) []textSpan {
	if utf8.RuneCountInString(value[span.start:span.end]) <= c.chunkSize {
		return []textSpan{span}
	}
	for i, sep := range separators {
		if !strings.Contains(value[span.start:span.end], sep) {
			continue
		}
		var parts []textSpan
		start := span.start
		for {
			index := strings.Index(value[start:span.end], sep)
			if index < 0 {
				parts = append(parts, textSpan{start, span.end})
				break
			}
			parts = append(parts, textSpan{start, start + index})
			start += index + len(sep)
		}
		return c.packRanges(value, parts, separators[i+1:])
	}
	return hardSplitRanges(value, span, c.chunkSize, c.chunkOverlap)
}

func (c *dependencies) packRanges(value string, parts []textSpan, finer []string) []textSpan {
	var chunks []textSpan
	current := textSpan{}
	for _, part := range parts {
		if part.start == part.end {
			continue
		}
		if utf8.RuneCountInString(value[part.start:part.end]) > c.chunkSize {
			if current.start != current.end {
				chunks = append(chunks, current)
				current = textSpan{}
			}
			chunks = append(chunks, c.subRanges(value, part, finer)...)
			continue
		}
		if current.start == current.end {
			current = part
			continue
		}
		candidate := textSpan{current.start, part.end}
		if utf8.RuneCountInString(value[candidate.start:candidate.end]) <= c.chunkSize {
			current = candidate
			continue
		}
		chunks = append(chunks, current)
		current = textSpan{suffixStart(value, current, c.chunkOverlap), part.end}
	}
	if current.start != current.end {
		chunks = append(chunks, current)
	}
	return chunks
}

func suffixStart(value string, span textSpan, overlap int) int {
	start := span.end
	for i := 0; i < overlap && start > span.start; i++ {
		_, size := utf8.DecodeLastRuneInString(value[span.start:start])
		start -= size
	}
	return start
}

func hardSplitRanges(value string, span textSpan, size, overlap int) []textSpan {
	// Invalid direct-package configuration must never create a non-advancing
	// splitter. Bootstrap validates its configured size and overlap separately.
	size = max(1, size)
	overlap = max(0, min(overlap, size-1))
	var chunks []textSpan
	for start := span.start; start < span.end; {
		end := start
		for i := 0; i < size && end < span.end; i++ {
			_, width := utf8.DecodeRuneInString(value[end:span.end])
			end += width
		}
		chunk := textSpan{start, end}
		chunks = append(chunks, chunk)
		if end == span.end {
			break
		}
		start = suffixStart(value, chunk, overlap)
	}
	return chunks
}
