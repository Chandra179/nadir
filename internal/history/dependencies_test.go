package history

import (
	"context"
	"testing"
	"time"

	qdrant "github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const testDimensions = 8

// fakeEmbedder avoids requiring a running Ollama instance for this test —
// only Qdrant reachability is exercised. It derives a deterministic vector
// from the text's length so different inputs don't collide.
type fakeEmbedder struct{}

func (fakeEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	vec := make([]float32, testDimensions)
	for i := range vec {
		vec[i] = float32((len(text) + i) % 7)
	}
	return vec, nil
}

func (fakeEmbedder) Dimensions() int { return testDimensions }

func testDependencies(t *testing.T) *dependencies {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping qdrant integration test in -short mode")
	}

	conn, err := grpc.NewClient("localhost:6334", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Skipf("qdrant dial unavailable: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := qdrant.NewQdrantClient(conn).HealthCheck(ctx, &qdrant.HealthCheckRequest{}); err != nil {
		t.Skipf("qdrant unreachable: %v", err)
	}

	deps, err := NewDependencies(DependenciesConfig{
		Conn:       conn,
		Collection: "chat_history_test",
		Embedder:   fakeEmbedder{},
	})
	if err != nil {
		t.Fatalf("NewDependencies: %v", err)
	}
	if err := deps.EnsureCollection(context.Background()); err != nil {
		t.Fatalf("EnsureCollection: %v", err)
	}
	return deps
}

func TestSessionAndTurnRoundTrip(t *testing.T) {
	deps := testDependencies(t)
	ctx := context.Background()

	session, err := deps.CreateSession(ctx, "What's the secant formula?")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if session.ID == "" {
		t.Fatal("CreateSession: expected a non-empty ID")
	}

	turn := Turn{
		Query:     "What's the secant formula?",
		TopK:      5,
		Generate:  true,
		Results:   []TurnResult{{FilePath: "numerical-methods.md", LineStart: 10, Score: 0.9, Text: "x_{n+1} = ..."}},
		Count:     1,
		Answer:    "The secant formula is...",
		HasAnswer: true,
	}
	if err := deps.AppendTurn(ctx, session.ID, turn, session.Title); err != nil {
		t.Fatalf("AppendTurn: %v", err)
	}

	got, err := deps.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.TurnCount != 1 {
		t.Errorf("TurnCount = %d, want 1", got.TurnCount)
	}

	turns, err := deps.ListTurns(ctx, session.ID)
	if err != nil {
		t.Fatalf("ListTurns: %v", err)
	}
	if len(turns) != 1 || turns[0].Answer != turn.Answer {
		t.Errorf("ListTurns = %+v, want one turn with answer %q", turns, turn.Answer)
	}
}
