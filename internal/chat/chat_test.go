package chat

import (
	"context"
	"errors"
	"go.uber.org/zap"
	"strings"
	"sync"
	"testing"
	"time"

	"nadir/internal/generator"
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
	tokens []string
	err    error
	got    string
}

func (f *fakeGenerator) Generate(ctx context.Context, prompt string) (<-chan generator.Event, error) {
	f.got = prompt
	if f.err != nil {
		return nil, f.err
	}
	ch := make(chan generator.Event, len(f.tokens)+1)
	for _, tk := range f.tokens {
		ch <- generator.TokenEvent{Text: tk}
	}
	ch <- generator.DoneEvent{}
	close(ch)
	return ch, nil
}

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

// waitFor polls cond — persistence and generation run on their own
// goroutines by design, so tests must wait for them instead of assuming
// they have run when StartTurn returns.
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

// drain consumes a turn's event stream until it closes, returning the
// concatenated token text and the terminal event kind.
func drain(t *testing.T, d *dependencies, turn Turn) (string, EventKind) {
	t.Helper()
	events, cancel, ok := d.Subscribe(context.Background(), turn.ID, 0)
	if !ok {
		t.Fatal("expected a live event stream for a streaming turn")
	}
	defer cancel()
	var text strings.Builder
	kind := EventDone
	for ev := range events {
		switch ev.Kind {
		case EventToken:
			text.WriteString(ev.Text)
		case EventError, EventDone:
			kind = ev.Kind
		}
	}
	return text.String(), kind
}

func TestStartTurnEmptyQuery(t *testing.T) {
	h := &fakeHistory{}
	d := NewDependencies(DependenciesConfig{Searcher: &fakeSearcher{}, History: h, Log: testLogger()})

	turn := d.StartTurn(context.Background(), Request{})

	if turn.Error == "" {
		t.Fatal("expected validation error for empty query")
	}
	if turn.SessionID != "" || h.mintCalled {
		t.Fatal("no session should be minted for an empty query")
	}
	// No session → nothing is persisted; the error lives only on the turn.
	if len(h.turns()) != 0 {
		t.Fatalf("unsessioned turn must not be persisted, got %+v", h.turns())
	}
}

func TestStartTurnMintsSessionOnFirstTurnOnly(t *testing.T) {
	h := &fakeHistory{}
	d := NewDependencies(DependenciesConfig{Searcher: &fakeSearcher{chunks: []store.ScoredChunk{{FilePath: "a.md"}}}, History: h, Log: testLogger()})

	turn := d.StartTurn(context.Background(), Request{Query: "q", TopK: 5})
	if turn.SessionID != "minted-1" || h.mintCount() != 1 {
		t.Fatalf("first turn must mint exactly one session, got %q (mints=%d)", turn.SessionID, h.mintCount())
	}
	waitFor(t, func() bool { return len(h.turns()) == 1 })

	turn = d.StartTurn(context.Background(), Request{Query: "q2", SessionID: "existing"})
	if turn.SessionID != "existing" || h.mintCount() != 1 {
		t.Fatalf("subsequent turns must reuse the given session id (mints=%d)", h.mintCount())
	}
}

func TestStartTurnWithoutHistory(t *testing.T) {
	d := NewDependencies(DependenciesConfig{Searcher: &fakeSearcher{chunks: []store.ScoredChunk{{}}}, Log: testLogger()})

	turn := d.StartTurn(context.Background(), Request{Query: "q"})

	if turn.SessionID != "" {
		t.Fatal("history disabled → no session id")
	}
	if turn.Error != "" || len(turn.Chunks) != 1 {
		t.Fatalf("unexpected turn: %+v", turn)
	}
}

func TestStartTurnSearchFailureStillReturnsSession(t *testing.T) {
	h := &fakeHistory{}
	d := NewDependencies(DependenciesConfig{
		Searcher: &fakeSearcher{err: errors.New("qdrant down")},
		History:  h, Log: testLogger(),
	})

	turn := d.StartTurn(context.Background(), Request{Query: "q"})

	if turn.SessionID != "minted-1" {
		t.Fatal("minted session id must survive a search failure so the conversation can continue")
	}
	if turn.Error == "" || turn.HasAnswer {
		t.Fatalf("expected search error, got %+v", turn)
	}
	var appended []history.Turn
	waitFor(t, func() bool { appended = h.turns(); return len(appended) == 1 })
	if !appended[0].Failed {
		t.Fatal("failed turn must be persisted with Failed=true")
	}
}

func TestStartTurnGenerationStreamsAndPersists(t *testing.T) {
	h := &fakeHistory{}
	d := NewDependencies(DependenciesConfig{
		Searcher:  &fakeSearcher{chunks: []store.ScoredChunk{{FilePath: "a.md"}}},
		Generator: &fakeGenerator{prompt: "P", tokens: []string{"answer ", "text"}},
		History:   h,
		Log:       testLogger(),
	})
	turn := d.StartTurn(context.Background(), Request{Query: "q", TopK: 3, Generate: true})

	if !turn.Streaming || turn.ID == "" || turn.Prompt == "" {
		t.Fatalf("generation must start as a subscribed stream: %+v", turn)
	}
	if turn.HasAnswer || turn.Answer != "" {
		t.Fatalf("answer must not exist at start time: %+v", turn)
	}

	text, kind := drain(t, d, turn)
	if text != "answer text" || kind != EventDone {
		t.Fatalf("stream must deliver tokens then done, got %q (%v)", text, kind)
	}
	waitFor(t, func() bool { return len(h.turns()) == 1 })
	appended := h.turns()[0]
	if !appended.HasAnswer || appended.Answer != "answer text" {
		t.Fatalf("supervisor must persist the completed answer, got %+v", appended)
	}
}

func TestStartTurnGenerateStartFailurePersistsError(t *testing.T) {
	h := &fakeHistory{}
	d := NewDependencies(DependenciesConfig{
		Searcher:  &fakeSearcher{chunks: []store.ScoredChunk{{}}},
		Generator: &fakeGenerator{err: errors.New("ollama down")},
		History:   h,
		Log:       testLogger(),
	})
	turn := d.StartTurn(context.Background(), Request{Query: "q", Generate: true})

	if turn.Error != "" {
		t.Fatal("search succeeded; Error must stay empty")
	}
	if turn.GenerateError == "" || turn.Streaming || turn.ID != "" {
		t.Fatalf("expected generate-start failure with no stream, got %+v", turn)
	}
	waitFor(t, func() bool { return len(h.turns()) == 1 })
	if h.turns()[0].GenerateError == "" || h.turns()[0].HasAnswer {
		t.Fatalf("failed start must persist a generate-stage error, got %+v", h.turns()[0])
	}
}

func TestStartTurnGenerateIgnoredWithoutGenerator(t *testing.T) {
	d := NewDependencies(DependenciesConfig{
		Searcher: &fakeSearcher{chunks: []store.ScoredChunk{{}}},
		Log:      testLogger(),
	})

	turn := d.StartTurn(context.Background(), Request{Query: "q", Generate: true})

	if turn.GenerateError != "" || turn.Streaming {
		t.Fatal("nil generator must silently skip generation")
	}
}

func TestPersistTurnCapturesShape(t *testing.T) {
	h := &fakeHistory{}
	d := NewDependencies(DependenciesConfig{
		Searcher:  &fakeSearcher{chunks: []store.ScoredChunk{{FilePath: "a.md", Header: "H", LineStart: 3, Score: 0.9, Text: "t", WindowText: "w", SourceSHA: "sha"}}},
		Generator: &fakeGenerator{prompt: "P", tokens: []string{"A"}},
		History:   h,
		Model:     "test-model",
		Log:       testLogger(),
	})

	req := Request{Query: "what?", TopK: 7, Generate: true, AttachedFiles: []string{"x.md"}}
	turn := d.StartTurn(context.Background(), req)
	drain(t, d, turn)
	waitFor(t, func() bool { return len(h.turns()) == 1 })

	appended := h.turns()[0]
	if appended.Query != "what?" || appended.TopK != 7 || appended.Model != "test-model" ||
		!appended.Generate || !appended.HasAnswer || appended.Answer != "A" ||
		len(appended.AttachedFiles) != 1 || appended.Count != 1 {
		t.Fatalf("turn shape wrong: %+v", appended)
	}
	if len(appended.Results) != 1 || appended.Results[0].Text != "w" || appended.Results[0].SourceSHA != "sha" {
		t.Fatalf("result snapshot wrong: %+v", appended.Results)
	}
}

func TestStartTurnRewritesFollowUpAgainstPriorTurns(t *testing.T) {
	h := &fakeHistory{priorTurns: []history.Turn{
		{Query: "what is the derivative of x^n?"},
		{Query: "and of sin(x)?", Answer: "cos(x)"},
	}}
	searcher := &fakeSearcher{chunks: []store.ScoredChunk{{FilePath: "a.md"}}}
	gen := &fakeGenerator{prompt: "P", tokens: []string{"A"}}
	rw := &fakeRewriter{rewritten: "what is the derivative of cos(x)?"}
	d := NewDependencies(DependenciesConfig{
		Searcher:  searcher,
		Generator: gen,
		History:   h,
		Rewriter:  rw,
		Log:       testLogger(),
	})

	turn := d.StartTurn(context.Background(), Request{Query: "what about the second one?", SessionID: "s1", Generate: true})

	if rw.called != 1 {
		t.Fatalf("rewriter must be called once for a follow-up with prior turns, called=%d", rw.called)
	}
	if len(rw.gotTurns) != 2 || rw.gotTurns[1].Answer != "cos(x)" {
		t.Fatalf("rewriter must receive prior turns with answers, got %+v", rw.gotTurns)
	}
	if searcher.gotQuery != "what is the derivative of cos(x)?" {
		t.Fatalf("retrieval must use the rewritten query, got %q", searcher.gotQuery)
	}
	if turn.RewrittenQuery != "what is the derivative of cos(x)?" {
		t.Fatalf("the rewrite must surface on the turn for the Think trace, got %q", turn.RewrittenQuery)
	}

	// Streaming contract: nothing persists until the supervisor finishes,
	// and the persisted turn carries the raw user query.
	waitFor(t, func() bool { return len(h.turns()) == 1 })
	if h.turns()[0].Query != "what about the second one?" {
		t.Fatalf("history must persist the raw user query, got %q", h.turns()[0].Query)
	}
	if !strings.Contains(gen.got, "what is the derivative of cos(x)?") {
		t.Fatalf("generation prompt must embed the rewritten query, got %q", gen.got)
	}
}

func TestStartTurnSkipsRewriteWithoutPriorTurns(t *testing.T) {
	h := &fakeHistory{}
	searcher := &fakeSearcher{chunks: []store.ScoredChunk{{}}}
	rw := &fakeRewriter{rewritten: "should not be used"}
	d := NewDependencies(DependenciesConfig{
		Searcher: searcher, History: h, Rewriter: rw, Log: testLogger(),
	})

	turn := d.StartTurn(context.Background(), Request{Query: "standalone question", SessionID: "s1"})

	if rw.called != 0 {
		t.Fatal("rewriter must be skipped when the session has no prior turns")
	}
	if searcher.gotQuery != "standalone question" || turn.Error != "" {
		t.Fatalf("raw query must be searched unchanged, got %q (turn %+v)", searcher.gotQuery, turn)
	}
}

func TestStartTurnSkipsRewriteOnFirstTurnWithoutSession(t *testing.T) {
	h := &fakeHistory{priorTurns: []history.Turn{{Query: "old"}}}
	searcher := &fakeSearcher{chunks: []store.ScoredChunk{{}}}
	rw := &fakeRewriter{rewritten: "rewritten"}
	d := NewDependencies(DependenciesConfig{
		Searcher: searcher, History: h, Rewriter: rw, Log: testLogger(),
	})

	d.StartTurn(context.Background(), Request{Query: "first question"})

	if rw.called != 0 {
		t.Fatal("a minted first turn has no prior turns; rewriter must be skipped")
	}
	if searcher.gotQuery != "first question" {
		t.Fatalf("raw query must be searched, got %q", searcher.gotQuery)
	}
}

func TestStartTurnRewriteFailureFallsBackToRawQuery(t *testing.T) {
	h := &fakeHistory{priorTurns: []history.Turn{{Query: "prior", Answer: "answer"}}}
	searcher := &fakeSearcher{chunks: []store.ScoredChunk{{}}}
	rw := &fakeRewriter{err: errors.New("ollama down")}
	d := NewDependencies(DependenciesConfig{
		Searcher: searcher, History: h, Rewriter: rw, Log: testLogger(),
	})

	turn := d.StartTurn(context.Background(), Request{Query: "follow-up?", SessionID: "s1"})

	if rw.called != 1 || searcher.gotQuery != "follow-up?" {
		t.Fatalf("rewrite failure must fall back to the raw query, called=%d searched=%q", rw.called, searcher.gotQuery)
	}
	if turn.Error != "" || len(turn.Chunks) != 1 || turn.RewrittenQuery != "" {
		t.Fatalf("fallback must not surface an error or a rewrite, got %+v", turn)
	}
}

func TestStartTurnRewriteSkippedWithoutRewriter(t *testing.T) {
	h := &fakeHistory{priorTurns: []history.Turn{{Query: "prior"}}}
	searcher := &fakeSearcher{chunks: []store.ScoredChunk{{}}}
	d := NewDependencies(DependenciesConfig{
		Searcher: searcher, History: h, Log: testLogger(),
	})

	d.StartTurn(context.Background(), Request{Query: "follow-up?", SessionID: "s1"})

	if searcher.gotQuery != "follow-up?" {
		t.Fatalf("nil rewriter must search the raw query, got %q", searcher.gotQuery)
	}
}

func TestStartTurnRewriteTurnsCapped(t *testing.T) {
	var prior []history.Turn
	for i := 0; i < 10; i++ {
		prior = append(prior, history.Turn{Query: "q"})
	}
	h := &fakeHistory{priorTurns: prior}
	rw := &fakeRewriter{rewritten: "rewritten"}
	d := NewDependencies(DependenciesConfig{
		Searcher: &fakeSearcher{chunks: []store.ScoredChunk{{}}}, History: h, Rewriter: rw, Log: testLogger(),
	})

	d.StartTurn(context.Background(), Request{Query: "follow-up?", SessionID: "s1"})

	if len(rw.gotTurns) != defaultRewriteTurns {
		t.Fatalf("rewriter must see at most %d turns, got %d", defaultRewriteTurns, len(rw.gotTurns))
	}
}

func TestSubscribeUnknownTurnID(t *testing.T) {
	d := NewDependencies(DependenciesConfig{Searcher: &fakeSearcher{}, Log: testLogger()})

	if _, _, ok := d.Subscribe(context.Background(), "nope", 0); ok {
		t.Fatal("unknown turn id must not subscribe")
	}
	if d.CancelTurn("nope") {
		t.Fatal("CancelTurn on unknown id must return false")
	}
}

// blockingGenerator emits one token, then holds the stream open until its
// context is cancelled — a stand-in for a long Ollama generation.
type blockingGenerator struct {
	started chan struct{}
}

func (b *blockingGenerator) Generate(ctx context.Context, prompt string) (<-chan generator.Event, error) {
	close(b.started)
	ch := make(chan generator.Event)
	go func() {
		ch <- generator.TokenEvent{Text: "partial "}
		<-ctx.Done()
		close(ch)
	}()
	return ch, nil
}

func TestCancelTurnPersistsPartialAnswer(t *testing.T) {
	h := &fakeHistory{}
	gen := &blockingGenerator{started: make(chan struct{})}
	d := NewDependencies(DependenciesConfig{
		Searcher:  &fakeSearcher{chunks: []store.ScoredChunk{{FilePath: "a.md"}}},
		Generator: gen,
		History:   h,
		Log:       testLogger(),
	})
	turn := d.StartTurn(context.Background(), Request{Query: "q", Generate: true})
	if !turn.Streaming {
		t.Fatalf("expected a streaming turn, got %+v", turn)
	}
	<-gen.started

	events, cancel, ok := d.Subscribe(context.Background(), turn.ID, 0)
	if !ok {
		t.Fatal("expected a live event stream")
	}
	defer cancel()
	first := <-events
	if first.Kind != EventToken || first.Text != "partial " {
		t.Fatalf("expected the first token before cancelling, got %+v", first)
	}

	if !d.CancelTurn(turn.ID) {
		t.Fatal("CancelTurn must find a live turn")
	}

	var text strings.Builder
	kind := EventDone
	for ev := range events {
		switch ev.Kind {
		case EventToken:
			text.WriteString(ev.Text)
		case EventError, EventDone:
			kind = ev.Kind
		}
	}
	if kind != EventDone || text.String() != "" {
		t.Fatalf("cancellation must end the stream as done with no further tokens, got %q (%v)", text.String(), kind)
	}
	waitFor(t, func() bool { return len(h.turns()) == 1 })
	appended := h.turns()[0]
	if !appended.HasAnswer || appended.Answer != "partial " {
		t.Fatalf("cancelled turn must persist the partial answer, got %+v", appended)
	}
}
