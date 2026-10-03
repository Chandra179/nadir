package chat

import (
	"context"
	"strings"
	"testing"

	"nadir/internal/core/conversation/history"
	"nadir/internal/core/retrieval/search"
)

func TestPriorCitationNumbersCannotBecomeCurrentSources(t *testing.T) {
	h := &fakeHistory{priorTurns: []history.Turn{{Query: "What does the retry policy do?", Answer: "It retries [3, 4]. Example values [7, 9].", Citations: []history.Citation{{Number: 3}, {Number: 4}}}}}
	rw := &fakeRewriter{rewritten: "Why does the retry policy matter?"}
	gen := &fakeGenerator{tokens: []string{"Current answer [1]."}}
	d := NewDependencies(DependenciesConfig{History: h, Rewriter: rw, Generator: gen, Searcher: &fakeSearcher{chunks: []search.Chunk{{Text: "Current evidence"}}}})
	turn := d.StartTurn(context.Background(), Request{Query: "Why does it matter?", SessionID: "s1", Generate: true})
	if strings.Contains(rw.gotTurns[0].Answer, "[3, 4]") || !strings.Contains(rw.gotTurns[0].Answer, "[7, 9]") || strings.Contains(gen.got, "[3, 4]") {
		t.Fatalf("prior citation numbers leaked into the next request: rewrite=%+v prompt=%q", rw.gotTurns, gen.got)
	}
	drain(t, d, turn)
}

func TestUnresolvedUnchangedRewriteRetainsPreviousQuestionForRetrieval(t *testing.T) {
	query := "Does the second one keep ordering?"
	h := &fakeHistory{priorTurns: []history.Turn{{Query: "Compare TCP first and UDP second."}}}
	searcher := &fakeSearcher{}
	d := NewDependencies(DependenciesConfig{History: h, Rewriter: &fakeRewriter{rewritten: query}, Searcher: searcher})
	turn := d.StartTurn(context.Background(), Request{Query: query, SessionID: "s1"})
	if !strings.Contains(searcher.gotQuery, query) || !strings.Contains(searcher.gotQuery, "Compare TCP first and UDP second.") || turn.Query != query {
		t.Fatalf("unresolved reference lost its subject: searched=%q turn=%+v", searcher.gotQuery, turn)
	}
}

func TestStandaloneQuestionWithLocalPronounKeepsItsOwnSubject(t *testing.T) {
	query := "How does Redis choose its cluster slot?"
	h := &fakeHistory{priorTurns: []history.Turn{{Query: "Compare TCP first and UDP second."}}}
	searcher := &fakeSearcher{}
	d := NewDependencies(DependenciesConfig{History: h, Rewriter: &fakeRewriter{rewritten: query}, Searcher: searcher})
	d.StartTurn(context.Background(), Request{Query: query, SessionID: "s1"})
	if searcher.gotQuery != query {
		t.Fatalf("standalone question acquired an unrelated prior subject: %q", searcher.gotQuery)
	}
}

func TestUnresolvedConditionalReferenceKeepsTheSelectedAlternative(t *testing.T) {
	query := "What happens if it fails before finishing?"
	h := &fakeHistory{priorTurns: []history.Turn{{Query: "Should I use option Alpha or Beta?", Answer: "Use option Beta [2].", Citations: []history.Citation{{Number: 2}}}}}
	searcher := &fakeSearcher{}
	d := NewDependencies(DependenciesConfig{History: h, Rewriter: &fakeRewriter{rewritten: query}, Searcher: searcher})
	turn := d.StartTurn(context.Background(), Request{Query: query, SessionID: "s1"})
	if !strings.Contains(searcher.gotQuery, "Use option Beta") || strings.Contains(searcher.gotQuery, "[2]") || turn.Query != query {
		t.Fatalf("conditional reference lost its selected alternative or reused citations: searched=%q turn=%+v", searcher.gotQuery, turn)
	}
	query = "What happens if Redis restarts?"
	d = NewDependencies(DependenciesConfig{History: h, Rewriter: &fakeRewriter{rewritten: query}, Searcher: searcher})
	d.StartTurn(context.Background(), Request{Query: query, SessionID: "s1"})
	if searcher.gotQuery != query {
		t.Fatalf("named conditional subject inherited an unrelated prior option: %q", searcher.gotQuery)
	}
}
