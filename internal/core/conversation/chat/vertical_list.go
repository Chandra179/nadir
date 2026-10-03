package chat

import "strings"

// Match an entire explicitly named vertical list, preserving every literal
// item and requiring a list boundary. This does not infer a category from
// keywords or accept a subset of a longer/conditional list.
func matchesVerticalList(text, claim string) bool {
	label, values, ok := strings.Cut(claim, ":")
	if !ok || strings.Count(values, ",") < 2 {
		return false
	}
	items := strings.Split(values, ",")
	for i, item := range items {
		item = strings.TrimSpace(item)
		item = strings.TrimPrefix(strings.TrimPrefix(item, "and "), "or ")
		if item == "" {
			return false
		}
		items[i] = item
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		line = strings.Join(strings.Fields(strings.TrimSpace(line)), " ")
		if !matchesVerbatimSentence(line, strings.TrimSpace(label)+":") || i+len(items) >= len(lines) {
			continue
		}
		matches := true
		for j, item := range items {
			matches = matches && strings.Join(strings.Fields(strings.TrimSpace(lines[i+j+1])), " ") == item
		}
		end := i + len(items) + 1
		if matches && (end == len(lines) || strings.TrimSpace(lines[end]) == "") {
			return true
		}
	}
	return false
}
