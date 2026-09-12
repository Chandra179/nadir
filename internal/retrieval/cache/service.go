package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"nadir/internal/adapters/qdrant/documents"
	"nadir/internal/adapters/qdrant/shared"

	"github.com/google/uuid"
	qdrant "github.com/qdrant/go-client/qdrant"
)

func (c *dependencies) Clear(ctx context.Context) error {
	// Invalidate logically before deleting points. A concurrent detached Set
	// may still finish after the delete, but its captured older generation will
	// never be accepted by Get.
	c.generation.Add(1)
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

func (c *dependencies) EnsureCollection(ctx context.Context) error {
	return qdrantutil.EnsureDenseCollection(ctx, qdrantutil.Clients{
		Points:      c.points,
		Collections: c.collection,
	}, c.name, c.dimensions, nil)
}

func (c *dependencies) Get(ctx context.Context, query string) ([]store.ScoredChunk, bool, error) {
	version := c.cacheVersion()
	vec, err := c.embedder.Embed(ctx, c.embedQuery(query))
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
	if c.cacheVersion() != version || qdrantutil.StringFromPayload(hit.Payload, "cache_version") != version {
		return nil, false, nil
	}
	if c.ttl > 0 {
		if ts := qdrantutil.StringFromPayload(hit.Payload, "cached_at"); ts != "" {
			t, err := time.Parse(time.RFC3339, ts)
			if err == nil && time.Since(t) > c.ttl {
				return nil, false, nil
			}
		}
	}

	rawJSON := qdrantutil.StringFromPayload(hit.Payload, "results_json")
	if rawJSON == "" {
		return nil, false, nil
	}

	var chunks []store.ScoredChunk
	if err := json.Unmarshal([]byte(rawJSON), &chunks); err != nil {
		return nil, false, fmt.Errorf("semantic cache decode: %w", err)
	}
	return chunks, true, nil
}

func (c *dependencies) Set(ctx context.Context, query string, chunks []store.ScoredChunk) error {
	version := c.cacheVersion()
	vec, err := c.embedder.Embed(ctx, c.embedQuery(query))
	if err != nil {
		return fmt.Errorf("semantic cache embed for set: %w", err)
	}

	raw, err := json.Marshal(chunks)
	if err != nil {
		return fmt.Errorf("semantic cache marshal: %w", err)
	}

	ns := uuid.MustParse("b1c2d3e4-f5a6-7b8c-9d00-1f2a3b4c5d6e")
	id := uuid.NewSHA1(ns, []byte(query)).String()

	payload := map[string]*qdrant.Value{
		"query":        qdrantutil.StringValue(query),
		"results_json": qdrantutil.StringValue(string(raw)),
		"cached_at":    qdrantutil.StringValue(time.Now().UTC().Format(time.RFC3339)),
	}
	payload["cache_version"] = qdrantutil.StringValue(version)

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

func (c *dependencies) embedQuery(query string) string {
	return c.queryPrefix + query
}

func (c *dependencies) cacheVersion() string {
	return effectiveVersion(c.version, c.generation.Load())
}

func effectiveVersion(base string, generation uint64) string {
	return fmt.Sprintf("%s:g%d", base, generation)
}
