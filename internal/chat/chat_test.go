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
	"nadir/internal/rewriter"
	"nadir/internal/store"
)

type fakeSearcher struct {
	chunks    []store.ScoredChunk
	fromCache bool
	err       error
	gotTopK   int
	gotQuery  string
}

func (f *fakeSearcher) Query(ctx context.Context, query, keyword string, topK int, filter *store.SearchFilter, skipCache bool) ([]store.ScoredChunk, bool, error) {
	f.gotTopK = topK
	f.gotQuery = query
	return f.chunks, f.fromCache, f.err
}

type fakeRewriter struct {
	rewritten string
	err       error
	called    int
	gotTurns  []rewriter.Turn
	gotQuery  string
}

func (f *fakeRewriter) Rewrite(ctx context.Context, turns []rewriter.Turn, query string) (string, error) {
	f.called++
	f.gotTurns = turns
	f.gotQuery = query
	if f.err != nil {
		return "", f.err
	}
	return f.rewritten, nil
}

type fakeGenerator struct {
	prompt string
	body   string
	err    error
	gotQ   string
}

func (f *fakeGenerator) Generate(ctx context.Context, query string, chunks []store.ScoredChunk) (string, io.ReadCloser, error) {
	f.gotQ = query
	if f.err != nil {
		return "", nil, f.err
	}
	return f.prompt, io.NopCloser(strings.NewReader(f.body)), nil
}

func (f *fakeGenerator) gotQuery() string { return f.gotQ }

type fakeHistory struct {
	mu         sync.Mutex
	sessions   []history.Session
	appended   []history.Turn
	priorTurns []history.Turn
	createErr  error
	appendErr  error
	listErr    error
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

func (f *fakeHistory) ListTurns(ctx context.Context, sessionID string) ([]history.Turn, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]history.Turn(nil), f.priorTurns...), nil
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

func TestAskRewritesFollowUpAgainstPriorTurns(t *testing.T) {
	h := &fakeHistory{priorTurns: []history.Turn{
		{Query: "what is the derivative of x^n?"},
		{Query: "and of sin(x)?", Answer: "cos(x)"},
	}}
	searcher := &fakeSearcher{chunks: []store.ScoredChunk{{FilePath: "a.md"}}}
	gen := &fakeGenerator{prompt: "P", body: "A"}
	rw := &fakeRewriter{rewritten: "what is the derivative of cos(x)?"}
	d := NewDependencies(DependenciesConfig{
		Searcher:  searcher,
		Generator: gen,
		History:   h,
		Rewriter:  rw,
		Log:       testLogger(),
	})

	res := d.Ask(context.Background(), Request{Query: "what about the second one?", SessionID: "s1", Generate: true})

	if rw.called != 1 {
		t.Fatalf("rewriter must be called once for a follow-up with prior turns, called=%d", rw.called)
	}
	if len(rw.gotTurns) != 2 || rw.gotTurns[1].Answer != "cos(x)" {
		t.Fatalf("rewriter must receive prior turns with answers, got %+v", rw.gotTurns)
	}
	if searcher.gotQuery != "what is the derivative of cos(x)?" {
		t.Fatalf("retrieval must use the rewritten query, got %q", searcher.gotQuery)
	}
	if !res.HasAnswer || gen.gotQuery() != "what is the derivative of cos(x)?" {
		t.Fatalf("generation must use the rewritten query, got %q", gen.gotQuery())
	}

	var appended []history.Turn
	waitFor(t, func() bool { appended = h.turns(); return len(appended) == 1 })
	if appended[0].Query != "what about the second one?" {
		t.Fatalf("history must persist the raw user query, got %q", appended[0].Query)
	}
}

func TestAskSkipsRewriteWithoutPriorTurns(t *testing.T) {
	h := &fakeHistory{}
	searcher := &fakeSearcher{chunks: []store.ScoredChunk{{}}}
	rw := &fakeRewriter{rewritten: "should not be used"}
	d := NewDependencies(DependenciesConfig{
		Searcher: searcher, History: h, Rewriter: rw, Log: testLogger(),
	})

	res := d.Ask(context.Background(), Request{Query: "standalone question", SessionID: "s1"})

	if rw.called != 0 {
		t.Fatal("rewriter must be skipped when the session has no prior turns")
	}
	if searcher.gotQuery != "standalone question" || res.Error != "" {
		t.Fatalf("raw query must be searched unchanged, got %q (res %+v)", searcher.gotQuery, res)
	}
}

func TestAskSkipsRewriteOnFirstTurnWithoutSession(t *testing.T) {
	h := &fakeHistory{priorTurns: []history.Turn{{Query: "old"}}}
	searcher := &fakeSearcher{chunks: []store.ScoredChunk{{}}}
	rw := &fakeRewriter{rewritten: "rewritten"}
	d := NewDependencies(DependenciesConfig{
		Searcher: searcher, History: h, Rewriter: rw, Log: testLogger(),
	})

	d.Ask(context.Background(), Request{Query: "first question"})

	if rw.called != 0 {
		t.Fatal("a minted first turn has no prior turns; rewriter must be skipped")
	}
	if searcher.gotQuery != "first question" {
		t.Fatalf("raw query must be searched, got %q", searcher.gotQuery)
	}
}

func TestAskRewriteFailureFallsBackToRawQuery(t *testing.T) {
	h := &fakeHistory{priorTurns: []history.Turn{{Query: "prior", Answer: "answer"}}}
	searcher := &fakeSearcher{chunks: []store.ScoredChunk{{}}}
	rw := &fakeRewriter{err: errors.New("ollama down")}
	d := NewDependencies(DependenciesConfig{
		Searcher: searcher, History: h, Rewriter: rw, Log: testLogger(),
	})

	res := d.Ask(context.Background(), Request{Query: "follow-up?", SessionID: "s1"})

	if rw.called != 1 || searcher.gotQuery != "follow-up?" {
		t.Fatalf("rewrite failure must fall back to the raw query, called=%d searched=%q", rw.called, searcher.gotQuery)
	}
	if res.Error != "" || len(res.Chunks) != 1 {
		t.Fatalf("fallback must not surface an error, got %+v", res)
	}
}

func TestAskRewriteSkippedWithoutRewriter(t *testing.T) {
	h := &fakeHistory{priorTurns: []history.Turn{{Query: "prior"}}}
	searcher := &fakeSearcher{chunks: []store.ScoredChunk{{}}}
	d := NewDependencies(DependenciesConfig{
		Searcher: searcher, History: h, Log: testLogger(),
	})

	d.Ask(context.Background(), Request{Query: "follow-up?", SessionID: "s1"})

	if searcher.gotQuery != "follow-up?" {
		t.Fatalf("nil rewriter must search the raw query, got %q", searcher.gotQuery)
	}
}

func TestAskRewriteTurnsCapped(t *testing.T) {
	var prior []history.Turn
	for i := 0; i < 10; i++ {
		prior = append(prior, history.Turn{Query: "q"})
	}
	h := &fakeHistory{priorTurns: prior}
	rw := &fakeRewriter{rewritten: "rewritten"}
	d := NewDependencies(DependenciesConfig{
		Searcher: &fakeSearcher{chunks: []store.ScoredChunk{{}}}, History: h, Rewriter: rw, Log: testLogger(),
	})

	d.Ask(context.Background(), Request{Query: "follow-up?", SessionID: "s1"})

	if len(rw.gotTurns) != defaultRewriteTurns {
		t.Fatalf("rewriter must see at most %d turns, got %d", defaultRewriteTurns, len(rw.gotTurns))
	}
}
