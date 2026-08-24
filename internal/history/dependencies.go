package history

import (
	"context"
	"fmt"

	qdrant "github.com/qdrant/go-client/qdrant"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"nadir/internal/embedder"
)

const (
	defaultCollection = "chat_history"
	defaultListLimit  = 50
	titleMaxLen       = 60

	docTypeSession = "session"
	docTypeTurn    = "turn"
)

type DependenciesConfig struct {
	Conn       *grpc.ClientConn
	Collection string
	Embedder   embedder.Embedder
	Log        *zap.Logger
}

type dependencies struct {
	points     qdrant.PointsClient
	collection qdrant.CollectionsClient
	name       string
	embedder   embedder.Embedder
	dimensions int
	log        *zap.Logger
}

var _ History = (*dependencies)(nil)

func NewDependencies(cfg DependenciesConfig) (*dependencies, error) {
	collection := cfg.Collection
	if collection == "" {
		collection = defaultCollection
	}
	return &dependencies{
		points:     qdrant.NewPointsClient(cfg.Conn),
		collection: qdrant.NewCollectionsClient(cfg.Conn),
		name:       collection,
		embedder:   cfg.Embedder,
		dimensions: cfg.Embedder.Dimensions(),
		log:        cfg.Log,
	}, nil
}

// EnsureCollection creates the chat-history collection and its payload
// field indexes if missing, mirroring store.EnsureCollection /
// cache.EnsureCollection.
func (d *dependencies) EnsureCollection(ctx context.Context) error {
	_, err := d.collection.Get(ctx, &qdrant.GetCollectionInfoRequest{CollectionName: d.name})
	if err == nil {
		return nil
	}
	if status.Code(err) != codes.NotFound {
		return fmt.Errorf("history: get collection: %w", err)
	}

	_, err = d.collection.Create(ctx, &qdrant.CreateCollection{
		CollectionName: d.name,
		VectorsConfig: &qdrant.VectorsConfig{
			Config: &qdrant.VectorsConfig_Params{
				Params: &qdrant.VectorParams{
					Size:     uint64(d.dimensions),
					Distance: qdrant.Distance_Cosine,
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("history: create collection: %w", err)
	}

	kw := qdrant.FieldType_FieldTypeKeyword
	for _, field := range []string{"doc_type", "session_id"} {
		if _, err := d.points.CreateFieldIndex(ctx, &qdrant.CreateFieldIndexCollection{
			CollectionName: d.name,
			FieldName:      field,
			FieldType:      &kw,
		}); err != nil {
			return fmt.Errorf("history: create %s index: %w", field, err)
		}
	}
	integer := qdrant.FieldType_FieldTypeInteger
	for _, field := range []string{"updated_at", "sequence"} {
		if _, err := d.points.CreateFieldIndex(ctx, &qdrant.CreateFieldIndexCollection{
			CollectionName: d.name,
			FieldName:      field,
			FieldType:      &integer,
		}); err != nil {
			return fmt.Errorf("history: create %s index: %w", field, err)
		}
	}
	return nil
}
