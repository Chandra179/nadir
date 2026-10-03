package chat

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"nadir/internal/core/conversation/history"
	"nadir/internal/core/conversation/rewriting"
)

var citationMarker = regexp.MustCompile(`\[\s*\d+(?:\s*,\s*\d+)*\s*\]`)

func subjectLabel(subject *history.Subject) string {
	if subject == nil {
		return ""
	}
	return truncateToTokens(subject.Header, 160)
}

func referenceSearchQuery(query string, last rewriting.Turn) string {
	query += "\nPrevious user question: " + truncateToTokens(last.Query, 180)
	if last.Answer != "" {
		query += "\nPrevious answer (reference only): " + truncateToTokens(last.Answer, 180)
	}
	return query
}

func citedNumbers(turn history.Turn) map[int]bool {
	known, used := make(map[int]bool), make(map[int]bool)
	for _, c := range turn.Citations {
		known[c.Number] = true
	}
	for _, marker := range citationMarker.FindAllString(turn.Answer, -1) {
		for _, part := range strings.Split(strings.Trim(marker, "[]"), ",") {
			n, _ := strconv.Atoi(strings.TrimSpace(part))
			if known[n] {
				used[n] = true
			}
		}
	}
	return used
}

// Reference recognition can use terms from cited source snapshots when the
// prior answer omits a term. Those snapshots are not carried as answer facts.
func needsTurnReference(query string, turn history.Turn) bool {
	// An explicitly named known section is its own subject, even when written
	// in lowercase. Do not let a reused generic term select the prior option.
	for _, c := range turn.Citations {
		parts := strings.Split(c.Header, " > ")
		for i, part := range parts {
			if i == 0 && len(parts) > 1 {
				continue
			}
			if j := strings.Index(part, " ("); j >= 0 {
				part = part[:j]
			}
			name := normalizedReferenceWords(part)
			if len(strings.Fields(name)) >= 2 && strings.Contains(" "+normalizedReferenceWords(query)+" ", " "+name+" ") {
				return false
			}
		}
	}
	last := rewriting.Turn{Query: turn.Query, Answer: turn.Query + " " + referenceAnswer(turn)}
	used := citedNumbers(turn)
	for _, c := range turn.Citations {
		if used[c.Number] {
			last.Answer += " " + truncateToTokens(c.Header+" "+c.Text, 180)
		}
	}
	return needsReferenceContext(query, last)
}

// Accept only one explicitly named cited section. Comparisons naming several
// alternatives and negated mentions do not establish a selected alternative.
// Legacy turns derive this hint from their snapshots; new turns persist it.
func selectedSubject(turn history.Turn) *history.Subject {
	if turn.Subject != nil {
		return turn.Subject
	}
	answer := strings.ToLower(strings.Join(strings.Fields(referenceAnswer(turn)), " "))
	lead := strings.TrimLeft(answer, "* ")
	choice := false
	for _, prefix := range []string{"i recommend ", "use ", "choose ", "the "} {
		choice = choice || prefix != "the " && strings.HasPrefix(lead, prefix)
		lead = strings.TrimPrefix(lead, prefix)
	}
	q := normalizedReferenceWords(turn.Query)
	for _, intent := range []string{"should i use ", "should we use ", "should i choose ", "should we choose "} {
		choice = choice || strings.Contains(q, intent)
	}
	used := citedNumbers(turn)
	var selected *history.Subject
	var selectedName string
	for _, c := range turn.Citations {
		if !used[c.Number] {
			continue
		}
		parts := strings.Split(c.Header, " > ")
		for i := len(parts) - 1; i >= 0; i-- {
			if i == 0 && len(parts) > 1 {
				break // a shared document title cannot select an alternative
			}
			name := strings.TrimSpace(parts[i])
			if j := strings.Index(name, " ("); j >= 0 {
				name = name[:j]
			}
			if len(strings.Fields(name)) < 2 {
				continue
			}
			normalized := strings.ToLower(name)
			if !strings.Contains(answer, normalized) {
				continue
			}
			if choice && !strings.HasPrefix(lead, normalized) {
				continue // a recommendation may explain the rejected alternative
			}
			if selected != nil && (selectedName != normalized || selected.FilePath != c.FilePath || selected.Header != strings.Join(parts[:i+1], " > ")) {
				return nil
			}
			if !strings.HasPrefix(lead, normalized) {
				return nil
			}
			selectedName = normalized
			selected = &history.Subject{FilePath: c.FilePath, Header: strings.Join(parts[:i+1], " > ")}
			break
		}
	}
	return selected
}

func normalizedReferenceWords(s string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}), " ")
}

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

// Definite phrases can refer to a prior answer without a pronoun. Restrict
// fallback to a reused content term and a short English reference prefix;
// an explicitly named independent subject must keep its own context.
func needsReferenceContext(query string, last rewriting.Turn) bool {
	if unresolvedReference(query) {
		return true
	}
	q := strings.ToLower(strings.Join(strings.Fields(query), " "))
	definite := false
	for _, prefix := range []string{"why does the ", "why is the ", "how does the ", "what about the ", "and "} {
		definite = definite || strings.HasPrefix(q, prefix)
	}
	if !definite {
		return false
	}
	for i, word := range strings.Fields(query) {
		if i == 0 {
			continue
		}
		r, _ := utf8.DecodeRuneInString(word)
		if unicode.IsUpper(r) {
			return false
		}
	}
	terms := func(text string) []string {
		return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	}
	previous := make(map[string]bool)
	for _, word := range terms(last.Answer) {
		previous[word] = true
	}
	for _, word := range terms(query) {
		if utf8.RuneCountInString(word) < 4 || strings.Contains(" does what when which where have this that these those from with their they them about before after should could would matter matters ", " "+word+" ") {
			continue
		}
		if previous[word] {
			return true
		}
	}
	return false
}
