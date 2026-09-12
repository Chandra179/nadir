//go:build integration

package store

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"nadir/internal/adapters/qdrant/shared"

	"github.com/google/uuid"
	qdrant "github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestQdrantStoreIntegration(t *testing.T) {
	addr := os.Getenv("QDRANT_ADDR")
	if addr == "" {
		t.Skip("set QDRANT_ADDR to run Qdrant integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	clients := qdrantutil.NewClients(conn)
	name := "nadir_integration_" + uuid.NewString()
	defer cleanupIntegrationCollections(name, clients)

	s, err := NewDependencies(DependenciesConfig{Clients: clients, Collection: name, PrefetchMul: 2})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureCollection(ctx, 3); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceDocument(ctx, "integration.md", "integration-sha", []ScoredChunk{{
		Text:       "integration test document",
		FilePath:   "integration.md",
		LineStart:  1,
		ChunkIndex: 0,
		Vector:     []float32{1, 0, 0},
		SourceSHA:  "integration-sha",
	}}); err != nil {
		t.Fatal(err)
	}
	results, err := s.HybridSearch(ctx, []float32{1, 0, 0}, "integration document", 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].FilePath != "integration.md" {
		t.Fatalf("results = %+v, want the inserted integration document", results)
	}

	if err := s.ReplaceDocument(ctx, "integration.md", "integration-sha-v2", []ScoredChunk{{
		Text:       "replacement document",
		FilePath:   "integration.md",
		LineStart:  1,
		ChunkIndex: 0,
		Vector:     []float32{1, 0, 0},
		SourceSHA:  "integration-sha-v2",
	}}); err != nil {
		t.Fatal(err)
	}
	results, err = s.HybridSearch(ctx, []float32{1, 0, 0}, "replacement document", 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].SourceSHA != "integration-sha-v2" {
		t.Fatalf("results after replacement = %+v, want only the active new version", results)
	}

	if err := s.DeleteAll(ctx); err != nil {
		t.Fatalf("reset document collection: %v", err)
	}
	results, err = s.HybridSearch(ctx, []float32{1, 0, 0}, "replacement document", 10, nil)
	if err != nil {
		t.Fatalf("search after reset: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("results after reset = %+v, want empty collection", results)
	}
}

func cleanupIntegrationCollections(base string, clients qdrantutil.Clients) {
	ctx := context.Background()
	if aliases, err := clients.Collections.ListAliases(ctx, &qdrant.ListAliasesRequest{}); err == nil {
		for _, alias := range aliases.GetAliases() {
			if alias.GetAliasName() == activeAliasName(base) {
				_, _ = clients.Collections.UpdateAliases(ctx, &qdrant.ChangeAliases{Actions: []*qdrant.AliasOperations{{
					Action: &qdrant.AliasOperations_DeleteAlias{DeleteAlias: &qdrant.DeleteAlias{AliasName: alias.GetAliasName()}},
				}}})
			}
		}
	}
	if collections, err := clients.Collections.List(ctx, &qdrant.ListCollectionsRequest{}); err == nil {
		for _, collection := range collections.GetCollections() {
			name := collection.GetName()
			if name == base || strings.HasPrefix(name, base+generationSeparator) {
				_, _ = clients.Collections.Delete(ctx, &qdrant.DeleteCollection{CollectionName: name})
			}
		}
	}
}
