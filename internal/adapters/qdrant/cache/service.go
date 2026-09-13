package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	qdrant "github.com/qdrant/go-client/qdrant"

	"nadir/internal/adapters/qdrant/shared"
	semanticcache "nadir/internal/retrieval/cache"
)

// EnsureCollection creates or validates the cache's dense Qdrant collection.
// It is intentionally not part of the private cache backend seam: provisioning is a
// startup concern, not a runtime cache operation.
func (c *dependencies) EnsureCollection(ctx context.Context, dimensions int) error {
	return qdrantutil.EnsureDenseCollection(ctx, qdrantutil.Clients{
		Points:      c.points,
		Collections: c.collection,
	}, c.name, dimensions, nil)
}

func (c *dependencies) Clear(ctx context.Context) error {
	_, err := c.points.Delete(ctx, &qdrant.DeletePoints{
		CollectionName: c.name,
		Points: &qdrant.PointsSelector{
			PointsSelectorOneOf: &qdrant.PointsSelector_Filter{
				Filter: &qdrant.Filter{},
			},
		},
	})
	return err
}

func (c *dependencies) Find(ctx context.Context, vector []float32, threshold float32) (semanticcache.Entry, bool, error) {
	limit := uint64(1)
	resp, err := c.points.Search(ctx, &qdrant.SearchPoints{
		CollectionName: c.name,
		Vector:         vector,
		Limit:          limit,
		WithPayload:    qdrant.NewWithPayload(true),
		ScoreThreshold: &threshold,
	})
	if err != nil {
		return semanticcache.Entry{}, false, fmt.Errorf("semantic cache search: %w", err)
	}
	if len(resp.Result) == 0 {
		return semanticcache.Entry{}, false, nil
	}

	payload := resp.Result[0].Payload
	rawJSON := qdrantutil.StringFromPayload(payload, "results_json")
	if rawJSON == "" {
		return semanticcache.Entry{}, false, nil
	}
	var results []semanticcache.Candidate
	if err := json.Unmarshal([]byte(rawJSON), &results); err != nil {
		return semanticcache.Entry{}, false, fmt.Errorf("semantic cache decode: %w", err)
	}

	entry := semanticcache.Entry{
		Version: qdrantutil.StringFromPayload(payload, "cache_version"),
		Results: results,
	}
	if cachedAt := qdrantutil.StringFromPayload(payload, "cached_at"); cachedAt != "" {
		entry.CachedAt, _ = time.Parse(time.RFC3339, cachedAt)
	}
	return entry, true, nil
}

func (c *dependencies) Put(ctx context.Context, query string, vector []float32, entry semanticcache.Entry) error {
	raw, err := json.Marshal(entry.Results)
	if err != nil {
		return fmt.Errorf("semantic cache marshal: %w", err)
	}

	ns := uuid.MustParse("b1c2d3e4-f5a6-7b8c-9d00-1f2a3b4c5d6e")
	id := uuid.NewSHA1(ns, []byte(query)).String()
	cachedAt := entry.CachedAt
	if cachedAt.IsZero() {
		cachedAt = time.Now().UTC()
	}
	payload := map[string]*qdrant.Value{
		"query":         qdrantutil.StringValue(query),
		"results_json":  qdrantutil.StringValue(string(raw)),
		"cached_at":     qdrantutil.StringValue(cachedAt.UTC().Format(time.RFC3339)),
		"cache_version": qdrantutil.StringValue(entry.Version),
	}

	_, err = c.points.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: c.name,
		Points: []*qdrant.PointStruct{{
			Id:      qdrant.NewIDUUID(id),
			Vectors: qdrant.NewVectors(vector...),
			Payload: payload,
		}},
	})
	return err
}
