package chat

import (
	"context"
	"errors"
	"go.uber.org/zap"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"nadir/internal/history"
	"nadir/internal/store"
)

type fakeSearcher struct {
	chunks    []store.ScoredChunk
	fromCache bool
	err       error
	gotTopK   int
}

func (f *fakeSearcher) Query(ctx context.Context, query, keyword string, topK int, filter *store.SearchFilter, skipCache bool) ([]store.ScoredChunk, bool, error) {
	f.gotTopK = topK
	return f.chunks, f.fromCache, f.err
}

type fakeGenerator struct {
	prompt string
	body   string
	err    error
}

func (f *fakeGenerator) Generate(ctx context.Context, query string, chunks []store.ScoredChunk) (string, io.ReadCloser, error) {
	if f.err != nil {
		return "", nil, f.err
	}
	return f.prompt, io.NopCloser(strings.NewReader(f.body)), nil
}

type fakeHistory struct {
	mu         sync.Mutex
	sessions   []history.Session
	appended   []history.Turn
	createErr  error
	appendErr  error
	mintCalled bool
}

func (f *fakeHistory) CreateSession(ctx context.Context, title string) (history.Session, error) {
	if f.createErr != nil {
		return history.Session{}, f.createErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mintCalled = true
	s := history.Session{ID: "minted-1", Title: title}
	f.sessions = append(f.sessions, s)
	return s, nil
}

func (f *fakeHistory) AppendTurn(ctx context.Context, sessionID string, turn history.Turn, firstTurnTitle string) error {
	if f.appendErr != nil {
		return f.appendErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.appended = append(f.appended, turn)
	return nil
}

func (f *fakeHistory) mintCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.mintCalled {
		return len(f.sessions)
	}
	return 0
}

func (f *fakeHistory) turns() []history.Turn {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]history.Turn(nil), f.appended...)
}

func testLogger() *zap.Logger { return zap.NewNop() }

// waitFor polls cond — persist runs in a detached goroutine by design, so
// tests must wait for it instead of assuming it has run when Ask returns.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("condition not met within 2s")
}

func TestAskEmptyQuery(t *testing.T) {
	h := &fakeHistory{}
	d := NewDependencies(DependenciesConfig{Searcher: &fakeSearcher{}, History: h, Log: testLogger()})

	res := d.Ask(context.Background(), Request{})

	if res.Error == "" {
		t.Fatal("expected validation error for empty query")
	}
	if res.SessionID != "" || h.mintCalled {
		t.Fatal("no session should be minted for an empty query")
	}
}

func TestAskMintsSessionOnFirstTurnOnly(t *testing.T) {
	h := &fakeHistory{}
	d := NewDependencies(DependenciesConfig{Searcher: &fakeSearcher{chunks: []store.ScoredChunk{{FilePath: "a.md"}}}, History: h, Log: testLogger()})

	res := d.Ask(context.Background(), Request{Query: "q", TopK: 5})
	if res.SessionID != "minted-1" || h.mintCount() != 1 {
		t.Fatalf("first turn must mint exactly one session, got %q (mints=%d)", res.SessionID, h.mintCount())
	}
	waitFor(t, func() bool { return len(h.turns()) == 1 })

	res = d.Ask(context.Background(), Request{Query: "q2", SessionID: "existing"})
	if res.SessionID != "existing" || h.mintCount() != 1 {
		t.Fatalf("subsequent turns must reuse the given session id (mints=%d)", h.mintCount())
	}
}

func TestAskWithoutHistory(t *testing.T) {
	d := NewDependencies(DependenciesConfig{Searcher: &fakeSearcher{chunks: []store.ScoredChunk{{}}}, Log: testLogger()})

	res := d.Ask(context.Background(), Request{Query: "q"})

	if res.SessionID != "" {
		t.Fatal("history disabled → no session id")
	}
	if res.Error != "" || len(res.Chunks) != 1 {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestAskSearchFailureStillReturnsSession(t *testing.T) {
	h := &fakeHistory{}
	d := NewDependencies(DependenciesConfig{
		Searcher: &fakeSearcher{err: errors.New("qdrant down")},
		History:  h, Log: testLogger(),
	})

	res := d.Ask(context.Background(), Request{Query: "q"})

	if res.SessionID != "minted-1" {
		t.Fatal("minted session id must survive a search failure so the conversation can continue")
	}
	if res.Error == "" || res.HasAnswer {
		t.Fatalf("expected search error, got %+v", res)
	}
	var appended []history.Turn
	waitFor(t, func() bool { appended = h.turns(); return len(appended) == 1 })
	if !appended[0].Failed {
		t.Fatal("failed turn must be persisted with Failed=true")
	}
}

func TestAskGenerateSuccessAndFailure(t *testing.T) {
	ok := NewDependencies(DependenciesConfig{
		Searcher:  &fakeSearcher{chunks: []store.ScoredChunk{{FilePath: "a.md"}}},
		Generator: &fakeGenerator{prompt: "P", body: "answer text"},
		Log:       testLogger(),
	})
	res := ok.Ask(context.Background(), Request{Query: "q", TopK: 3, Generate: true})
	if !res.HasAnswer || res.Answer != "answer text" || res.Prompt != "P" {
		t.Fatalf("generate success path broken: %+v", res)
	}

	bad := NewDependencies(DependenciesConfig{
		Searcher:  &fakeSearcher{chunks: []store.ScoredChunk{{}}},
		Generator: &fakeGenerator{err: errors.New("ollama down")},
		Log:       testLogger(),
	})
	res = bad.Ask(context.Background(), Request{Query: "q", Generate: true})
	if res.Error != "" {
		t.Fatal("search succeeded; Error must stay empty")
	}
	if res.GenerateError == "" || res.HasAnswer {
		t.Fatalf("expected distinct generate-stage error, got %+v", res)
	}
}

func TestAskGenerateIgnoredWithoutGenerator(t *testing.T) {
	d := NewDependencies(DependenciesConfig{
		Searcher: &fakeSearcher{chunks: []store.ScoredChunk{{}}},
		Log:      testLogger(),
	})

	res := d.Ask(context.Background(), Request{Query: "q", Generate: true})

	if res.GenerateError != "" || res.HasAnswer {
		t.Fatal("nil generator must silently skip generation")
	}
}

func TestPersistCapturesTurnShape(t *testing.T) {
	h := &fakeHistory{}
	d := NewDependencies(DependenciesConfig{
		Searcher:  &fakeSearcher{chunks: []store.ScoredChunk{{FilePath: "a.md", Header: "H", LineStart: 3, Score: 0.9, Text: "t", WindowText: "w", SourceSHA: "sha"}}},
		Generator: &fakeGenerator{prompt: "P", body: "A"},
		History:   h,
		Model:     "test-model",
		Log:       testLogger(),
	})

	d.Ask(context.Background(), Request{Query: "what?", TopK: 7, Generate: true, AttachedFiles: []string{"x.md"}})

	var appended []history.Turn
	waitFor(t, func() bool { appended = h.turns(); return len(appended) == 1 })
	turn := appended[0]
	if turn.Query != "what?" || turn.TopK != 7 || turn.Model != "test-model" ||
		!turn.Generate || !turn.HasAnswer || turn.Answer != "A" ||
		len(turn.AttachedFiles) != 1 || turn.Count != 1 {
		t.Fatalf("turn shape wrong: %+v", turn)
	}
	if len(turn.Results) != 1 || turn.Results[0].Text != "w" || turn.Results[0].SourceSHA != "sha" {
		t.Fatalf("result snapshot wrong: %+v", turn.Results)
	}
}
