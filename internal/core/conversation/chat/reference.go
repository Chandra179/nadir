package chat

import (
	"regexp"
	"strconv"
	"strings"

	"nadir/internal/core/conversation/history"
)

var citationMarker = regexp.MustCompile(`\[\s*\d+(?:\s*,\s*\d+)*\s*\]`)

// Citation numbers belong to one turn. They must not become candidate source
// numbers for the next answer. Preserve other bracketed values as reference
// text; only markers mapped in that persisted turn are removed.
func referenceAnswer(turn history.Turn) string {
	known := make(map[int]bool, len(turn.Citations))
	for _, citation := range turn.Citations {
		known[citation.Number] = true
	}
	return citationMarker.ReplaceAllStringFunc(turn.Answer, func(marker string) string {
		for _, part := range strings.Split(strings.Trim(marker, "[]"), ",") {
			number, _ := strconv.Atoi(strings.TrimSpace(part))
			if !known[number] {
				return marker
			}
		}
		return ""
	})
}

func unresolvedReference(query string) bool {
	q := strings.ToLower(strings.Join(strings.Fields(query), " "))
	// Ordinal references have no subject of their own. Generic pronouns may
	// refer to a subject within this question ("Redis and its cluster slots"),
	// so leave those to the rewriter instead of adding an unrelated prior topic.
	for _, phrase := range []string{"the second one", "the first one", "the other one"} {
		if strings.Contains(q, phrase) {
			return true
		}
	}
	for _, prefix := range []string{"what happens if it ", "what if it ", "does it ", "can it ", "why does it "} {
		if strings.HasPrefix(q, prefix) && !strings.HasPrefix(q, "does it matter ") {
			return true
		}
	}
	return false
}
