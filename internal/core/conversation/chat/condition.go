package chat

import (
	"strings"

	"nadir/internal/core/conversation/history"
)

// missingScopedCondition prevents borrowing an event from a competing named
// alternative. This narrow English coverage guard is not an entailment test:
// recognizing a trigger permits generation, but does not prove its answer.
// Unknown phrasing and conditions outside explicit sibling sections are left
// to generation. Decisions use only evidence admitted to the current prompt.
func missingScopedCondition(query string, subject *history.Subject, citations []Citation) string {
	if subject == nil || subject.FilePath == "" {
		return ""
	}
	q := normalizedReferenceWords(query)
	condition := ""
	for _, prefix := range []string{"what happens if it ", "what if it "} {
		if strings.HasPrefix(q, prefix) {
			condition = strings.TrimPrefix(q, prefix)
			break
		}
	}
	words := strings.Fields(condition)
	if len(words) == 0 {
		return ""
	}
	trigger := words[0]
	if strings.Contains(" is are was were has have had does did can could should would will ", " "+trigger+" ") || len(trigger) < 3 {
		return ""
	}
	variants := []string{trigger}
	for _, suffix := range []string{"s", "es", "ed", "ing"} {
		if stem := strings.TrimSuffix(trigger, suffix); stem != trigger && len(stem) >= 3 {
			variants = append(variants, stem)
		}
	}
	mentionsTrigger := func(text string) bool {
		for _, word := range strings.Fields(normalizedReferenceWords(text)) {
			for _, variant := range variants {
				if word == variant || word == variant+"s" || word == variant+"es" || word == variant+"ed" || word == variant+"ing" {
					return true
				}
			}
		}
		return false
	}
	parentEnd := strings.LastIndex(subject.Header, " > ")
	if parentEnd < 0 {
		return ""
	}
	parent := subject.Header[:parentEnd] + " > "
	competing := false
	for _, citation := range citations {
		if citation.FilePath != subject.FilePath || !mentionsTrigger(citation.Text) {
			continue
		}
		if citation.Header == subject.Header || strings.HasPrefix(citation.Header, subject.Header+" > ") || strings.HasPrefix(subject.Header, citation.Header+" > ") {
			return "" // selected or shared ancestor evidence mentions the event
		}
		if strings.HasPrefix(citation.Header, parent) && !strings.Contains(strings.TrimPrefix(citation.Header, parent), " > ") {
			competing = true
		}
	}
	if !competing {
		return ""
	}
	label := subject.Header[parentEnd+3:]
	return "The retrieved notes do not establish that conditional outcome for " + truncateToTokens(label, 100) + "."
}
