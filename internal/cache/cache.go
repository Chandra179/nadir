package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	qdrant "github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"nadir/internal/store"
)

func (c *dependencies) Close() error {
	return c.conn.Close()
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

func (c *dependencies) EnsureCollection(ctx context.Context) error {
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

func (c *dependencies) Get(ctx context.Context, query string) ([]store.ScoredChunk, bool, error) {
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
					return nil, false, nil
				}
			}
		}
	}

	rawJSON := pbStr(hit.Payload, "results_json")
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
	vec, err := c.embedder.Embed(ctx, query)
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
		"query":        storeStrVal(query),
		"results_json": storeStrVal(string(raw)),
		"cached_at":    storeStrVal(time.Now().UTC().Format(time.RFC3339)),
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

func pbStr(p map[string]*qdrant.Value, key string) string {
	if v, ok := p[key]; ok {
		if s, ok := v.Kind.(*qdrant.Value_StringValue); ok {
			return s.StringValue
		}
	}
	return ""
}

func storeStrVal(s string) *qdrant.Value {
	return &qdrant.Value{Kind: &qdrant.Value_StringValue{StringValue: s}}
}
