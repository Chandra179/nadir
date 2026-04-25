package pkb

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	qdrant "github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// SemanticCache caches search results keyed by query embedding similarity.
// A query hitting the cache at score >= threshold returns the cached result directly,
// skipping embedder + store + reranker round-trips.
type SemanticCache struct {
	conn       *grpc.ClientConn
	points     qdrant.PointsClient
	collection qdrant.CollectionsClient
	name       string
	embedder   Embedder
	threshold  float32
	ttl        time.Duration // zero = no expiry
	dimensions int
}

// NewSemanticCache connects to Qdrant at addr and creates a cache using a dedicated collection.
// threshold: cosine similarity cutoff (0.85–0.95 typical).
// ttl: how long entries live; zero disables expiry.
func NewSemanticCache(addr, collection string, embedder Embedder, threshold float32, ttl time.Duration) (*SemanticCache, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("semantic cache dial %s: %w", addr, err)
	}
	return &SemanticCache{
		conn:       conn,
		points:     qdrant.NewPointsClient(conn),
		collection: qdrant.NewCollectionsClient(conn),
		name:       collection,
		embedder:   embedder,
		threshold:  threshold,
		ttl:        ttl,
		dimensions: embedder.Dimensions(),
	}, nil
}

// Close releases the underlying gRPC connection.
func (c *SemanticCache) Close() error {
	return c.conn.Close()
}

// EnsureCollection creates the cache collection if it does not exist.
func (c *SemanticCache) EnsureCollection(ctx context.Context) error {
	_, err := c.collection.Get(ctx, &qdrant.GetCollectionInfoRequest{CollectionName: c.name})
	if err == nil {
		return nil
	}
	if status.Code(err) != codes.NotFound {
		return fmt.Errorf("semantic cache get collection: %w", err)
	}
	_, err = c.collection.Create(ctx, &qdrant.CreateCollection{
		CollectionName: c.name,
		VectorsConfig: &qdrant.VectorsConfig{
			Config: &qdrant.VectorsConfig_Params{
				Params: &qdrant.VectorParams{
					Size:     uint64(c.dimensions),
					Distance: qdrant.Distance_Cosine,
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("semantic cache create collection: %w", err)
	}
	return nil
}

// Get embeds query and looks for a cache hit above the similarity threshold.
// Returns (results, true, nil) on hit; (nil, false, nil) on miss; (nil, false, err) on error.
func (c *SemanticCache) Get(ctx context.Context, query string) ([]ScoredChunk, bool, error) {
	vec, err := c.embedder.Embed(ctx, query)
	if err != nil {
		return nil, false, fmt.Errorf("semantic cache embed: %w", err)
	}

	limit := uint64(1)
	resp, err := c.points.Search(ctx, &qdrant.SearchPoints{
		CollectionName: c.name,
		Vector:         vec,
		Limit:          limit,
		WithPayload:    qdrant.NewWithPayload(true),
		ScoreThreshold: &c.threshold,
	})
	if err != nil {
		return nil, false, fmt.Errorf("semantic cache search: %w", err)
	}
	if len(resp.Result) == 0 {
		return nil, false, nil
	}

	hit := resp.Result[0]
	if c.ttl > 0 {
		if tsRaw, ok := hit.Payload["cached_at"]; ok {
			if ts, ok := tsRaw.Kind.(*qdrant.Value_StringValue); ok {
				t, err := time.Parse(time.RFC3339, ts.StringValue)
				if err == nil && time.Since(t) > c.ttl {
					return nil, false, nil // expired
				}
			}
		}
	}

	rawJSON := pbStr(hit.Payload, "results_json")
	if rawJSON == "" {
		return nil, false, nil
	}

	var chunks []ScoredChunk
	if err := json.Unmarshal([]byte(rawJSON), &chunks); err != nil {
		return nil, false, fmt.Errorf("semantic cache decode: %w", err)
	}
	return chunks, true, nil
}

// Set stores query+results in the cache. Embedding is computed internally.
func (c *SemanticCache) Set(ctx context.Context, query string, chunks []ScoredChunk) error {
	vec, err := c.embedder.Embed(ctx, query)
	if err != nil {
		return fmt.Errorf("semantic cache embed for set: %w", err)
	}

	raw, err := json.Marshal(chunks)
	if err != nil {
		return fmt.Errorf("semantic cache marshal: %w", err)
	}

	ns := uuid.MustParse("b1c2d3e4-f5a6-7b8c-9d0e-1f2a3b4c5d6e")
	id := uuid.NewSHA1(ns, []byte(query)).String()

	payload := map[string]*qdrant.Value{
		"query":        strVal(query),
		"results_json": strVal(string(raw)),
		"cached_at":    strVal(time.Now().UTC().Format(time.RFC3339)),
	}

	_, err = c.points.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: c.name,
		Points: []*qdrant.PointStruct{
			{
				Id:      qdrant.NewIDUUID(id),
				Vectors: qdrant.NewVectors(vec...),
				Payload: payload,
			},
		},
	})
	return err
}
