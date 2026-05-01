package pkb

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"nadir/config"

	qdrantcontainer "github.com/testcontainers/testcontainers-go/modules/qdrant"
)

// sharedContainerState holds the one Qdrant container shared across all container-mode profiles.
// Ingest runs exactly once regardless of profile count.
var sharedContainer struct {
	sync.Once
	store        Store
	docsIngested int
	err          error
}

// getSharedContainerStore returns a pre-ingested QdrantStore for container-mode evals.
// The container and ingest run exactly once; subsequent calls return the same store.
// Ingest uses config.yaml defaults (chunk_size/overlap). Profiles with different chunk
// sizes should use EVAL_STORE=live with a pre-populated collection instead.
func getSharedContainerStore(embedder Embedder, cfg *config.Config) (Store, int, error) {
	sharedContainer.Do(func() {
		ctx := context.Background()

		container, err := qdrantcontainer.Run(ctx, "qdrant/qdrant:latest")
		if err != nil {
			sharedContainer.err = err
			return
		}

		// Register cleanup via os.Exit path — TestMain handles this.
		containerCleanup = func() { _ = container.Terminate(ctx) }

		grpcEndpoint, err := container.GRPCEndpoint(ctx)
		if err != nil {
			sharedContainer.err = err
			return
		}
		store, err := NewQdrantStore(grpcEndpoint, "eval", 0)
		if err != nil {
			sharedContainer.err = err
			return
		}
		if err := store.EnsureCollection(ctx, embedder.Dimensions()); err != nil {
			sharedContainer.err = err
			return
		}

		// Ingest gitbook docs once.
		chunker := NewRecursiveChunker(cfg.Chunker.ChunkSize, cfg.Chunker.ChunkOverlap)
		pipeline := NewPipeline(chunker, embedder, store, cfg.Retry)
		gitbookRoot := filepath.Join("..", "..", "gitbook")
		var count int
		_ = filepath.Walk(gitbookRoot, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(info.Name(), ".md") {
				return nil
			}
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}
			rel, _ := filepath.Rel(gitbookRoot, path)
			if ingestErr := pipeline.Ingest(ctx, rel, string(content), "eval"); ingestErr == nil {
				count++
			}
			return nil
		})

		sharedContainer.store = store
		sharedContainer.docsIngested = count
	})
	return sharedContainer.store, sharedContainer.docsIngested, sharedContainer.err
}

// containerCleanup is called by TestMain after tests complete.
var containerCleanup func()

func TestMain(m *testing.M) {
	code := m.Run()
	if containerCleanup != nil {
		containerCleanup()
	}
	os.Exit(code)
}
