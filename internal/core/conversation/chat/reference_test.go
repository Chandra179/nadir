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
	if strings.Contains(rw.gotTurns[0].Answer, "[3, 4]") || !strings.Contains(rw.gotTurns[0].Answer, "[7, 9]") || strings.Contains(awaitPrompt(t, gen), "[3, 4]") {
		t.Fatalf("prior citation numbers leaked into the next request: rewrite=%+v prompt=%q", rw.gotTurns, awaitPrompt(t, gen))
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

func TestUnchangedDefiniteFollowupRetainsThePreviousSubject(t *testing.T) {
	query := "Why does the checksum comparison matter?"
	h := &fakeHistory{priorTurns: []history.Turn{{Query: "When should I verify an imported document?", Answer: "Compare the checksum before accepting the document."}}}
	searcher := &fakeSearcher{}
	d := NewDependencies(DependenciesConfig{History: h, Rewriter: &fakeRewriter{rewritten: query}, Searcher: searcher})
	d.StartTurn(context.Background(), Request{Query: query, SessionID: "s1"})
	if !strings.Contains(searcher.gotQuery, "imported document") {
		t.Fatalf("definite follow-up lost its subject: %q", searcher.gotQuery)
	}
}

func TestStandaloneQuestionDoesNotGiveGenerationAnUnrelatedPriorTopic(t *testing.T) {
	query := "How does Redis choose its cluster slot?"
	h := &fakeHistory{priorTurns: []history.Turn{{Query: "Compare TCP first and UDP second.", Answer: "UDP uses datagrams."}}}
	gen := &fakeGenerator{tokens: []string{"Current answer"}}
	d := NewDependencies(DependenciesConfig{History: h, Rewriter: &fakeRewriter{rewritten: query}, Generator: gen, Searcher: &fakeSearcher{chunks: []search.Chunk{{Text: "Redis evidence"}}}})
	turn := d.StartTurn(context.Background(), Request{Query: query, SessionID: "s1", Generate: true})
	if strings.Contains(awaitPrompt(t, gen), "TCP") || strings.Contains(awaitPrompt(t, gen), "UDP") {
		t.Fatalf("standalone generation acquired an unrelated subject: %q", awaitPrompt(t, gen))
	}
	drain(t, d, turn)
}

func TestSelectedAlternativeIsExplicitDespiteAChangedRewrite(t *testing.T) {
	query := "What happens if it fails before finishing?"
	h := &fakeHistory{priorTurns: []history.Turn{{Query: "Should I use immediate or durable transfers?", Answer: "Durable transfers [2].", Citations: []history.Citation{
		{Number: 1, FilePath: "transfer.md", Header: "Transfers > Immediate transfers", Text: "Immediate transfers may lose work after a crash."},
		{Number: 2, FilePath: "transfer.md", Header: "Transfers > Durable transfers", Text: "Acknowledge after successful processing."},
	}}}}
	searcher := &fakeSearcher{chunks: []search.Chunk{
		{FilePath: "transfer.md", SectionPath: "Transfers > Immediate transfers", Text: "Immediate transfers may lose work after a crash."},
		{FilePath: "transfer.md", SectionPath: "Transfers > Durable transfers", Text: "Acknowledge after successful processing."},
	}}
	gen := &fakeGenerator{tokens: []string{"The notes do not specify crash recovery."}}
	d := NewDependencies(DependenciesConfig{History: h, Rewriter: &fakeRewriter{rewritten: "What happens when immediate transfers fail?"}, Generator: gen, Searcher: searcher})
	turn := d.StartTurn(context.Background(), Request{Query: query, SessionID: "s1", Generate: true})
	if !strings.Contains(searcher.gotQuery, "Durable transfers") || strings.Contains(searcher.gotQuery, "immediate transfers fail") {
		t.Fatalf("rewrite replaced selected alternative: %q", searcher.gotQuery)
	}
	if !strings.Contains(awaitPrompt(t, gen), "Resolved conversation subject: Transfers > Durable transfers") || len(turn.Citations) != 2 || turn.Query != query {
		t.Fatalf("explicit subject, contrast evidence or raw intent lost: %+v prompt=%q", turn, awaitPrompt(t, gen))
	}
	drain(t, d, turn)
}

func TestDefiniteReferenceCanUseCitedEvidenceWithoutCopyingItAsFacts(t *testing.T) {
	query := "Why does the checksum comparison matter?"
	h := &fakeHistory{priorTurns: []history.Turn{{Query: "May an old uploader replace a newer document?", Answer: "No, an old uploader could replace the newer document [1].", Citations: []history.Citation{
		{Number: 1, FilePath: "upload.md", Header: "Upload ownership", Text: "Compare the checksum before replacing the document. A mismatch identifies a different version."},
		{Number: 2, FilePath: "unrelated.md", Header: "Unused", Text: "Uncited resource with private metadata."},
	}}}}
	searcher := &fakeSearcher{chunks: []search.Chunk{{Text: "Current evidence"}}}
	gen := &fakeGenerator{tokens: []string{"Current answer"}}
	d := NewDependencies(DependenciesConfig{History: h, Rewriter: &fakeRewriter{rewritten: query}, Generator: gen, Searcher: searcher})
	turn := d.StartTurn(context.Background(), Request{Query: query, SessionID: "s1", Generate: true})
	if !strings.Contains(searcher.gotQuery, "old uploader") || strings.Contains(awaitPrompt(t, gen), "A mismatch identifies") || strings.Contains(searcher.gotQuery, "private metadata") {
		t.Fatalf("cited reference lost or evidence/uncited details leaked: search=%q prompt=%q", searcher.gotQuery, awaitPrompt(t, gen))
	}
	drain(t, d, turn)
}

func TestFollowupContextWorksWithoutTheOptionalRewriter(t *testing.T) {
	query := "What happens if it fails before finishing?"
	h := &fakeHistory{priorTurns: []history.Turn{{Query: "Which transfer mode should I use?", Answer: "Durable transfers [1].", Citations: []history.Citation{{Number: 1, Header: "Transfers > Durable transfers", FilePath: "transfer.md"}}}}}
	searcher := &fakeSearcher{}
	d := NewDependencies(DependenciesConfig{History: h, Searcher: searcher})
	d.StartTurn(context.Background(), Request{Query: query, SessionID: "s1"})
	if !strings.Contains(searcher.gotQuery, "Durable transfers") {
		t.Fatalf("disabled optional rewriter erased reference: %q", searcher.gotQuery)
	}
}

func TestSelectedSubjectSurvivesADeclineAndClearsOnTopicSwitch(t *testing.T) {
	subject := &history.Subject{FilePath: "transfer.md", Header: "Transfers > Durable transfers"}
	h := &fakeHistory{priorTurns: []history.Turn{{Query: "What happens if it crashes?", Answer: "The notes do not specify crash recovery.", Subject: subject}}}
	searcher := &fakeSearcher{chunks: []search.Chunk{{Text: "Current evidence"}}}
	gen := &fakeGenerator{tokens: []string{"Current answer"}}
	d := NewDependencies(DependenciesConfig{History: h, Searcher: searcher, Generator: gen})
	turn := d.StartTurn(context.Background(), Request{Query: "Can it be retried?", SessionID: "s1", Generate: true})
	drain(t, d, turn)
	waitFor(t, func() bool { return len(h.turns()) == 1 })
	if got := h.turns()[0].Subject; got == nil || *got != *subject {
		t.Fatalf("decline lost the selected subject: %+v", h.turns()[0])
	}
	turn = d.StartTurn(context.Background(), Request{Query: "How does Redis choose its cluster slot?", SessionID: "s1", Generate: true})
	drain(t, d, turn)
	if strings.Contains(awaitPrompt(t, gen), "Durable transfers") || turn.Subject != nil || searcher.gotQuery != turn.Query {
		t.Fatalf("topic switch inherited the prior alternative: %+v prompt=%q", turn, awaitPrompt(t, gen))
	}
}

func TestSelectionDoesNotGuessFromAnOrderedComparison(t *testing.T) {
	turn := history.Turn{Query: "Compare Alpha first and Beta second.", Answer: "The alpha method requires gradients [1], while the beta method does not [2].", Citations: []history.Citation{
		{Number: 1, Header: "Methods > Alpha Method > Properties"},
		{Number: 2, Header: "Methods > Beta Method > Properties"},
	}}
	if got := selectedSubject(turn); got != nil {
		t.Fatalf("comparison falsely selected an alternative: %+v", got)
	}
	turn.Answer = "Do not use the alpha method [1]."
	if got := selectedSubject(turn); got != nil {
		t.Fatalf("negated recommendation selected an alternative: %+v", got)
	}
}

func TestChoiceMayExplainRejectedAlternativeWithoutLosingSelectedSubject(t *testing.T) {
	turn := history.Turn{Query: "Should I use immediate transfers or durable transfers?", Answer: "Durable transfers [1]. Immediate transfers can be lost on a crash [2].", Citations: []history.Citation{
		{Number: 1, FilePath: "transfers.md", Header: "Transfers > Durable transfers"},
		{Number: 2, FilePath: "transfers.md", Header: "Transfers > Immediate transfers"},
	}}
	selected := selectedSubject(turn)
	if selected == nil || selected.Header != "Transfers > Durable transfers" {
		t.Fatalf("explanation of rejected option erased selection: %+v", selected)
	}
	turn.Query = "Compare immediate transfers and durable transfers."
	if got := selectedSubject(turn); got != nil {
		t.Fatalf("comparison guessed a selection: %+v", got)
	}
}

func TestLowercaseNamedAlternativeOverridesPreviousSelection(t *testing.T) {
	query := "How does the immediate transfer mode handle failures?"
	h := &fakeHistory{priorTurns: []history.Turn{{Query: "Which transfer mode?", Answer: "Durable transfer mode [2].", Subject: &history.Subject{Header: "Transfers > Durable transfer mode"}, Citations: []history.Citation{
		{Number: 1, Header: "Transfers > Immediate transfer mode"},
		{Number: 2, Header: "Transfers > Durable transfer mode"},
	}}}}
	searcher := &fakeSearcher{}
	d := NewDependencies(DependenciesConfig{History: h, Searcher: searcher})
	turn := d.StartTurn(context.Background(), Request{Query: query, SessionID: "s1"})
	if searcher.gotQuery != query || turn.Subject != nil {
		t.Fatalf("named alternative inherited prior mode: query=%q subject=%+v", searcher.gotQuery, turn.Subject)
	}
}

func TestConditionalTriggerOnlyInCompetingAlternativeCannotBecomeAnAnswer(t *testing.T) {
	query := "What happens if it crashes before acknowledging?"
	h := &fakeHistory{priorTurns: []history.Turn{{Query: "Should I use immediate transfers or durable transfers?", Answer: "Durable transfers [2]. Immediate transfers can be lost if it crashes [1].", Citations: []history.Citation{
		{Number: 1, FilePath: "transfer.md", Header: "Transfers > Immediate transfers"},
		{Number: 2, FilePath: "transfer.md", Header: "Transfers > Durable transfers"},
	}}}}
	gen := &fakeGenerator{tokens: []string{"A crash loses the transfer [1]."}}
	searcher := &fakeSearcher{chunks: []search.Chunk{
		{FilePath: "transfer.md", SectionPath: "Transfers > Immediate transfers", Text: "Transfers can be lost if it crashes before processing."},
		{FilePath: "transfer.md", SectionPath: "Transfers > Durable transfers", Text: "Acknowledge after successful processing."},
	}}
	d := NewDependencies(DependenciesConfig{History: h, Generator: gen, Searcher: searcher})
	turn := d.StartTurn(context.Background(), Request{Query: query, SessionID: "s1", Generate: true})
	if !turn.HasAnswer || turn.Streaming || gen.received() != "" || !strings.Contains(turn.Answer, "Durable transfers") || !strings.Contains(turn.Answer, "do not establish") {
		t.Fatalf("competing condition leaked into an answer: %+v prompt=%q", turn, gen.received())
	}
	// Explicit coverage for the selected alternative must remain answerable.
	waitFor(t, func() bool { return len(h.turns()) == 1 })
	searcher.chunks[1].Text = "If it crashes, the transfer returns to the queue."
	turn = d.StartTurn(context.Background(), Request{Query: query, SessionID: "s1", Generate: true})
	if !turn.Streaming {
		t.Fatalf("guard rejected an explicitly documented selected outcome: %+v", turn)
	}
	drain(t, d, turn)
}
