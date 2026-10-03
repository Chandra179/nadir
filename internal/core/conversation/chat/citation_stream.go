package chat

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// citationStream corrects only a uniquely identifiable verbatim attribution.
// It does not assess entailment. Content streams immediately; only a bracketed
// marker is held until complete, so replay and history see the same number.
// State is bounded even for a provider that emits no paragraph boundaries.
type citationStream struct {
	query          string
	citations      []Citation
	clause         string
	previousClause string
	marker         string
}

func (s *citationStream) push(text string) string {
	var out strings.Builder
	for len(text) > 0 {
		if s.marker != "" {
			b := text[0]
			text = text[1:]
			s.marker += string(b)
			if b == ']' {
				out.WriteString(s.correct(s.marker))
				if strings.TrimSpace(s.clause) != "" && strings.TrimSpace(s.clause) != "," {
					s.previousClause = s.clause
				}
				s.marker, s.clause = "", ""
			} else if !((b >= '0' && b <= '9') || b == ',' || b == ' ' || b == '\t') || len(s.marker) > 64 {
				out.WriteString(s.marker)
				s.remember(s.marker)
				s.marker = ""
			}
			continue
		}
		i := strings.IndexByte(text, '[')
		if i < 0 {
			i = len(text)
		}
		out.WriteString(text[:i])
		s.remember(text[:i])
		text = text[i:]
		if len(text) == 0 {
			break
		}
		text = text[1:]
		if strings.HasSuffix(s.clause, " ") {
			s.marker = "["
		} else {
			out.WriteByte('[')
			s.remember("[")
		}
	}
	return out.String()
}

func (s *citationStream) remember(text string) {
	s.clause += text
	if i := strings.LastIndexByte(s.clause, '\n'); i >= 0 {
		s.previousClause = ""
		s.clause = s.clause[i+1:]
	}
	if len(s.clause) > 4096 {
		s.clause = s.clause[len(s.clause)-4096:]
	}
}

func (s *citationStream) flush() string {
	text := s.marker
	s.marker = ""
	return text
}

func (s *citationStream) correct(marker string) string {
	if !citationMarker.MatchString(marker) {
		return marker
	}
	known := make(map[int]bool, len(s.citations))
	for _, c := range s.citations {
		known[c.Number] = true
	}
	for _, p := range strings.Split(strings.Trim(marker, "[]"), ",") {
		n, _ := strconv.Atoi(strings.TrimSpace(p))
		if !known[n] {
			return marker
		}
	}
	clause := s.clause
	if (strings.TrimSpace(clause) == "" || strings.TrimSpace(clause) == ",") && s.previousClause != "" {
		clause = s.previousClause // adjacent markers cite the same assertion
	}
	claim := strings.Join(strings.Fields(strings.TrimSpace(clause)), " ")
	if restriction := formulaRestriction(strings.TrimSuffix(claim, "."), marker, s.citations); restriction != "" {
		return "(" + restriction + ") " + marker
	}
	if number := tableValueSource(s.query, strings.TrimSuffix(claim, "."), s.citations); number != 0 {
		return fmt.Sprintf("[%d]", number)
	}
	// A lone number or short phrase has too little identity to establish which
	// section supports the answer. Preserve case and mathematical punctuation.
	namedList := len(strings.Fields(claim)) >= 4 && strings.Contains(claim, ":") && strings.Count(claim, ",") >= 2
	if len(strings.Fields(claim)) < 5 && !namedList {
		return marker
	}
	for _, c := range s.citations {
		text := strings.Join(strings.Fields(c.Text), " ")
		if strings.Contains(text, claim+" "+marker) {
			return marker
		} // literal source brackets
	}
	claim = strings.TrimSuffix(claim, ".")
	for _, prefix := range []string{"No. ", "Yes. "} {
		claim = strings.TrimPrefix(claim, prefix)
	}
	match := 0
	var source Citation
	for _, c := range s.citations {
		if matchesVerbatimSentence(c.Text, claim) || matchesQualifiedProperty(c, claim) || matchesNamedList(s.query, c.Text, claim) || matchesExactFieldLabel(c.Text, claim) || matchesVerticalList(c.Text, claim) {
			if match != 0 {
				// Overlapping windows from the same versioned section can
				// repeat one complete assertion. They are one source identity;
				// genuinely different or unversioned sources stay ambiguous.
				if source.FilePath != "" && source.Header != "" && source.SourceSHA != "" && source.FilePath == c.FilePath && source.Header == c.Header && source.SourceSHA == c.SourceSHA {
					continue
				}
				return marker
			}
			match = c.Number
			source = c
		}
	}
	if match == 0 {
		return marker
	}
	return fmt.Sprintf("[%d]", match)
}

// A complete, explicit label can itself answer a lookup. Match the entire
// label at a line start; never a prefix of a conditional label or a heading
// inferred from a body substring. The minimum-length guard remains in correct.
func matchesExactFieldLabel(text, claim string) bool {
	for _, line := range strings.Split(text, "\n") {
		label, _, ok := strings.Cut(strings.TrimSpace(line), ":")
		if ok && strings.Join(strings.Fields(label), " ") == claim {
			return true
		}
	}
	return false
}

// A complete verbatim list can follow a named field rather than a sentence
// boundary. Require at least two content terms from that field in the question
// and copy the entire value; never treat a partial list or generic prefix as
// support. Conditions or negations in a prefix make it ineligible.
func matchesNamedList(query, text, claim string) bool {
	if strings.Count(claim, ",") < 2 {
		return false
	}
	terms := func(s string) map[string]bool {
		out := map[string]bool{}
		for _, word := range strings.Fields(normalizedReferenceWords(s)) {
			if len(word) >= 4 && !strings.Contains(" what which should before after from that this they have most identify ", " "+word+" ") {
				out[strings.TrimSuffix(word, "s")] = true
			}
		}
		return out
	}
	queryTerms := terms(query)
	for _, line := range strings.Split(text, "\n") {
		prefix, value, ok := strings.Cut(line, ":")
		if !ok {
			// An explicit imperative can label a complete list without a
			// colon. Require that same action in the question.
			verb, rest, found := strings.Cut(strings.TrimSpace(line), " ")
			if found && queryTerms[strings.ToLower(verb)] && strings.Contains(" collect estimate identify list measure ", " "+strings.ToLower(verb)+" ") && strings.TrimSuffix(strings.Join(strings.Fields(rest), " "), ".") == claim {
				return true
			}
			continue
		}
		if strings.TrimSuffix(strings.Join(strings.Fields(strings.TrimSpace(value)), " "), ".") != claim {
			continue
		}
		p := " " + normalizedReferenceWords(prefix) + " "
		for _, qualifier := range []string{" if ", " unless ", " except ", " when ", " only ", " not ", " never ", " without "} {
			if strings.Contains(p, qualifier) {
				return false
			}
		}
		overlaps := 0
		for term := range terms(prefix) {
			if queryTerms[term] {
				overlaps++
			}
		}
		if overlaps >= 2 {
			return true
		}
	}
	return false
}

// A property line may omit the subject supplied by its Markdown heading.
// Permit only that exact heading prefix and sentence-initial capitalization;
// the entire remaining assertion must still be a complete verbatim property.
func matchesQualifiedProperty(c Citation, claim string) bool {
	words := strings.Fields(claim)
	if len(words) > 0 && strings.EqualFold(words[0], "the") {
		words = words[1:]
	}
	for _, heading := range strings.Split(c.Header, " > ") {
		if i := strings.Index(heading, " ("); i >= 0 {
			heading = heading[:i]
		}
		subject := strings.Fields(heading)
		if len(subject) < 2 || len(words) <= len(subject) {
			continue
		}
		matches := true
		for i, word := range subject {
			matches = matches && strings.EqualFold(words[i], word)
			// Single-letter names can be case-sensitive mathematical symbols.
			if len([]rune(word)) == 1 {
				matches = matches && words[i] == word
			}
		}
		if !matches {
			continue
		}
		predicate := strings.Join(words[len(subject):], " ")
		if matchesVerbatimSentence(c.Text, predicate) {
			return true
		}
		first := []rune(words[len(subject)])
		letters := len(first) > 1
		for _, r := range first {
			letters = letters && unicode.IsLetter(r)
		}
		if letters {
			runes := []rune(predicate)
			runes[0] = unicode.ToUpper(runes[0])
			if matchesVerbatimSentence(c.Text, string(runes)) {
				return true
			}
		}
	}
	return false
}

// CorrectCitationAttributions applies the same conservative stream correction
// to an evaluator's collected answer. It does not validate semantic support.
func CorrectCitationAttributions(query, answer string, citations []Citation) string {
	stream := citationStream{query: query, citations: citations}
	return stream.push(answer) + stream.flush()
}

func matchesVerbatimSentence(text, claim string) bool {
	// Retain line boundaries: a condition or negation just before/after the
	// copied words must not be mistaken for a supported complete assertion.
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.Join(strings.Fields(line), " ")
	}
	text = strings.Join(lines, "\n")
	for offset := 0; offset < len(text); {
		i := strings.Index(text[offset:], claim)
		if i < 0 {
			break
		}
		start := offset + i
		end := start + len(claim)
		before := strings.TrimRight(text[:start], " ")
		after := text[end:]
		boundaryBefore := before == "" || strings.HasSuffix(before, "\n") || strings.ContainsAny(before[len(before)-1:], ".!?")
		boundaryAfter := after == "" || after[0] == '\n' || (strings.ContainsAny(after[:1], ".!?") && (len(after) == 1 || after[1] == ' ' || after[1] == '\n'))
		if boundaryBefore && boundaryAfter {
			return true
		}
		offset = start + 1
	}
	return false
}
