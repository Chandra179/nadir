package cache

import (
	"context"
	"testing"
	"time"

	qdrant "github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc"

	semanticcache "nadir/internal/core/retrieval/cache"
	qdrantutil "nadir/internal/providers/qdrant/shared"
)

func TestNewDependenciesRequiresSharedClients(t *testing.T) {
	if _, err := NewDependencies(DependenciesConfig{}); err == nil {
		t.Fatal("NewDependencies accepted an empty Qdrant client set")
	}
}

// roundTripPoints stores the last upserted point and serves it back from
// Search, standing in for Qdrant. Other methods panic on the nil embedded
// client, proving the cache adapter needs nothing else.
type roundTripPoints struct {
	qdrant.PointsClient
	stored *qdrant.PointStruct
}

func (r *roundTripPoints) Upsert(_ context.Context, in *qdrant.UpsertPoints, _ ...grpc.CallOption) (*qdrant.PointsOperationResponse, error) {
	r.stored = in.Points[0]
	return &qdrant.PointsOperationResponse{}, nil
}

func (r *roundTripPoints) Search(context.Context, *qdrant.SearchPoints, ...grpc.CallOption) (*qdrant.SearchResponse, error) {
	return &qdrant.SearchResponse{Result: []*qdrant.ScoredPoint{{Payload: r.stored.Payload}}}, nil
}

type noCollections struct{ qdrant.CollectionsClient }

func TestEntryRoundTripPreservesRequestedTopK(t *testing.T) {
	points := &roundTripPoints{}
	adapter, err := NewDependencies(DependenciesConfig{
		Clients: qdrantutil.Clients{Points: points, Collections: noCollections{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	entry := semanticcache.Entry{
		Version: "v:g1", CachedAt: time.Unix(1700000000, 0).UTC(), RequestedTopK: 5,
		Results: []semanticcache.Candidate{{Text: "one", FilePath: "a.md"}},
	}
	if err := adapter.Put(ctx, "query", []float32{1, 0}, entry); err != nil {
		t.Fatal(err)
	}
	got, hit, err := adapter.Find(ctx, []float32{1, 0}, 0.9)
	if err != nil || !hit {
		t.Fatalf("Find() hit=%v err=%v", hit, err)
	}
	if got.RequestedTopK != 5 || got.Version != "v:g1" || len(got.Results) != 1 || got.Results[0].Text != "one" {
		t.Fatalf("entry lost fields in the round trip: %+v", got)
	}

	// Records written before the field existed carry no request size.
	delete(points.stored.Payload, "requested_top_k")
	legacy, hit, err := adapter.Find(ctx, []float32{1, 0}, 0.9)
	if err != nil || !hit || legacy.RequestedTopK != 0 {
		t.Fatalf("legacy record = %+v hit=%v err=%v, want RequestedTopK 0", legacy, hit, err)
	}
}
