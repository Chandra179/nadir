package history

import (
	"context"
	"fmt"
	"sync"

	qdrant "github.com/qdrant/go-client/qdrant"

	"nadir/internal/providers/qdrant/shared"
)

const (
	defaultCollection   = "chat_history"
	defaultListLimit    = 50
	defaultTurnPageSize = 500
	titleMaxLen         = 60

	docTypeSession = "session"
	docTypeTurn    = "turn"
)

// DependenciesConfig groups the shared Qdrant clients and the history
// collection's vector size.
type DependenciesConfig struct {
	Clients    qdrantutil.Clients
	Collection string
	// Dimensions is the dense vector size of the history collection. It must
	// match the collection, which shares the document embedding dimensions.
	Dimensions   int
	TurnPageSize int
}

type dependencies struct {
	points       qdrant.PointsClient
	collection   qdrant.CollectionsClient
	name         string
	placeholder  []float32
	dimensions   int
	turnPageSize uint32
	writeMu      sync.Mutex
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
	if cfg.Dimensions <= 0 {
		return nil, fmt.Errorf("history vector dimensions must be positive")
	}
	turnPageSize := cfg.TurnPageSize
	if turnPageSize <= 0 {
		turnPageSize = defaultTurnPageSize
	}
	return &dependencies{
		points:       cfg.Clients.Points,
		collection:   cfg.Clients.Collections,
		name:         collection,
		placeholder:  placeholderVector(cfg.Dimensions),
		dimensions:   cfg.Dimensions,
		turnPageSize: uint32(turnPageSize),
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

// placeholderVector returns the constant vector stored with every history
// point. Qdrant requires a vector for a dense collection, but history is only
// listed, scrolled and filtered by payload; nothing searches it by similarity,
// so embedding titles and questions would spend model time for no reader. A
// unit vector keeps the point valid under the collection's cosine distance and
// leaves existing collections and their older embedded points compatible.
func placeholderVector(dimensions int) []float32 {
	vec := make([]float32, dimensions)
	vec[0] = 1
	return vec
}
