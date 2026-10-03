package chat

import (
	"regexp"
	"strconv"
	"strings"
)

var literalRestriction = regexp.MustCompile(`^,\s*([\p{L}_][\p{L}\p{N}_]*\s*(?:≠|≤|≥|!=|<=|>=|<|>)\s*[-+]?[\p{L}\p{N}_.]+)\.?$`)

// Preserve a recorded domain restriction when the model copied its formula
// verbatim but stopped at the comma. Only currently cited, complete evidence
// can supply it. This copies a literal suffix, not a derived restriction, and
// leaves conditions attached to non-verbatim/paraphrased formulas untouched.
func formulaRestriction(claim, marker string, citations []Citation) string {
	if !strings.Contains(claim, "=") {
		return ""
	}
	used := make(map[int]bool)
	for _, part := range strings.Split(strings.Trim(marker, "[]"), ",") {
		n, _ := strconv.Atoi(strings.TrimSpace(part))
		used[n] = true
	}
	restriction := ""
	for _, c := range citations {
		if !used[c.Number] || c.Truncated {
			continue
		}
		for _, line := range strings.Split(c.Text, "\n") {
			line = strings.Join(strings.Fields(strings.TrimSpace(line)), " ")
			if !strings.HasPrefix(line, claim) {
				continue
			}
			m := literalRestriction.FindStringSubmatch(line[len(claim):])
			if m == nil {
				continue
			}
			r := strings.TrimSuffix(m[1], ".")
			if restriction != "" && restriction != r {
				return ""
			}
			restriction = r
		}
	}
	return restriction
}
