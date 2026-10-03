package chat

import (
	"context"
	"strings"
	"testing"

	"nadir/internal/core/retrieval/search"
)

func TestCopiedFormulaRetainsItsRecordedDomainInStreamHistoryAndEvaluation(t *testing.T) {
	const raw = "S = W / T [2]."
	const want = "S = W / T (T ≠ 0) [2]."
	h := &fakeHistory{}
	d := NewDependencies(DependenciesConfig{History: h, Searcher: &fakeSearcher{chunks: []search.Chunk{
		{Text: "S is a rate."}, {Text: "S = W / T, T ≠ 0"},
	}}, Generator: &fakeGenerator{tokens: strings.Split(raw, "")}})
	turn := d.StartTurn(context.Background(), Request{Query: "What is the rate formula?", Generate: true})
	answer, _ := drain(t, d, turn)
	if answer != want || CorrectCitationAttributions(turn.Query, raw, turn.Citations) != want {
		t.Fatalf("formula lost its literal domain: %q", answer)
	}
	if got := CorrectCitationAttributions(turn.Query, answer, turn.Citations); got != answer {
		t.Fatalf("restriction duplicated on replay: %q", got)
	}
	waitFor(t, func() bool { return len(h.turns()) == 1 })
	if h.turns()[0].Answer != answer {
		t.Fatal("saved formula differs from visible answer")
	}
}

func TestFormulaRestrictionIsNeverInferredOrBorrowedFromAnUncitedSection(t *testing.T) {
	const answer = "S = W / T [2]."
	for _, c := range []Citation{
		{Number: 1, Text: "S = W / T, T ≠ 0"},
		{Number: 2, Text: "s = W / T, T ≠ 0"},
		{Number: 2, Text: "S = W / T, T ≠ 0 only for a constant rate"},
		{Number: 2, Text: "If the rate is constant, S = W / T, T ≠ 0"},
		{Number: 2, Text: "S = W / T, T ≠ 0", Truncated: true},
	} {
		citations := []Citation{c}
		if c.Number != 2 {
			citations = append(citations, Citation{Number: 2, Text: "Other evidence."})
		}
		if got := CorrectCitationAttributions("What is the rate?", answer, citations); got != answer {
			t.Fatalf("unrecorded or differently scoped restriction added: %q", got)
		}
	}
}
