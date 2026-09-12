package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	domainchat "nadir/internal/conversation/chat"
	domainhistory "nadir/internal/conversation/history"
	"nadir/internal/knowledge/indexing"
	"nadir/internal/retrieval/search"
)

type e2eIngest struct{ files []ingest.UploadFile }

func (f *e2eIngest) Run(_ context.Context, files []ingest.UploadFile) (ingest.Result, error) {
	f.files = append([]ingest.UploadFile(nil), files...)
	return ingest.Result{Processed: len(files)}, nil
}

type e2eStore struct{ resetCalls int }

func (f *e2eStore) DeleteAll(context.Context) error { f.resetCalls++; return nil }

type e2eHistory struct{}

func (e2eHistory) ListSessions(context.Context, int) ([]domainhistory.Session, error) {
	return []domainhistory.Session{{ID: "session-1", Title: "A chat"}}, nil
}
func (e2eHistory) GetSession(context.Context, string) (domainhistory.Session, error) {
	return domainhistory.Session{ID: "session-1", Title: "A chat"}, nil
}
func (e2eHistory) ListTurns(context.Context, string) ([]domainhistory.Turn, error) { return nil, nil }

type e2eChat struct {
	startCalls    []domainchat.Request
	cancelled     []string
	deleted       []string
	deleteAllCall int
}

func (f *e2eChat) StartTurn(_ context.Context, req domainchat.Request) domainchat.Turn {
	f.startCalls = append(f.startCalls, req)
	return domainchat.Turn{
		ID: "turn-1", SessionID: "session-1", Streaming: true,
		Chunks: []search.Chunk{{Text: "The secant formula.", FilePath: "math.md", LineStart: 4, SourceSHA: "sha-1", Score: 0.9}},
	}
}
func (f *e2eChat) Subscribe(context.Context, string, int64) (<-chan domainchat.TurnEvent, func(), bool) {
	ch := make(chan domainchat.TurnEvent, 2)
	ch <- domainchat.TurnEvent{Seq: 1, Kind: domainchat.EventToken, Text: "hello"}
	ch <- domainchat.TurnEvent{Seq: 2, Kind: domainchat.EventDone}
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
func (f *e2eChat) DeleteAllSessions(context.Context) error { f.deleteAllCall++; return nil }

type e2eServer struct {
	handler http.Handler
	URL     string
}

func startE2EServer(t *testing.T, deps *dependencies) *e2eServer {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	NewRouter(engine, deps)
	return &e2eServer{handler: engine, URL: "http://test"}
}

type handlerTransport struct{ handler http.Handler }

func (t handlerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	recorder := httptest.NewRecorder()
	t.handler.ServeHTTP(recorder, req)
	return recorder.Result(), nil
}

func TestHTTPWorkflow(t *testing.T) {
	ingestFake := &e2eIngest{}
	storeFake := &e2eStore{}
	chatFake := &e2eChat{}
	server := startE2EServer(t, NewDependencies(DependenciesConfig{
		Ingest: ingestFake, Store: storeFake, History: e2eHistory{}, Chat: chatFake, TopK: 5, MaxTopK: 10,
	}))
	client := &http.Client{Transport: handlerTransport{handler: server.handler}}

	resp, err := client.Get(server.URL + RouteHealth)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("health response = %v/%d", err, resp.StatusCode)
	}
	resp.Body.Close()

	var upload bytes.Buffer
	writer := multipart.NewWriter(&upload)
	part, err := writer.CreateFormFile("files", "notes.md")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("hello"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	resp, err = client.Post(server.URL+RouteDocuments, writer.FormDataContentType(), &upload)
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

	resp, err = client.Post(server.URL+RouteTurns, "application/json", strings.NewReader(`{"query":"what is the secant formula?","generate":true}`))
	if err != nil {
		t.Fatal(err)
	}
	var turnBody struct {
		SessionID string `json:"session_id"`
		StreamURL string `json:"stream_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&turnBody); err != nil {
		resp.Body.Close()
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || turnBody.SessionID != "session-1" || turnBody.StreamURL != "/api/v1/turns/turn-1/events" {
		t.Fatalf("turn response = %d/%+v", resp.StatusCode, turnBody)
	}

	resp, err = client.Post(server.URL+RouteTurns, "application/json", strings.NewReader(`{"query":"edited question","session_id":"session-1","edit":true,"edit_sequence":0}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || len(chatFake.startCalls) != 2 || !chatFake.startCalls[1].Edit || chatFake.startCalls[1].EditSequence != 0 {
		t.Fatalf("edit response = %d calls=%+v", resp.StatusCode, chatFake.startCalls)
	}

	resp, err = client.Get(server.URL + "/api/v1/turns/turn-1/events")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "event: token") || !strings.Contains(string(body), "event: done") {
		t.Fatalf("SSE response = %d/%s", resp.StatusCode, body)
	}

	resp, err = client.Get(server.URL + RouteSessions)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"title":"A chat"`) {
		t.Fatalf("sessions response = %d/%s", resp.StatusCode, body)
	}

	resp, err = client.Do(mustRequest(t, http.MethodDelete, server.URL+"/api/v1/sessions/session-1", nil))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || len(chatFake.deleted) != 1 {
		t.Fatalf("delete response = %d calls=%v", resp.StatusCode, chatFake.deleted)
	}

	resp, err = client.Do(mustRequest(t, http.MethodDelete, server.URL+RouteSessions, nil))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || chatFake.deleteAllCall != 1 {
		t.Fatalf("delete all response = %d calls=%d", resp.StatusCode, chatFake.deleteAllCall)
	}

	resp, err = client.Post(server.URL+RouteDocumentsReset, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || storeFake.resetCalls != 1 {
		t.Fatalf("reset response = %d calls=%d", resp.StatusCode, storeFake.resetCalls)
	}
}

func TestReadinessReturnsServiceUnavailableWithFailedCheck(t *testing.T) {
	server := startE2EServer(t, NewDependencies(DependenciesConfig{
		Readiness: func(context.Context) ReadinessReport {
			return ReadinessReport{
				Ready: false,
				Checks: map[string]ReadinessCheck{
					"embedding": {Model: "embed", Error: "model is unavailable"},
				},
			}
		},
	}))
	client := &http.Client{Transport: handlerTransport{handler: server.handler}}

	resp, err := client.Get(server.URL + RouteReady)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("readiness status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
	var report ReadinessReport
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		t.Fatal(err)
	}
	if report.Ready || report.Checks["embedding"].Error == "" {
		t.Fatalf("readiness report = %+v, want failed embedding diagnostic", report)
	}

	resp, err = client.Get(server.URL + RouteHealth)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("liveness status = %d, want %d while readiness is failing", resp.StatusCode, http.StatusOK)
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
