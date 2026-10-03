package chat

import (
	"fmt"
	"regexp"
	"strings"
)

var orderQuestion = regexp.MustCompile(`(?i)^which ([a-z]+) (faster|slower) (.+?) or (.+?)\??$`)
var explicitOrder = regexp.MustCompile(`(?i)^([a-z]+) faster than (.+?) but slower than (.+?)\.?$`)
var contrastQuestion = regexp.MustCompile(`(?i)^how (?:does|do) (.+?) differ from (.+?)\??$`)
var reasonPairQuestion = regexp.MustCompile(`(?i)^why .+? (?:choose|use|favor|favour) ([a-z0-9_-]+) for (.+?) and ([a-z0-9_-]+) for (.+?)\??$`)

// AnswerExplicitComparison uses only a directly recorded order relation or a
// complete literal contrast paragraph. It avoids model synthesis for these
// narrow English forms. It neither assesses arbitrary entailment nor resolves
// ambiguous/contradictory comparisons; those retain the normal generation path.
func AnswerExplicitComparison(query string, citations []Citation) string {
	q := strings.TrimSpace(query)
	if m := reasonPairQuestion.FindStringSubmatch(q); m != nil {
		var parts []string
		for _, option := range [][2]string{{m[1], m[2]}, {m[3], strings.TrimSuffix(m[4], "?")}} {
			policy, subject := normalizedReferenceWords(option[0]), comparisonTerms(option[1])
			paragraph, source := "", 0
			for _, c := range citations {
				for _, line := range strings.Split(c.Text, "\n") {
					line = strings.TrimSpace(line)
					label, _, ok := strings.Cut(line, ":")
					if !ok || !completeComparisonParagraph(line) || !strings.HasPrefix(normalizedReferenceWords(label)+" ", policy+" ") || !containsComparisonTerms(label, subject) {
						continue
					}
					if paragraph != "" && paragraph != line {
						return ""
					}
					paragraph, source = line, c.Number
				}
			}
			if paragraph == "" {
				return "" // both expressly requested reasons require evidence
			}
			parts = append(parts, fmt.Sprintf("%q [%d].", paragraph, source))
		}
		return "The notes state:\n\n" + strings.Join(parts, "\n\n")
	}
	if m := orderQuestion.FindStringSubmatch(q); m != nil {
		answer, source := "", 0
		for _, c := range citations {
			if c.Truncated || conditionalComparisonHeading(c.Header) {
				continue
			}
			for _, line := range strings.Split(c.Text, "\n") {
				r := explicitOrder.FindStringSubmatch(strings.TrimSpace(line))
				if r == nil || !strings.EqualFold(m[1], r[1]) {
					continue
				}
				low, high := strings.TrimSpace(r[2]), strings.TrimSuffix(strings.TrimSpace(r[3]), ".")
				same := func(a, b string) bool {
					return len([]rune(a)) >= 3 && normalizedReferenceWords(a) == normalizedReferenceWords(b)
				}
				if !(same(m[3], low) && same(m[4], high) || same(m[3], high) && same(m[4], low)) {
					continue
				}
				result := fmt.Sprintf("%s %s faster than %s", high, strings.ToLower(r[1]), low)
				if strings.EqualFold(m[2], "slower") {
					result = fmt.Sprintf("%s %s slower than %s", low, strings.ToLower(r[1]), high)
				}
				if answer != "" && answer != result {
					return "" // conflicting recorded order, not a model verdict
				}
				answer, source = result, c.Number
			}
		}
		if answer != "" {
			return fmt.Sprintf("%s [%d].", answer, source)
		}
	}
	if m := contrastQuestion.FindStringSubmatch(q); m != nil {
		left, right := m[1], strings.TrimSuffix(m[2], "?")
		if i := strings.Index(strings.ToLower(right), " in "); i >= 0 {
			right = right[:i] // preserve the full question; isolate its named option
		}
		l, r := comparisonTerms(left), comparisonTerms(right)
		if len(l) < 2 || len(r) < 2 {
			return ""
		}
		paragraph, source := "", 0
		for _, c := range citations {
			for _, line := range strings.Split(c.Text, "\n") {
				line = strings.TrimSpace(line)
				if !completeComparisonParagraph(line) || !strings.Contains(strings.ToLower(line), " while ") {
					continue
				}
				if !containsComparisonTerms(line, l) || !containsComparisonTerms(line, r) {
					continue
				}
				if paragraph != "" && paragraph != line {
					return "" // multiple different contrast excerpts
				}
				paragraph, source = line, c.Number
			}
		}
		if paragraph != "" {
			return fmt.Sprintf("The notes state: %q [%d].", paragraph, source)
		}
	}
	return ""
}

func conditionalComparisonHeading(header string) bool {
	for _, part := range strings.Split(strings.ToLower(header), " > ") {
		for _, prefix := range []string{"if ", "when ", "under ", "assuming ", "only ", "with ", "unless ", "conditional"} {
			if strings.HasPrefix(strings.TrimSpace(part), prefix) {
				return true
			}
		}
	}
	return false
}

func completeComparisonParagraph(line string) bool {
	return len(line) > 0 && len(line) <= 1200 && strings.ContainsAny(line[len(line)-1:], ".!?")
}

func comparisonTerms(s string) []string {
	var out []string
	for _, word := range strings.Fields(normalizedReferenceWords(s)) {
		if !strings.Contains(" a an the ", " "+word+" ") {
			out = append(out, strings.TrimSuffix(word, "s"))
		}
	}
	return out
}

func containsComparisonTerms(text string, required []string) bool {
	if len(required) == 0 {
		return false
	}
	actual := make(map[string]bool)
	for _, word := range comparisonTerms(text) {
		actual[word] = true
	}
	for _, word := range required {
		if !actual[word] {
			return false
		}
	}
	return true
}
