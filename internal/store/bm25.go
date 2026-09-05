package store

import (
	"hash/fnv"
	"strings"
	"unicode"
)

// vectorizeSparse builds a term-frequency sparse vector for the BM25 leg.
// Terms are hashed into a fixed index space (no persisted vocabulary), so
// ingest and query-time encodings match; IDF is applied server-side by
// Qdrant's Idf modifier.
func vectorizeSparse(text string) (indices []uint32, values []float32) {
	counts := make(map[uint32]float32)
	for _, tok := range tokenize(text) {
		h := fnv.New32a()
		h.Write([]byte(tok))
		counts[h.Sum32()]++
	}

	indices = make([]uint32, 0, len(counts))
	values = make([]float32, 0, len(counts))
	for idx, c := range counts {
		indices = append(indices, idx)
		values = append(values, c)
	}
	return indices, values
}

func tokenize(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}
