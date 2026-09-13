//go:build integration

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	qdranthistory "nadir/internal/adapters/qdrant/history"
	"nadir/internal/adapters/qdrant/shared"
	"nadir/internal/conversation/chat"
	domainhistory "nadir/internal/conversation/history"
	"nadir/internal/embedding"
	api "nadir/internal/transport/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	qdrant "github.com/qdrant/go-client/qdrant"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type integrationEmbedder struct{}

var _ embedding.Embedder = integrationEmbedder{}

func (integrationEmbedder) Embed(context.Context, string) ([]float32, error) {
	return []float32{1, 0, 0}, nil
}

func (integrationEmbedder) Dimensions() int { return 3 }

func TestQdrantHistorySurvivesHTTPRestart(t *testing.T) {
	addr := os.Getenv("QDRANT_ADDR")
	if addr == "" {
		t.Skip("set QDRANT_ADDR to run Qdrant lifecycle integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	clients := qdrantutil.NewClients(conn)
	collection := "nadir_lifecycle_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	defer func() {
		_, _ = clients.Collections.Delete(context.Background(), &qdrant.DeleteCollection{CollectionName: collection})
	}()

	history, err := qdranthistory.NewDependencies(qdranthistory.DependenciesConfig{
		Clients: clients, Collection: collection, Embedder: integrationEmbedder{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := history.EnsureCollection(ctx); err != nil {
		t.Fatal(err)
	}

	generator := &lifecycleGenerator{started: make(chan struct{})}
	chatService := chat.NewDependencies(chat.DependenciesConfig{
		Searcher: lifecycleSearcher{}, Generator: generator, History: history,
		PersistTimeout: 3 * time.Second, Log: zap.NewNop(),
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addrHTTP := listener.Addr().String()
	srv := &http.Server{Handler: integrationRouter(chatService, history)}
	processCtx, stopProcess := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runHTTPServer(processCtx, srv, 5*time.Second, func() error {
			return srv.Serve(listener)
		}, chatService.Drain, zap.NewNop())
	}()

	request, err := http.NewRequest(http.MethodPost, "http://"+addrHTTP+api.RouteTurns,
		strings.NewReader(`{"query":"what survives restart?","generate":true}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var startBody struct {
		SessionID string `json:"session_id"`
		Streaming bool   `json:"streaming"`
	}
	if err := json.NewDecoder(response.Body).Decode(&startBody); err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || startBody.SessionID == "" || !startBody.Streaming {
		t.Fatalf("start response = %d/%+v", response.StatusCode, startBody)
	}
	select {
	case <-generator.started:
	case <-time.After(time.Second):
		t.Fatal("generation did not start")
	}

	stopProcess()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("HTTP process did not drain")
	}

	var turns int
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		items, listErr := history.ListTurns(ctx, startBody.SessionID)
		if listErr == nil {
			turns = len(items)
			if turns == 1 {
				break
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	if turns != 1 {
		t.Fatalf("external history turns after shutdown = %d, want 1", turns)
	}

	// A new HTTP process can read the persisted session and turn. This is the
	// restart half of the contract; the event broker itself remains local.
	chatAfterRestart := chat.NewDependencies(chat.DependenciesConfig{
		Searcher: lifecycleSearcher{}, History: history, Log: zap.NewNop(),
	})
	listener2, err := net.Listen("tcp", addrHTTP)
	if err != nil {
		t.Fatalf("listen after restart: %v", err)
	}
	processCtx2, stopProcess2 := context.WithCancel(context.Background())
	srv2 := &http.Server{Handler: integrationRouter(chatAfterRestart, history)}
	done2 := make(chan error, 1)
	go func() {
		done2 <- runHTTPServer(processCtx2, srv2, 5*time.Second, func() error {
			return srv2.Serve(listener2)
		}, chatAfterRestart.Drain, zap.NewNop())
	}()

	response, err = http.Get(fmt.Sprintf("http://%s/api/v1/sessions/%s", addrHTTP, startBody.SessionID))
	if err != nil {
		t.Fatal(err)
	}
	var detail struct {
		Turns []struct {
			Query string `json:"query"`
		} `json:"turns"`
	}
	if err := json.NewDecoder(response.Body).Decode(&detail); err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || len(detail.Turns) != 1 || detail.Turns[0].Query != "what survives restart?" {
		t.Fatalf("restarted session response = %d/%+v", response.StatusCode, detail)
	}
	stopProcess2()
	if err := <-done2; err != nil {
		t.Fatal(err)
	}
}

func integrationRouter(chatService chat.Chat, history domainhistory.Reader) http.Handler {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	deps := api.NewDependencies(api.DependenciesConfig{
		Chat: chatService, History: history, TopK: 1, MaxTopK: 5,
	})
	return api.NewRouter(engine, deps)
}
