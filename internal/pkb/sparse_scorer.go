package pkb

import "strings"

// TFSparseScorer scores by raw term frequency — sum of query term occurrences in text.
// Fast, zero-dependency default for the BM25 hybrid search leg.
type TFSparseScorer struct{}

func (TFSparseScorer) Score(query, text string) float64 {
	textLower := strings.ToLower(text)
	var score float64
	for _, term := range strings.Fields(strings.ToLower(query)) {
		if term != "" {
			score += float64(strings.Count(textLower, term))
		}
	}
	return score
}
