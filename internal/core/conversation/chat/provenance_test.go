package chat

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"nadir/internal/core/retrieval/search"
)

func TestGenerationAndHistoryShareAdmittedCitationSnapshots(t *testing.T) {
	chunks := []search.Chunk{
		{FilePath: "first.md", LineStart: 3, ChunkIndex: 6, SourceSHA: "version-1", Text: "First evidence"},
		{FilePath: "second.md", LineStart: 9, ChunkIndex: 2, SourceSHA: "version-2", Text: "Second evidence", WindowText: "Expanded second evidence with neighbors."},
		{FilePath: "third.md", LineStart: 12, ChunkIndex: 7, SourceSHA: "version-3", Text: "Third evidence"},
	}
	store := &fakeHistory{}
	generator := &fakeGenerator{tokens: []string{"Second evidence [2]."}}
	d := NewDependencies(DependenciesConfig{Searcher: &fakeSearcher{chunks: chunks}, Generator: generator, History: store,
		MaxContextTokens: 500, ContextWindowTokens: 4096, ReservedOutputTokens: 512})
	turn := d.StartTurn(context.Background(), Request{Query: "which evidence?", Generate: true, TopK: 3})
	if len(turn.Citations) != 3 || awaitPrompt(t, generator) != turn.Prompt {
		t.Fatalf("generation lost admitted evidence: %+v", turn)
	}
	if strings.Index(turn.Prompt, "[2] (source:") > strings.Index(turn.Prompt, "[3] (source:") {
		t.Fatalf("expected retrieval order without renumbering: %q", turn.Prompt)
	}
	second := turn.Citations[1]
	if second.Number != 2 || second.RetrievalRank != 2 || second.FilePath != "second.md" || second.ChunkIndex != 2 || second.SourceSHA != "version-2" || second.Text != chunks[1].WindowText {
		t.Fatalf("citation 2 drifted from its evidence: %+v", second)
	}
	answer, terminal := drain(t, d, turn)
	if answer != "Second evidence [2]." || terminal != EventDone {
		t.Fatalf("answer=%q terminal=%v", answer, terminal)
	}
	waitFor(t, func() bool { return len(store.turns()) == 1 })
	persisted := store.turns()[0]
	if !reflect.DeepEqual(persisted.Citations, citationResults(turn.Citations)) || persisted.Prompt != awaitPrompt(t, generator) {
		t.Fatalf("history differs from generation input: %+v", persisted)
	}
	if persisted.Results[1].ChunkIndex != 2 || persisted.Results[1].LineStart != 9 {
		t.Fatalf("history lost chunk identity: %+v", persisted.Results[1])
	}
}

func TestModelBudgetRejectsGenerationAndPersistsFailure(t *testing.T) {
	store := &fakeHistory{}
	generator := &fakeGenerator{}
	d := NewDependencies(DependenciesConfig{
		Searcher:  &fakeSearcher{chunks: []search.Chunk{{FilePath: "evidence.md", Text: "Some evidence"}}},
		Generator: generator, History: store, ContextWindowTokens: 700, ReservedOutputTokens: 512,
	})
	turn := d.StartTurn(context.Background(), Request{Query: strings.Repeat("question ", 200), Generate: true})
	if turn.GenerateError == "" || turn.Streaming || turn.ID != "" || generator.received() != "" || len(turn.Citations) != 0 {
		t.Fatalf("oversized request reached generation: %+v prompt=%q", turn, generator.received())
	}
	waitFor(t, func() bool { return len(store.turns()) == 1 })
	if persisted := store.turns()[0]; persisted.GenerateError != turn.GenerateError || persisted.Prompt != "" {
		t.Fatalf("history lost bounded prompt failure: %+v", persisted)
	}
}

func TestPartialEvidenceSnapshotMatchesGeneratedPrompt(t *testing.T) {
	generator := &fakeGenerator{tokens: []string{"Answer [1]."}}
	d := NewDependencies(DependenciesConfig{
		Searcher: &fakeSearcher{chunks: []search.Chunk{
			{FilePath: "first.md", LineStart: 11, ChunkIndex: 4, Text: strings.Repeat("rank-one ", 100)},
			{FilePath: "second.md", Text: "unadmitted evidence"},
		}}, Generator: generator, MaxContextTokens: 80,
	})
	turn := d.StartTurn(context.Background(), Request{Query: "what?", Generate: true})
	if len(turn.Citations) != 1 || !turn.Citations[0].Truncated {
		t.Fatalf("unexpected admitted evidence: %+v", turn.Citations)
	}
	citation := turn.Citations[0]
	if !strings.Contains(awaitPrompt(t, generator), citationEntry(citation)) || strings.Contains(awaitPrompt(t, generator), "unadmitted evidence") || strings.Contains(awaitPrompt(t, generator), strings.Repeat("rank-one ", 100)) {
		t.Fatalf("citation snapshot differs from actual prompt: %q citation=%+v", awaitPrompt(t, generator), citation)
	}
	drain(t, d, turn)
}
