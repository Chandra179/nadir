package chat

import (
	"context"
	"strings"
	"testing"

	"nadir/internal/core/retrieval/search"
)

func TestVerbatimClaimUsesUniqueSupportingSectionInStreamAndHistory(t *testing.T) {
	const claim = "Backpressure: limit queues and concurrent work when producers are faster than consumers."
	store := &fakeHistory{}
	d := NewDependencies(DependenciesConfig{History: store, Searcher: &fakeSearcher{chunks: []search.Chunk{
		{Text: claim + " Decide whether to delay, drop, or reject work."},
		{Text: "Producers publish messages to an exchange."},
	}}, Generator: &fakeGenerator{tokens: []string{claim, " [", "2", "]."}}})
	turn := d.StartTurn(context.Background(), Request{Query: "What does backpressure do?", Generate: true})
	answer, terminal := drain(t, d, turn)
	if want := claim + " [1]."; answer != want || terminal != EventDone {
		t.Fatalf("stream=%q, terminal=%v; want %q", answer, terminal, want)
	}
	waitFor(t, func() bool { return len(store.turns()) == 1 })
	if store.turns()[0].Answer != answer {
		t.Fatalf("history differs from visible stream: %+v", store.turns()[0])
	}
}

func TestLiteralDenialDropsAnUnrelatedCitationOnlyForItsCompleteAssertion(t *testing.T) {
	const answer = "No. Scripts provide atomic execution, not a transaction with another service [1, 2]."
	citations := []Citation{{Number: 1, Text: "Scripts provide atomic execution, not a transaction with another service."}, {Number: 2, Text: "A lock expires after processing."}}
	if got := CorrectCitationAttributions("Does a script make another service atomic?", answer, citations); got != "No. Scripts provide atomic execution, not a transaction with another service [1]." {
		t.Fatalf("complete denial kept unrelated source: %q", got)
	}
	separate := strings.Replace(answer, "[1, 2]", "[1], [2]", 1)
	if got := CorrectCitationAttributions("Does a script make another service atomic?", separate, citations); got != "No. Scripts provide atomic execution, not a transaction with another service [1], [1]." {
		t.Fatalf("adjacent citation retained unrelated source: %q", got)
	}
}

func TestCompleteVerticalAndImperativeListsUseTheirActualSources(t *testing.T) {
	citations := []Citation{{Number: 1, Text: "A record stores more than its body. Record fields:\nName\nOwner\nSize\n\nA record has an identifier."}, {Number: 2, Text: "A header."}}
	const answer = "Record fields: Name, Owner, and Size [1, 2]."
	if got := CorrectCitationAttributions("Which record fields are stored?", answer, citations); got != "Record fields: Name, Owner, and Size [1]." {
		t.Fatalf("complete vertical list kept unrelated source: %q", got)
	}
	for _, text := range []string{"Record fields:\nName\nOwner\nSize\nSecret", "Record fields:\nName\nOwner\nSize only for active records\n", "If active, Record fields:\nName\nOwner\nSize\n"} {
		citations[0].Text = text
		if got := CorrectCitationAttributions("Which record fields are stored?", answer, citations); got != answer {
			t.Fatalf("incomplete/conditional vertical list accepted: %q", got)
		}
	}
	citations[0].Text = "estimate request rate, payload size, retention, and concurrent users."
	const list = "request rate, payload size, retention, and concurrent users [1, 2]."
	if got := CorrectCitationAttributions("Which workload estimates are needed?", list, citations); got != "request rate, payload size, retention, and concurrent users [1]." {
		t.Fatalf("explicit imperative list kept unrelated source: %q", got)
	}
	citations[0].Text = "estimate request rate, payload size, retention, and concurrent users only for peak load."
	if got := CorrectCitationAttributions("Which workload estimates are needed?", list, citations); got != list {
		t.Fatalf("imperative condition omitted: %q", got)
	}
}

func TestExactSubsectionLabelUsesItsSourceWithoutDroppingLabelConditions(t *testing.T) {
	const answer = "Using Unique Keys / Idempotent Updates [2]."
	citations := []Citation{{Number: 1, Text: "Using Unique Keys / Idempotent Updates: repeated writes produce the same result."}, {Number: 2, Text: "Consumers support batching."}}
	if got := CorrectCitationAttributions("How should a consumer handle duplicate writes?", answer, citations); got != "Using Unique Keys / Idempotent Updates [1]." {
		t.Fatalf("explicit subsection label cited unrelated source: %q", got)
	}
	for _, text := range []string{"Using Unique Keys / Idempotent Updates only with deduplication: repeats are safe.", "If deduplication is enabled, Using Unique Keys / Idempotent Updates: repeats are safe."} {
		citations[0].Text = text
		if got := CorrectCitationAttributions("How should a consumer handle duplicate writes?", answer, citations); got != answer {
			t.Fatalf("label condition omitted: %q", got)
		}
	}
}

func TestCompleteNamedPropertyCanRepeatInOneVersionedSection(t *testing.T) {
	const answer = "Ownership: MEMBER_OF, OWNED_BY, OPERATED_BY [2]."
	source := Citation{Number: 1, FilePath: "relations.md", Header: "Graph > Taxonomy", SourceSHA: "version1", Text: "Ownership: MEMBER_OF, OWNED_BY, OPERATED_BY\nActivity: STARTED, ENDED, PAUSED"}
	copy := source
	copy.Number = 3
	citations := []Citation{source, {Number: 2, Text: "Rules constrain legal predicates."}, copy}
	if got := CorrectCitationAttributions("Which ownership and activity examples distinguish these edges?", answer, citations); got != "Ownership: MEMBER_OF, OWNED_BY, OPERATED_BY [1]." {
		t.Fatalf("repeated exact property kept unrelated source: %q", got)
	}
	for _, changed := range []Citation{
		{Number: 3, FilePath: source.FilePath, Header: "Graph > Another Taxonomy", SourceSHA: source.SourceSHA, Text: source.Text},
		{Number: 3, FilePath: source.FilePath, Header: source.Header, SourceSHA: "version2", Text: source.Text},
	} {
		citations[2] = changed
		if got := CorrectCitationAttributions("Which examples?", answer, citations); got != answer {
			t.Fatalf("different sections/versions treated as one source: %q", got)
		}
	}
}

func TestCompleteNamedListAttributionDoesNotUseItsClippedTail(t *testing.T) {
	const query = "Which capacity limits should the design identify?"
	const answer = "memory, storage, bandwidth, or connections [2]."
	citations := []Citation{{Number: 1, Text: "Identify capacity limits: memory, storage, bandwidth, or connections."}, {Number: 2, Text: "ry, storage, bandwidth, or connections."}}
	if got := CorrectCitationAttributions(query, answer, citations); got != "memory, storage, bandwidth, or connections [1]." {
		t.Fatalf("complete list kept clipped source: %q", got)
	}
	for _, text := range []string{
		"If running in batch mode, capacity limits: memory, storage, bandwidth, or connections.",
		"Not capacity limits: memory, storage, bandwidth, or connections.",
		"Capacity limits: memory, storage, bandwidth, or connections, depending on traffic.",
		"Other criteria: memory, storage, bandwidth, or connections.",
	} {
		citations[0].Text = text
		if got := CorrectCitationAttributions(query, answer, citations); got != answer {
			t.Fatalf("list omitted condition or ignored field: %q from %q", got, text)
		}
	}
}

func TestCitationCorrectionDoesNotGuessSupport(t *testing.T) {
	for _, tc := range []struct {
		name, answer string
		chunks       []search.Chunk
	}{
		{"paraphrase", "Slow the producer to avoid overwhelming the consumer [2].", []search.Chunk{{Text: "Limit queues and concurrent work."}, {Text: "Producers publish messages."}}},
		{"ambiguous", "Limit queues and concurrent work [2].", []search.Chunk{{Text: "Limit queues and concurrent work."}, {Text: "Limit queues and concurrent work."}}},
		{"unmapped literal", "Limit queues and concurrent work [7, 9].", []search.Chunk{{Text: "Limit queues and concurrent work."}, {Text: "Other evidence."}}},
		{"negated source", "Delete the marker after processing finishes [2].", []search.Chunk{{Text: "Never Delete the marker after processing finishes."}, {Text: "Other evidence."}}},
		{"omitted source condition", "Delete the marker after processing finishes [2].", []search.Chunk{{Text: "Delete the marker after processing finishes only if the token matches."}, {Text: "Other evidence."}}},
		{"literal source brackets", "Choose the marker matching this array index [2].", []search.Chunk{{Text: "Choose the marker matching this array index [2]."}, {Text: "Other evidence."}}},
		{"short value", "42 [2].", []search.Chunk{{Text: "42"}, {Text: "Other evidence."}}},
		{"incomplete marker", "Limit queues and concurrent work [2", []search.Chunk{{Text: "Limit queues and concurrent work."}, {Text: "Other evidence."}}},
		{"case sensitive symbols", "The value of variable X is four [2].", []search.Chunk{{Text: "The value of variable x is four."}, {Text: "Other evidence."}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := NewDependencies(DependenciesConfig{Searcher: &fakeSearcher{chunks: tc.chunks}, Generator: &fakeGenerator{tokens: strings.Split(tc.answer, "")}})
			turn := d.StartTurn(context.Background(), Request{Query: "what?", Generate: true})
			got, _ := drain(t, d, turn)
			if got != tc.answer {
				t.Fatalf("changed unsupported or ambiguous marker: got %q want %q", got, tc.answer)
			}
		})
	}
}

func TestSectionQualifiedVerbatimPropertyUsesItsActualSource(t *testing.T) {
	answer := "The epsilon method does not require calculating gradients [2]."
	citations := []Citation{
		{Number: 1, Header: "Optimization > Epsilon Method > Properties", Text: "Does not require calculating gradients\nMay fail if the denominator is zero"},
		{Number: 2, Header: "Optimization > Epsilon Method", Text: "A succession of estimates approximates an optimum."},
		{Number: 3, Header: "Optimization > Delta Method > Properties", Text: "Does not require calculating gradients"},
	}
	if got := CorrectCitationAttributions("Does the epsilon method require gradients?", answer, citations); got != "The epsilon method does not require calculating gradients [1]." {
		t.Fatalf("section-qualified property cited introduction: %q", got)
	}
	for _, text := range []string{"Does not require calculating gradients if the approximation is accurate", "Does require calculating gradients", "Does not require calculating Gradients"} {
		citations[0].Text = text
		if got := CorrectCitationAttributions("Does the epsilon method require gradients?", answer, citations); got != answer {
			t.Fatalf("qualifier ignored a condition, negation or symbol case: %q with %q", got, text)
		}
	}
}
