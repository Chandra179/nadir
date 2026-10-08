package history

import (
	"context"
	"testing"

	qdrant "github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc"

	qdrantutil "nadir/internal/providers/qdrant/shared"
)

// recordingPoints captures upserted vectors. Unimplemented methods panic on
// the nil embedded client, proving the history writes need nothing else.
type recordingPoints struct {
	qdrant.PointsClient
	vectors [][]float32
}

func (r *recordingPoints) Upsert(_ context.Context, in *qdrant.UpsertPoints, _ ...grpc.CallOption) (*qdrant.PointsOperationResponse, error) {
	for _, point := range in.Points {
		r.vectors = append(r.vectors, point.GetVectors().GetVector().GetDense().GetData())
	}
	return &qdrant.PointsOperationResponse{}, nil
}

func (r *recordingPoints) Get(context.Context, *qdrant.GetPoints, ...grpc.CallOption) (*qdrant.GetResponse, error) {
	return &qdrant.GetResponse{}, nil
}

func (r *recordingPoints) SetPayload(context.Context, *qdrant.SetPayloadPoints, ...grpc.CallOption) (*qdrant.PointsOperationResponse, error) {
	return &qdrant.PointsOperationResponse{}, nil
}

// noCollections satisfies the clients check; history writes never provision.
type noCollections struct{ qdrant.CollectionsClient }

func TestHistoryWritesUsePlaceholderVectorsWithoutAnEmbedder(t *testing.T) {
	points := &recordingPoints{}
	deps, err := NewDependencies(DependenciesConfig{
		Clients:    qdrantutil.Clients{Points: points, Collections: noCollections{}},
		Dimensions: testDimensions,
	})
	if err != nil {
		t.Fatalf("NewDependencies: %v", err)
	}
	ctx := context.Background()
	session, err := deps.CreateSession(ctx, "a long first question")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := deps.AppendTurn(ctx, session.ID, Turn{Query: "a long first question"}, "a long first question"); err != nil {
		t.Fatalf("AppendTurn: %v", err)
	}
	// CreateSession, then AppendTurn's missing-session create and the turn.
	if len(points.vectors) != 3 {
		t.Fatalf("upserted %d points, want 3", len(points.vectors))
	}
	for i, vec := range points.vectors {
		if len(vec) != testDimensions || vec[0] != 1 {
			t.Fatalf("point %d vector = %v, want a %d-d unit placeholder", i, vec, testDimensions)
		}
		for _, x := range vec[1:] {
			if x != 0 {
				t.Fatalf("point %d vector = %v, want a constant placeholder", i, vec)
			}
		}
	}
}

func TestNewDependenciesRequiresPositiveDimensions(t *testing.T) {
	_, err := NewDependencies(DependenciesConfig{Clients: qdrantutil.Clients{Points: &recordingPoints{}, Collections: noCollections{}}})
	if err == nil {
		t.Fatal("NewDependencies accepted a history collection with no vector size")
	}
}
