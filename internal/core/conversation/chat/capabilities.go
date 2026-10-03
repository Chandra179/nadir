package chat

import (
	"net/netip"
	"strings"
)

const liveStateUnavailable = "I can search your indexed notes, but I cannot observe the current state of your systems."

// Nadir has no live-system tools. Explicit requests for a system's
// present state cannot be satisfied by a static document or its example values.
// This narrow English guard complements grounding instructions; it is not a
// general answerability classifier. Source lookups and how-to questions still
// go through retrieval, including documents that describe live systems.
func requiresLiveObservation(query string) bool {
	q := " " + strings.ToLower(strings.Join(strings.Fields(query), " ")) + " "
	for _, source := range []string{" notes", " document", " example", "according to"} {
		if strings.Contains(q, source) {
			return false
		}
	}
	presentSystem := strings.Contains(q, "actual production") ||
		(strings.Contains(q, "officially report") && strings.Contains(q, "today"))
	for _, term := range []string{" my ", " our ", " on this ", " on the local "} {
		presentSystem = presentSystem || strings.Contains(q, term)
	}
	if !presentSystem {
		return false
	}
	temporal := false
	for _, term := range []string{"right now", "currently", "at the moment", "as of now", "at present", " current ", " latest ", "today"} {
		temporal = temporal || strings.Contains(q, term)
	}
	if !temporal {
		return false
	}
	for _, prefix := range []string{" what ", " what's ", " which ", " how many ", " how much ", " is ", " are "} {
		if strings.HasPrefix(q, prefix) {
			return true
		}
	}
	return false
}

// A literal-address lookup requires an address in admitted evidence. This
// guard only rejects its absence; an address elsewhere in the notes does not
// establish that it belongs to the requested host. How-to questions and the
// existing live-observation policy remain separate.
func missingLiteralAddress(query string, citations []Citation) string {
	q := strings.ToLower(strings.Join(strings.Fields(query), " "))
	if !(strings.HasPrefix(q, "what is ") || strings.HasPrefix(q, "what's ")) ||
		!(strings.Contains(q, "the ip address of ") || strings.Contains(q, "the ip address for ")) {
		return ""
	}
	for _, c := range citations {
		for _, token := range strings.FieldsFunc(c.Text, func(r rune) bool {
			return !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F' || r == '.' || r == ':')
		}) {
			token = strings.Trim(token, ".")
			if _, err := netip.ParseAddr(token); err == nil {
				return ""
			}
			if _, err := netip.ParseAddrPort(token); err == nil {
				return ""
			}
		}
	}
	return "The retrieved notes do not specify that IP address."
}
