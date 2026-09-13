package server

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	domainchat "nadir/internal/conversation/chat"
	"nadir/internal/conversation/generation"
	domainhistory "nadir/internal/conversation/history"
	"nadir/internal/retrieval/search"
	api "nadir/internal/transport/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type lifecycleSearcher struct{}

func (lifecycleSearcher) Query(context.Context, search.Request) (search.Result, error) {
	return search.Result{Chunks: []search.Chunk{{FilePath: "lifecycle.md", Text: "shutdown test evidence"}}}, nil
}

type lifecycleGenerator struct {
	started chan struct{}
	once    sync.Once
}

func (g *lifecycleGenerator) Generate(ctx context.Context, _ string) (<-chan generation.Event, error) {
	g.once.Do(func() { close(g.started) })
	events := make(chan generation.Event)
	go func() {
		<-ctx.Done()
		close(events)
	}()
	return events, nil
}

type lifecycleHistory struct {
	mu        sync.Mutex
	started   chan struct{}
	release   chan struct{}
	startOnce sync.Once
	sessions  map[string]domainhistory.Session
	turns     []domainhistory.Turn
}

func newLifecycleHistory() *lifecycleHistory {
	return &lifecycleHistory{
		started:  make(chan struct{}),
		release:  make(chan struct{}),
		sessions: make(map[string]domainhistory.Session),
	}
}

func (h *lifecycleHistory) CreateSession(_ context.Context, title string) (domainhistory.Session, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	session := domainhistory.Session{ID: "lifecycle-session", Title: title}
	h.sessions[session.ID] = session
	return session, nil
}

func (h *lifecycleHistory) TruncateSession(context.Context, string, int) error { return nil }

func (h *lifecycleHistory) AppendTurn(ctx context.Context, sessionID string, turn domainhistory.Turn, _ string) error {
	h.startOnce.Do(func() { close(h.started) })
	select {
	case <-h.release:
		h.mu.Lock()
		h.turns = append(h.turns, turn)
		session := h.sessions[sessionID]
		session.TurnCount++
		h.sessions[sessionID] = session
		h.mu.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *lifecycleHistory) ListTurns(context.Context, string) ([]domainhistory.Turn, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]domainhistory.Turn(nil), h.turns...), nil
}

func (h *lifecycleHistory) DeleteSession(context.Context, string) error { return nil }
func (h *lifecycleHistory) DeleteAllSessions(context.Context) error     { return nil }

func (h *lifecycleHistory) persistedTurns() []domainhistory.Turn {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]domainhistory.Turn(nil), h.turns...)
}

func lifecycleRouter(chatService domainchat.Chat) http.Handler {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	deps := api.NewDependencies(api.DependenciesConfig{Chat: chatService, TopK: 1, MaxTopK: 5})
	return api.NewRouter(engine, deps)
}

func TestRunHTTPServerDrainsActiveChatBeforeReturning(t *testing.T) {
	history := newLifecycleHistory()
	generator := &lifecycleGenerator{started: make(chan struct{})}
	chatService := domainchat.NewDependencies(domainchat.DependenciesConfig{
		Searcher:       lifecycleSearcher{},
		Generator:      generator,
		History:        history,
		PersistTimeout: time.Second,
		Log:            zap.NewNop(),
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: lifecycleRouter(chatService)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- runHTTPServer(ctx, srv, time.Second, func() error {
			return srv.Serve(listener)
		}, chatService.Drain, zap.NewNop())
	}()

	request, err := http.NewRequest(http.MethodPost, "http://"+listener.Addr().String()+api.RouteTurns,
		strings.NewReader(`{"query":"will shutdown drain this?","generate":true}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		SessionID string `json:"session_id"`
		Streaming bool   `json:"streaming"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || body.SessionID == "" || !body.Streaming {
		t.Fatalf("start response = %d/%+v, want streaming turn", response.StatusCode, body)
	}
	select {
	case <-generator.started:
	case <-time.After(time.Second):
		t.Fatal("generation did not start")
	}

	cancel()
	select {
	case <-history.started:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not reach the external history write")
	}
	select {
	case err := <-done:
		t.Fatalf("server returned before history persistence completed: %v", err)
	case <-time.After(30 * time.Millisecond):
	}

	close(history.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runHTTPServer() = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not finish after history persistence was released")
	}
	if turns := history.persistedTurns(); len(turns) != 1 || !turns[0].HasAnswer {
		t.Fatalf("persisted turns = %+v, want one cancelled turn", turns)
	}
}

func TestRunHTTPServerCanRestartOnTheSameAddress(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	start := func(ctx context.Context, ln net.Listener) <-chan error {
		done := make(chan error, 1)
		srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })}
		go func() {
			done <- runHTTPServer(ctx, srv, time.Second, func() error { return srv.Serve(ln) }, nil, zap.NewNop())
		}()
		return done
	}

	ctx1, cancel1 := context.WithCancel(context.Background())
	done1 := start(ctx1, listener)
	if response, err := http.Get("http://" + addr); err != nil {
		t.Fatal(err)
	} else {
		response.Body.Close()
	}
	cancel1()
	if err := <-done1; err != nil {
		t.Fatal(err)
	}

	listener2, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("restart listener on %s: %v", addr, err)
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	done2 := start(ctx2, listener2)
	if response, err := http.Get("http://" + addr); err != nil {
		t.Fatal(err)
	} else {
		response.Body.Close()
	}
	cancel2()
	if err := <-done2; err != nil {
		t.Fatal(err)
	}
}

func TestRunHTTPServerReportsServeFailure(t *testing.T) {
	want := errors.New("listener failed")
	server := &http.Server{}
	err := runHTTPServer(context.Background(), server, time.Second, func() error { return want }, nil, zap.NewNop())
	if !errors.Is(err, want) {
		t.Fatalf("runHTTPServer() = %v, want wrapped serve error", err)
	}
}
