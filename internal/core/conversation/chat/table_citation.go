package chat

import (
	"regexp"
	"strconv"
	"strings"
)

var tableRowBoundary = regexp.MustCompile(`\|[ \t]*\|`)
var literalQueryWord = regexp.MustCompile(`[\p{L}\p{N}_.-]+`)
var headingAbbreviation = regexp.MustCompile(`^(.+?) \(([\p{L}\p{N}_-]+)\)$`)

// A short value is identifiable only when the question names a unique table
// row and column and the entire answer is that cell's literal content. Aliases
// must be declared by an admitted source heading, never inferred from a value.
// This is attribution, not table reasoning or a general support validator.
func tableValueSource(query, claim string, citations []Citation) int {
	lower := strings.ToLower(strings.TrimSpace(query))
	words := literalQueryWord.FindAllString(lower, -1)
	conversion := false
	if len(words) > 0 && strings.Contains(lower, " expressed in ") {
		_, err := strconv.ParseFloat(words[0], 64)
		conversion = err == nil
	}
	if claim == "" || !(strings.HasPrefix(lower, "what is ") || strings.HasPrefix(lower, "what's ") || strings.HasPrefix(lower, "value of ") || conversion) {
		return 0
	}
	queryWords := literalQueryWord.FindAllString(strings.ToLower(query), -1)
	q := " " + strings.Join(queryWords, " ") + " "
	matchesLabel := func(label string) bool {
		words := literalQueryWord.FindAllString(strings.ToLower(strings.TrimSpace(label)), -1)
		return len(words) > 0 && strings.Contains(q, " "+strings.Join(words, " ")+" ")
	}
	columnNamed := func(column string) bool {
		if matchesLabel(column) {
			return true
		}
		for _, c := range citations {
			for _, heading := range strings.Split(c.Header, " > ") {
				alias := headingAbbreviation.FindStringSubmatch(heading)
				if len(alias) == 3 && alias[2] == column && matchesLabel(alias[1]) {
					return true
				}
			}
		}
		return false
	}
	match := 0
	for _, c := range citations {
		text := restoreTableRows(c.Text)
		lines := strings.Split(text, "\n")
		for i := 0; i+2 < len(lines); i++ {
			headers := tableCells(lines[i])
			separator := tableCells(lines[i+1])
			if len(headers) < 2 || len(separator) != len(headers) || !tableSeparator(separator) {
				continue
			}
			if conversion && !columnNamed(headers[0]) {
				continue // the source unit must be named, not merely the number
			}
			column := -1
			for j := 1; j < len(headers); j++ {
				if columnNamed(headers[j]) {
					if column != -1 {
						column = -1
						break
					}
					column = j
				}
			}
			if column == -1 {
				continue
			}
			for j := i + 2; j < len(lines); j++ {
				row := tableCells(lines[j])
				if len(row) != len(headers) {
					break
				}
				if !matchesLabel(row[0]) {
					continue
				}
				// A matching row with a different value or a second matching
				// row/source makes the lookup ambiguous, even if the answer
				// happens to equal one of the cells.
				if row[column] != claim || match != 0 {
					return 0
				}
				match = c.Number
			}
		}
	}
	return match
}

// Restore old, flattened Markdown tables only after validating every row's
// width and the separator. Width determines boundaries, so empty cells are
// retained. Other lines, code operators and incomplete fragments stay literal.
func restoreTableRows(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		boundary := tableRowBoundary.FindStringIndex(line)
		if boundary == nil || len(line) < 2 || line[0] != '|' || line[len(line)-1] != '|' {
			continue
		}
		width := len(tableCells(line[:boundary[0]+1]))
		if width < 2 {
			continue
		}
		cells := strings.Split(line[1:len(line)-1], "|")
		var rows [][]string
		valid := true
		for pos := 0; pos < len(cells); {
			if pos+width > len(cells) {
				valid = false
				break
			}
			row := cells[pos : pos+width]
			for j := range row {
				row[j] = strings.TrimSpace(row[j])
			}
			rows = append(rows, row)
			pos += width
			if pos < len(cells) {
				if strings.TrimSpace(cells[pos]) != "" {
					valid = false
					break
				}
				pos++ // a closing pipe followed by the next row's opening pipe
			}
		}
		if !valid || len(rows) < 3 || !tableSeparator(rows[1]) {
			continue
		}
		formatted := make([]string, len(rows))
		for j, row := range rows {
			formatted[j] = "| " + strings.Join(row, " | ") + " |"
		}
		lines[i] = strings.Join(formatted, "\n")
	}
	return strings.Join(lines, "\n")
}

func tableCells(line string) []string {
	line = strings.TrimSpace(line)
	if len(line) < 2 || !strings.HasPrefix(line, "|") || !strings.HasSuffix(line, "|") {
		return nil
	}
	cells := strings.Split(line[1:len(line)-1], "|")
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	return cells
}

func tableSeparator(cells []string) bool {
	for _, cell := range cells {
		if len(strings.Trim(cell, ":")) < 3 || strings.Trim(cell, "-:") != "" {
			return false
		}
	}
	return true
}
