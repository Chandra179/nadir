package history

import (
	"context"
	"fmt"
	"sync"

	qdrant "github.com/qdrant/go-client/qdrant"

	"nadir/internal/adapters/qdrant/shared"
	"nadir/internal/embedding"
)

const (
	defaultCollection = "chat_history"
	defaultListLimit  = 50
	titleMaxLen       = 60

	docTypeSession = "session"
	docTypeTurn    = "turn"
)

// DependenciesConfig groups the shared Qdrant clients and history embedder.
type DependenciesConfig struct {
	Clients    qdrantutil.Clients
	Collection string
	Embedder   embedding.Embedder
}

type dependencies struct {
	points     qdrant.PointsClient
	collection qdrant.CollectionsClient
	name       string
	embedder   embedding.Embedder
	dimensions int
	writeMu    sync.Mutex
}

// NewDependencies constructs a chat-history persistence Adapter over shared
// Qdrant clients.
func NewDependencies(cfg DependenciesConfig) (*dependencies, error) {
	collection := cfg.Collection
	if collection == "" {
		collection = defaultCollection
	}
	if cfg.Clients.Points == nil || cfg.Clients.Collections == nil {
		return nil, fmt.Errorf("qdrant clients are required")
	}
	if cfg.Embedder == nil {
		return nil, fmt.Errorf("history embedder is required")
	}
	return &dependencies{
		points:     cfg.Clients.Points,
		collection: cfg.Clients.Collections,
		name:       collection,
		embedder:   cfg.Embedder,
		dimensions: cfg.Embedder.Dimensions(),
	}, nil
}

// EnsureCollection creates or validates the chat-history collection through
// the shared Qdrant infrastructure while keeping history's index schema local.
func (d *dependencies) EnsureCollection(ctx context.Context) error {
	return qdrantutil.EnsureDenseCollection(ctx, qdrantutil.Clients{
		Points:      d.points,
		Collections: d.collection,
	}, d.name, d.dimensions, []qdrantutil.FieldIndex{
		{Name: "doc_type", Type: qdrant.FieldType_FieldTypeKeyword},
		{Name: "session_id", Type: qdrant.FieldType_FieldTypeKeyword},
		{Name: "updated_at", Type: qdrant.FieldType_FieldTypeInteger},
		{Name: "sequence", Type: qdrant.FieldType_FieldTypeInteger},
	})
}

func (d *dependencies) lockWrites() func() {
	// Qdrant has no compare-and-swap update for the session turn counter in
	// this flow. Serialize history writes so sequence numbers and turn_count
	// remain consistent. A single mutex is intentionally bounded and safe;
	// history writes are low-volume compared with retrieval.
	d.writeMu.Lock()
	return d.writeMu.Unlock
}
