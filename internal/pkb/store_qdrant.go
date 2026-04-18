package pkb

import (
	"context"
	"fmt"

	qdrant "github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// QdrantStore implements Store backed by Qdrant via gRPC.
type QdrantStore struct {
	points     qdrant.PointsClient
	collection qdrant.CollectionsClient
	name       string
}

func NewQdrantStore(addr, collection string) (*QdrantStore, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("qdrant dial %s: %w", addr, err)
	}
	return &QdrantStore{
		points:     qdrant.NewPointsClient(conn),
		collection: qdrant.NewCollectionsClient(conn),
		name:       collection,
	}, nil
}

func (s *QdrantStore) EnsureCollection(ctx context.Context, dimensions int) error {
	_, err := s.collection.Get(ctx, &qdrant.GetCollectionInfoRequest{CollectionName: s.name})
	if err != nil {
		_, err = s.collection.Create(ctx, &qdrant.CreateCollection{
			CollectionName: s.name,
			VectorsConfig: &qdrant.VectorsConfig{
				Config: &qdrant.VectorsConfig_Params{
					Params: &qdrant.VectorParams{
						Size:     uint64(dimensions),
						Distance: qdrant.Distance_Cosine,
					},
				},
			},
		})
		if err != nil {
			return err
		}
	}
	// Ensure full-text index on text field for BM25 hybrid search.
	ft := qdrant.FieldType_FieldTypeText
	_, err = s.points.CreateFieldIndex(ctx, &qdrant.CreateFieldIndexCollection{
		CollectionName: s.name,
		FieldName:      "text",
		FieldType:      &ft,
		FieldIndexParams: qdrant.NewPayloadIndexParamsText(&qdrant.TextIndexParams{
			Tokenizer: qdrant.TokenizerType_Word,
			Lowercase: qdrant.PtrOf(true),
		}),
	})
	return err
}

func (s *QdrantStore) Upsert(ctx context.Context, chunks []ScoredChunk) error {
	points := make([]*qdrant.PointStruct, len(chunks))
	for i, c := range chunks {
		id := chunkID(c.FilePath, c.LineStart)
		points[i] = &qdrant.PointStruct{
			Id:      qdrant.NewIDNum(id),
			Vectors: qdrant.NewVectors(c.Vector...),
			Payload: map[string]*qdrant.Value{
				"file_path":  strVal(c.FilePath),
				"header":     strVal(c.Header),
				"line_start": intVal(int64(c.LineStart)),
				"text":       strVal(c.Text),
				"source_sha": strVal(c.SourceSHA),
			},
		}
	}
	_, err := s.points.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: s.name,
		Points:         points,
	})
	return err
}

func (s *QdrantStore) DeleteByFile(ctx context.Context, filePath string) error {
	_, err := s.points.Delete(ctx, &qdrant.DeletePoints{
		CollectionName: s.name,
		Points: &qdrant.PointsSelector{
			PointsSelectorOneOf: &qdrant.PointsSelector_Filter{
				Filter: &qdrant.Filter{
					Must: []*qdrant.Condition{
						{
							ConditionOneOf: &qdrant.Condition_Field{
								Field: &qdrant.FieldCondition{
									Key: "file_path",
									Match: &qdrant.Match{
										MatchValue: &qdrant.Match_Keyword{Keyword: filePath},
									},
								},
							},
						},
					},
				},
			},
		},
	})
	return err
}

func (s *QdrantStore) HybridSearch(ctx context.Context, vector []float32, query string, topK int) ([]ScoredChunk, error) {
	prefetchLimit := uint64(topK * 3)
	resp, err := s.points.Query(ctx, &qdrant.QueryPoints{
		CollectionName: s.name,
		Prefetch: []*qdrant.PrefetchQuery{
			{
				Query: qdrant.NewQueryNearest(qdrant.NewVectorInput(vector...)),
				Limit: &prefetchLimit,
			},
			{
				Filter: &qdrant.Filter{
					Must: []*qdrant.Condition{
						qdrant.NewMatchText("text", query),
					},
				},
				Limit: &prefetchLimit,
			},
		},
		Query:       qdrant.NewQueryFusion(qdrant.Fusion_RRF),
		WithPayload: qdrant.NewWithPayload(true),
		Limit:       qdrant.PtrOf(uint64(topK)),
	})
	if err != nil {
		return nil, err
	}
	results := make([]ScoredChunk, len(resp.Result))
	for i, r := range resp.Result {
		p := r.Payload
		results[i] = ScoredChunk{
			DocumentChunk: DocumentChunk{
				Text:      pbStr(p, "text"),
				FilePath:  pbStr(p, "file_path"),
				Header:    pbStr(p, "header"),
				LineStart: int(pbInt(p, "line_start")),
			},
			SourceSHA: pbStr(p, "source_sha"),
			Score:     r.Score,
		}
	}
	return results, nil
}

func (s *QdrantStore) Search(ctx context.Context, vector []float32, topK int) ([]ScoredChunk, error) {
	resp, err := s.points.Search(ctx, &qdrant.SearchPoints{
		CollectionName: s.name,
		Vector:         vector,
		Limit:          uint64(topK),
		WithPayload:    qdrant.NewWithPayload(true),
	})
	if err != nil {
		return nil, err
	}

	results := make([]ScoredChunk, len(resp.Result))
	for i, r := range resp.Result {
		p := r.Payload
		results[i] = ScoredChunk{
			DocumentChunk: DocumentChunk{
				Text:      pbStr(p, "text"),
				FilePath:  pbStr(p, "file_path"),
				Header:    pbStr(p, "header"),
				LineStart: int(pbInt(p, "line_start")),
			},
			SourceSHA: pbStr(p, "source_sha"),
			Score:     r.Score,
		}
	}
	return results, nil
}

func (s *QdrantStore) GetFileSHA(ctx context.Context, filePath string) (string, error) {
	resp, err := s.points.Scroll(ctx, &qdrant.ScrollPoints{
		CollectionName: s.name,
		Filter: &qdrant.Filter{
			Must: []*qdrant.Condition{
				{
					ConditionOneOf: &qdrant.Condition_Field{
						Field: &qdrant.FieldCondition{
							Key: "file_path",
							Match: &qdrant.Match{
								MatchValue: &qdrant.Match_Keyword{Keyword: filePath},
							},
						},
					},
				},
			},
		},
		Limit:       qdrant.PtrOf(uint32(1)),
		WithPayload: qdrant.NewWithPayload(true),
	})
	if err != nil {
		return "", err
	}
	if len(resp.Result) == 0 {
		return "", nil
	}
	return pbStr(resp.Result[0].Payload, "source_sha"), nil
}

func chunkID(filePath string, lineStart int) uint64 {
	h := uint64(14695981039346656037)
	for _, b := range []byte(filePath) {
		h ^= uint64(b)
		h *= 1099511628211
	}
	h ^= uint64(lineStart)
	h *= 1099511628211
	return h
}

func strVal(s string) *qdrant.Value {
	return &qdrant.Value{Kind: &qdrant.Value_StringValue{StringValue: s}}
}

func intVal(n int64) *qdrant.Value {
	return &qdrant.Value{Kind: &qdrant.Value_IntegerValue{IntegerValue: n}}
}

func pbStr(p map[string]*qdrant.Value, key string) string {
	if v, ok := p[key]; ok {
		if s, ok := v.Kind.(*qdrant.Value_StringValue); ok {
			return s.StringValue
		}
	}
	return ""
}

func pbInt(p map[string]*qdrant.Value, key string) int64 {
	if v, ok := p[key]; ok {
		if n, ok := v.Kind.(*qdrant.Value_IntegerValue); ok {
			return n.IntegerValue
		}
	}
	return 0
}
