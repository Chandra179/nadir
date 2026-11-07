// Package lexical holds the provider-neutral text rules of the keyword (BM25)
// retrieval leg: how text is split into terms, which question words a natural
// language query drops, and which context line a chunk is indexed with. Storage
// Adapters encode the terms their own way (hashed sparse vectors, SQL full-text
// tables) but must agree on these rules so keyword behaviour does not depend on
// the backend.
package lexical

import (
	"strings"
	"unicode"
)

// Tokens splits text into lowercase letter and digit runs.
func Tokens(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// QueryTerms returns the terms of a natural-language question for the lexical
// leg. Common English function words are dropped because they otherwise give
// unrelated passages a rank contribution; negations and numbers are kept.
// Document text and explicit keyword lookups use Tokens and keep every term.
func QueryTerms(query string) []string {
	var terms []string
	for _, term := range Tokens(query) {
		switch term {
		case "a", "an", "the", "i", "it", "its", "we", "our", "my", "me", "you", "your",
			"what", "which", "how", "why", "do", "does", "did", "is", "are", "was", "were",
			"can", "could", "should", "would", "to", "of", "for", "from", "at", "on", "in", "and", "or", "with", "while", "when":
			continue
		}
		terms = append(terms, term)
	}
	return terms
}

// ContextualText is the text a chunk is lexically indexed with when no
// enriched text is supplied. It mirrors the chunker's contextual format so the
// keyword leg indexes the same context the dense leg embeds.
func ContextualText(filePath, header, text string) string {
	var sb strings.Builder
	sb.WriteString(filePath)
	if header != "" {
		sb.WriteString(" > ")
		sb.WriteString(header)
	}
	sb.WriteString("\n")
	sb.WriteString(text)
	return sb.String()
}
