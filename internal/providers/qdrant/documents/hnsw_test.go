package store

import (
	"context"
	"testing"

	"nadir/internal/core/retrieval/search"

	qdrant "github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc"
)

type hnswCollections struct {
	qdrant.CollectionsClient
	create *qdrant.CreateCollection
}

func (c *hnswCollections) Create(_ context.Context, in *qdrant.CreateCollection, _ ...grpc.CallOption) (*qdrant.CollectionOperationResponse, error) {
	c.create = in
	return &qdrant.CollectionOperationResponse{}, nil
}

type hnswPoints struct {
	qdrant.PointsClient
	query *qdrant.QueryPoints
	batch *qdrant.QueryBatchPoints
}

func (p *hnswPoints) CreateFieldIndex(context.Context, *qdrant.CreateFieldIndexCollection, ...grpc.CallOption) (*qdrant.PointsOperationResponse, error) {
	return &qdrant.PointsOperationResponse{}, nil
}

func (p *hnswPoints) Query(_ context.Context, in *qdrant.QueryPoints, _ ...grpc.CallOption) (*qdrant.QueryResponse, error) {
	p.query = in
	return &qdrant.QueryResponse{}, nil
}

func (p *hnswPoints) QueryBatch(_ context.Context, in *qdrant.QueryBatchPoints, _ ...grpc.CallOption) (*qdrant.QueryBatchResponse, error) {
	p.batch = in
	results := make([]*qdrant.BatchResult, len(in.GetQueryPoints()))
	for i := range results {
		results[i] = &qdrant.BatchResult{}
	}
	return &qdrant.QueryBatchResponse{Result: results}, nil
}

func TestCreateCollectionAppliesConfiguredHNSW(t *testing.T) {
	collections := &hnswCollections{}
	store := &dependencies{
		points:     &hnswPoints{},
		collection: collections,
		hnsw:       HNSWConfig{M: 32, EfConstruct: 200, FullScanThreshold: 500, IndexingThreshold: 100},
	}
	if err := store.createCollection(context.Background(), "docs", 3); err != nil {
		t.Fatal(err)
	}
	h := collections.create.GetHnswConfig()
	if h.GetM() != 32 || h.GetEfConstruct() != 200 || h.GetFullScanThreshold() != 500 {
		t.Fatalf("hnsw config = %+v", h)
	}
	if got := collections.create.GetOptimizersConfig().GetIndexingThreshold(); got != 100 {
		t.Fatalf("indexing threshold = %d, want 100", got)
	}
}

func TestCreateCollectionLeavesQdrantDefaultsWhenUnset(t *testing.T) {
	collections := &hnswCollections{}
	store := &dependencies{points: &hnswPoints{}, collection: collections}
	if err := store.createCollection(context.Background(), "docs", 3); err != nil {
		t.Fatal(err)
	}
	if collections.create.HnswConfig != nil || collections.create.OptimizersConfig != nil {
		t.Fatalf("unset HNSW config must stay nil: %+v", collections.create)
	}
}

func TestCreateCollectionSetsOnlyConfiguredHNSWFields(t *testing.T) {
	collections := &hnswCollections{}
	store := &dependencies{points: &hnswPoints{}, collection: collections, hnsw: HNSWConfig{M: 8}}
	if err := store.createCollection(context.Background(), "docs", 3); err != nil {
		t.Fatal(err)
	}
	h := collections.create.GetHnswConfig()
	if h.GetM() != 8 || h.EfConstruct != nil || h.FullScanThreshold != nil {
		t.Fatalf("only M should be set: %+v", h)
	}
}

func TestFusedSearchSetsHnswEfOnDensePrefetchOnly(t *testing.T) {
	points := &hnswPoints{}
	store := &dependencies{points: points, activeAlias: "docs__active", prefetchMul: 5, hnsw: HNSWConfig{Ef: 128}}
	if _, err := store.HybridSearch(context.Background(), []float32{1, 0, 0}, "secant derivative", 5, &search.Filter{}); err != nil {
		t.Fatal(err)
	}
	prefetch := points.query.GetPrefetch()
	if len(prefetch) != 2 {
		t.Fatalf("prefetch legs = %d, want 2", len(prefetch))
	}
	if got := prefetch[0].GetParams().GetHnswEf(); got != 128 {
		t.Fatalf("dense prefetch hnsw_ef = %d, want 128", got)
	}
	if prefetch[1].Params != nil {
		t.Fatalf("sparse leg must not carry hnsw params: %+v", prefetch[1].Params)
	}
	if points.query.Params != nil {
		t.Fatalf("top-level params are ignored with prefetch and must stay unset: %+v", points.query.Params)
	}
}

func TestAdaptiveSearchSetsHnswEfOnEveryDenseLeg(t *testing.T) {
	points := &hnswPoints{}
	store := &dependencies{
		points: points, activeAlias: "docs__active", prefetchMul: 5, adaptiveSignals: true,
		hnsw: HNSWConfig{Ef: 64},
	}
	if _, err := store.HybridSearch(context.Background(), []float32{1, 0, 0}, "secant derivative", 5, nil); err != nil {
		t.Fatal(err)
	}
	queries := points.batch.GetQueryPoints()
	if len(queries) != 3 {
		t.Fatalf("batch queries = %d, want 3", len(queries))
	}
	if got := queries[0].GetParams().GetHnswEf(); got != 64 {
		t.Fatalf("standalone dense hnsw_ef = %d, want 64", got)
	}
	if queries[1].Params != nil {
		t.Fatalf("standalone sparse query must not carry hnsw params: %+v", queries[1].Params)
	}
	if got := queries[2].GetPrefetch()[0].GetParams().GetHnswEf(); got != 64 {
		t.Fatalf("fusion dense prefetch hnsw_ef = %d, want 64", got)
	}
}

func TestSearchOmitsParamsWhenHnswEfUnset(t *testing.T) {
	points := &hnswPoints{}
	store := &dependencies{points: points, activeAlias: "docs__active", prefetchMul: 5}
	if _, err := store.HybridSearch(context.Background(), []float32{1, 0, 0}, "secant derivative", 5, nil); err != nil {
		t.Fatal(err)
	}
	for _, leg := range points.query.GetPrefetch() {
		if leg.Params != nil {
			t.Fatalf("unset hnsw_ef must leave params nil: %+v", leg.Params)
		}
	}
}
