package qdrantutil

import (
	"context"
	"fmt"

	qdrant "github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// FieldIndex describes a payload index required by a dense collection.
type FieldIndex struct {
	Name string
	Type qdrant.FieldType
}

// EnsureDenseCollection creates or validates a dense collection and creates
// its required payload indexes. Domain adapters retain ownership of their
// payload shape and any additional vector configuration.
func EnsureDenseCollection(ctx context.Context, clients Clients, name string, dimensions int, indexes []FieldIndex) error {
	info, err := clients.Collections.Get(ctx, &qdrant.GetCollectionInfoRequest{CollectionName: name})
	if err == nil {
		return ValidateDenseCollection(name, info.GetResult(), dimensions)
	}
	if status.Code(err) != codes.NotFound {
		return fmt.Errorf("qdrant get collection: %w", err)
	}

	if _, err := clients.Collections.Create(ctx, &qdrant.CreateCollection{
		CollectionName: name,
		VectorsConfig: &qdrant.VectorsConfig{
			Config: &qdrant.VectorsConfig_Params{
				Params: &qdrant.VectorParams{
					Size:     uint64(dimensions),
					Distance: qdrant.Distance_Cosine,
				},
			},
		},
	}); err != nil {
		return fmt.Errorf("qdrant create collection: %w", err)
	}

	for _, index := range indexes {
		fieldType := index.Type
		if _, err := clients.Points.CreateFieldIndex(ctx, &qdrant.CreateFieldIndexCollection{
			CollectionName: name,
			FieldName:      index.Name,
			FieldType:      &fieldType,
		}); err != nil {
			return fmt.Errorf("qdrant create %s index: %w", index.Name, err)
		}
	}
	return nil
}
