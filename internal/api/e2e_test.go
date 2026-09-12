package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	domainchat "nadir/internal/chat"
	domainhistory "nadir/internal/history"
	"nadir/internal/ingest"
	"nadir/internal/search"
	"nadir/internal/store"
)

type e2eIngest struct {
	files []ingest.UploadFile
}

func (f *e2eIngest) Run(_ context.Context, files []ingest.UploadFile) (ingest.Result, error) {
	f.files = append([]ingest.UploadFile(nil), files...)
	return ingest.Result{Processed: len(files)}, nil
}

type e2eStore struct {
	resetCalls int
}

func (f *e2eStore) ReplaceDocument(context.Context, string, string, []store.ScoredChunk) error {
	return nil
}
func (f *e2eStore) DeleteAll(context.Context) error {
	f.resetCalls++
	return nil
}
func (f *e2eStore) HybridSearch(context.Context, []float32, string, int, *store.SearchFilter) ([]store.ScoredChunk, error) {
	return nil, nil
}
func (f *e2eStore) KeywordSearch(context.Context, string, int, *store.SearchFilter) ([]store.ScoredChunk, error) {
	return nil, nil
}
func (f *e2eStore) GetAllFileSHAs(context.Context) (map[string]string, error) {
	return map[string]string{}, nil
}
func (f *e2eStore) Stats(context.Context) (store.Stats, error) { return store.Stats{}, nil }

type e2eHistory struct{}

func (e2eHistory) CreateSession(context.Context, string) (domainhistory.Session, error) {
	return domainhistory.Session{ID: "session-1"}, nil
}
func (e2eHistory) TruncateSession(context.Context, string, int) error { return nil }
func (e2eHistory) AppendTurn(context.Context, string, domainhistory.Turn, string) error {
	return nil
}
func (e2eHistory) ListSessions(context.Context, int) ([]domainhistory.Session, error) {
	return []domainhistory.Session{{ID: "session-1", Title: "A chat"}}, nil
}
func (e2eHistory) GetSession(context.Context, string) (domainhistory.Session, error) {
	return domainhistory.Session{ID: "session-1"}, nil
}
func (e2eHistory) ListTurns(context.Context, string) ([]domainhistory.Turn, error) {
	return nil, nil
}
func (e2eHistory) DeleteSession(context.Context, string) error { return nil }
func (e2eHistory) DeleteAllSessions(context.Context) error     { return nil }

type e2eChat struct {
	startCalls    []domainchat.Request
	cancelled     []string
	deleted       []string
	deleteAllCall int
}

func (f *e2eChat) StartTurn(_ context.Context, req domainchat.Request) domainchat.Turn {
	f.startCalls = append(f.startCalls, req)
	return domainchat.Turn{
		ID:        "turn-1",
		SessionID: "session-1",
		Streaming: true,
		Chunks: []search.Chunk{{
			Text:      "The secant formula.",
			FilePath:  "math.md",
			LineStart: 4,
			SourceSHA: "sha-1",
			Score:     0.9,
		}},
	}
}

func (f *e2eChat) Subscribe(context.Context, string, int64) (<-chan domainchat.TurnEvent, func(), bool) {
	ch := make(chan domainchat.TurnEvent, 2)
	ch <- domainchat.TurnEvent{Seq: 1, Kind: domainchat.EventToken, Text: "hello"}
	ch <- domainchat.TurnEvent{Seq: 2, Kind: domainchat.EventDone, Text: ""}
	close(ch)
	return ch, func() {}, true
}
func (f *e2eChat) CancelTurn(turnID string) bool {
	f.cancelled = append(f.cancelled, turnID)
	return turnID == "turn-1"
}
func (f *e2eChat) DeleteSession(_ context.Context, sessionID string) error {
	f.deleted = append(f.deleted, sessionID)
	return nil
}
func (f *e2eChat) DeleteAllSessions(context.Context) error {
	f.deleteAllCall++
	return nil
}

type e2eServer struct {
	*http.Server
	URL string
}

func startE2EServer(t *testing.T, deps *dependencies) *e2eServer {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	NewRouter(engine, deps)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: engine}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	return &e2eServer{Server: server, URL: "http://" + listener.Addr().String()}
}

func TestHTTPWorkflow(t *testing.T) {
	ingestFake := &e2eIngest{}
	storeFake := &e2eStore{}
	chatFake := &e2eChat{}
	d := NewDependencies(DependenciesConfig{
		Ingest:  ingestFake,
		Store:   storeFake,
		History: e2eHistory{},
		Chat:    chatFake,
		TopK:    5,
		MaxTopK: 10,
	})
	server := startE2EServer(t, d)
	client := &http.Client{}

	resp, err := client.Get(server.URL + RouteHealth)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	var upload bytes.Buffer
	writer := multipart.NewWriter(&upload)
	part, err := writer.CreateFormFile("files", "notes.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	resp, err = client.Post(server.URL+RouteIngest, writer.FormDataContentType(), &upload)
	if err != nil {
		t.Fatal(err)
	}
	var ingestBody struct {
		Processed int `json:"processed"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ingestBody); err != nil {
		resp.Body.Close()
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || ingestBody.Processed != 1 || len(ingestFake.files) != 1 {
		t.Fatalf("ingest response = %d/%+v files=%+v", resp.StatusCode, ingestBody, ingestFake.files)
	}

	form := url.Values{"query": {"what is the secant formula?"}, "generate": {"on"}}
	resp, err = client.PostForm(server.URL+RouteRetrievalSearch, form)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "/retrieval/turns/turn-1/events") {
		t.Fatalf("search response = %d/%s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("X-Nadir-Session-Id"); got != "session-1" {
		t.Fatalf("session header = %q, want session-1", got)
	}

	editForm := url.Values{
		"query":         {"edited question"},
		"session_id":    {"session-1"},
		"edit":          {"on"},
		"edit_sequence": {"0"},
	}
	resp, err = client.PostForm(server.URL+RouteRetrievalSearch, editForm)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || len(chatFake.startCalls) != 2 || !chatFake.startCalls[1].Edit || chatFake.startCalls[1].EditSequence != 0 {
		t.Fatalf("edit response = %d start_calls=%+v", resp.StatusCode, chatFake.startCalls)
	}

	resp, err = client.Get(server.URL + "/retrieval/turns/turn-1/events")
	if err != nil {
		t.Fatal(err)
	}
	body, err = io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "event: token") || !strings.Contains(string(body), "event: done") {
		t.Fatalf("SSE response = %d/%s", resp.StatusCode, body)
	}

	resp, err = client.Post(server.URL+"/retrieval/turns/turn-1/cancel", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent || len(chatFake.cancelled) != 1 {
		t.Fatalf("cancel response = %d calls=%v", resp.StatusCode, chatFake.cancelled)
	}

	resp, err = client.Get(server.URL + RouteHistorySessions)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "A chat") {
		t.Fatalf("history response = %d/%s", resp.StatusCode, body)
	}

	resp, err = client.Do(mustRequest(t, http.MethodDelete, server.URL+"/history/sessions/session-1", nil))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || len(chatFake.deleted) != 1 {
		t.Fatalf("delete session response = %d calls=%v", resp.StatusCode, chatFake.deleted)
	}

	resp, err = client.Do(mustRequest(t, http.MethodDelete, server.URL+RouteHistorySessions, nil))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || chatFake.deleteAllCall != 1 {
		t.Fatalf("delete all response = %d calls=%d", resp.StatusCode, chatFake.deleteAllCall)
	}

	resp, err = client.Post(server.URL+RouteStoreReset, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || storeFake.resetCalls != 1 {
		t.Fatalf("reset response = %d calls=%d", resp.StatusCode, storeFake.resetCalls)
	}
}

func mustRequest(t *testing.T, method, target string, body io.Reader) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, target, body)
	if err != nil {
		t.Fatal(err)
	}
	return req
}
