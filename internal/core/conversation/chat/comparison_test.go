package chat

import (
	"context"
	"strings"
	"testing"

	"nadir/internal/core/retrieval/search"
)

func TestExplicitOrderAnswersRequestedPairInsteadOfThirdSubject(t *testing.T) {
	chunks := []search.Chunk{{Header: "Algorithms > Theta > Properties", Text: "Converges faster than Alpha method but slower than Omega method\nRequires an approximation"}}
	for _, tc := range []struct{ query, want string }{
		{"Which converges faster Alpha method or Omega method?", "Omega method converges faster than Alpha method [1]."},
		{"Which converges slower Omega method or Alpha method?", "Alpha method converges slower than Omega method [1]."},
	} {
		h := &fakeHistory{}
		g := &fakeGenerator{tokens: []string{"Theta is the answer [1]."}}
		d := NewDependencies(DependenciesConfig{History: h, Searcher: &fakeSearcher{chunks: chunks}, Generator: g})
		turn := d.StartTurn(context.Background(), Request{Query: tc.query, Generate: true})
		if turn.Streaming || turn.Answer != tc.want || g.got != "" {
			t.Fatalf("explicit ordered pair lost: %+v", turn)
		}
		waitFor(t, func() bool { return len(h.turns()) == 1 })
		if h.turns()[0].Answer != tc.want {
			t.Fatal("immediate comparison was not persisted")
		}
	}
}

func TestLiteralContrastKeepsCompleteSourceParagraphAndConditions(t *testing.T) {
	const paragraph = "Local buffers keep data in a process, while durable queues keep it in a separate service. A standalone local buffer is appropriate only if losing its contents is acceptable."
	citations := []Citation{{Number: 1, Text: paragraph}, {Number: 2, Text: "Durable queues charge for storage."}}
	got := AnswerExplicitComparison("How do durable queues differ from a standalone local buffer in these notes?", citations)
	if !strings.Contains(got, paragraph) || !strings.HasSuffix(got, "[1].") || strings.Contains(got, "charge") {
		t.Fatalf("contrast dropped behavior/condition or chose unrelated revenue: %q", got)
	}
}

func TestComparisonDoesNotInventMissingOrderOrSelectAmbiguousExcerpts(t *testing.T) {
	const query = "Which converges faster Alpha method or Omega method?"
	for _, text := range []string{
		"Converges faster than Alpha method but slower than Omega method if the seed is close.",
		"If the seed is close, converges faster than Alpha method but slower than Omega method.",
		"Runs faster than Alpha method but slower than Omega method.",
		"Converges faster than Another method but slower than Omega method.",
	} {
		if got := AnswerExplicitComparison(query, []Citation{{Number: 1, Text: text}}); got != "" {
			t.Fatalf("condition, operation or missing alternative ignored: %q", got)
		}
	}
	conflict := []Citation{{Number: 1, Text: "Converges faster than Alpha method but slower than Omega method"}, {Number: 2, Text: "Converges faster than Omega method but slower than Alpha method"}}
	if got := AnswerExplicitComparison(query, conflict); got != "" {
		t.Fatalf("conflicting order resolved without evidence: %q", got)
	}
	for _, c := range []Citation{
		{Number: 1, Header: "Methods > Under a close initial guess", Text: conflict[0].Text},
		{Number: 1, Truncated: true, Text: conflict[0].Text},
	} {
		if got := AnswerExplicitComparison(query, []Citation{c}); got != "" {
			t.Fatalf("source restriction or truncation ignored: %q", got)
		}
	}
	for _, citations := range [][]Citation{
		{{Number: 1, Text: "Local buffers and durable queues store work."}},
		{{Number: 1, Text: "Local buffers differ, while queues store work."}},
		{{Number: 1, Text: "Local buffers are fast, while durable queues are persistent."}, {Number: 2, Text: "Local buffers are cheap, while durable queues are costly."}},
	} {
		if got := AnswerExplicitComparison("How do local buffers differ from durable queues?", citations); got != "" {
			t.Fatalf("incomplete/ambiguous contrast became an answer: %q", got)
		}
	}
}

func TestPairedReasonsQuoteEachNamedScopeWithItsOwnEvidence(t *testing.T) {
	const query = "Why does the design use Cache for hot reads and DB for durable writes?"
	const first = "Cache path (hot read): Prefer speed when losing a cached copy is acceptable."
	const second = "DB path (durable write): Persist the result before acknowledging the write."
	citations := []Citation{{Number: 1, Text: first}, {Number: 2, Text: second}, {Number: 3, Text: "Cache and DB have different requirements."}}
	want := "The notes state:\n\n\"" + first + "\" [1].\n\n\"" + second + "\" [2]."
	if got := AnswerExplicitComparison(query, citations); got != want {
		t.Fatalf("reason scopes were merged, omitted or cited incorrectly: %q", got)
	}
	if got := AnswerExplicitComparison(query, citations[:1]); got != "" {
		t.Fatalf("missing second reason presented as complete: %q", got)
	}
	citations[1].Text = "DB path (cold read): This mentions durable writes in the body."
	if got := AnswerExplicitComparison(query, citations); got != "" {
		t.Fatalf("body terms mistaken for scope: %q", got)
	}
}
