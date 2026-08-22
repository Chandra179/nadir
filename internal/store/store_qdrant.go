package store

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	"github.com/google/uuid"
	qdrant "github.com/qdrant/go-client/qdrant"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *dependencies) EnsureCollection(ctx context.Context, dimensions int) error {
	_, err := s.collection.Get(ctx, &qdrant.GetCollectionInfoRequest{CollectionName: s.name})
	if err != nil {
		if status.Code(err) != codes.NotFound {
			return fmt.Errorf("qdrant get collection: %w", err)
		}
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
			return fmt.Errorf("qdrant create collection: %w", err)
		}
	}
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
	if err != nil {
		return fmt.Errorf("qdrant create text index: %w", err)
	}
	fk := qdrant.FieldType_FieldTypeKeyword
	_, err = s.points.CreateFieldIndex(ctx, &qdrant.CreateFieldIndexCollection{
		CollectionName: s.name,
		FieldName:      "file_path",
		FieldType:      &fk,
	})
	if err != nil {
		return fmt.Errorf("qdrant create file_path index: %w", err)
	}
	return nil
}

func (s *dependencies) Upsert(ctx context.Context, chunks []ScoredChunk) error {
	points := make([]*qdrant.PointStruct, len(chunks))
	for i, c := range chunks {
		id := chunkID(c.FilePath, c.LineStart, c.ChunkIndex)
		points[i] = &qdrant.PointStruct{
			Id:      qdrant.NewIDUUID(id),
			Vectors: qdrant.NewVectors(c.Vector...),
			Payload: map[string]*qdrant.Value{
				"file_path":   strVal(c.FilePath),
				"header":      strVal(c.Header),
				"line_start":  intVal(int64(c.LineStart)),
				"chunk_index": intVal(int64(c.ChunkIndex)),
				"text":        strVal(c.Text),
				"window_text": strVal(c.WindowText),
				"source_sha":  strVal(c.SourceSHA),
			},
		}
	}
	_, err := s.points.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: s.name,
		Points:         points,
	})
	return err
}

func (s *dependencies) DeleteByFile(ctx context.Context, filePath string) error {
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

func buildFilterConditions(f *SearchFilter) []*qdrant.Condition {
	if f == nil {
		return nil
	}
	var conds []*qdrant.Condition
	if f.FilePath != "" {
		conds = append(conds, &qdrant.Condition{
			ConditionOneOf: &qdrant.Condition_Field{
				Field: &qdrant.FieldCondition{
					Key:   "file_path",
					Match: &qdrant.Match{MatchValue: &qdrant.Match_Keyword{Keyword: f.FilePath}},
				},
			},
		})
	}
	if f.Header != "" {
		conds = append(conds, &qdrant.Condition{
			ConditionOneOf: &qdrant.Condition_Field{
				Field: &qdrant.FieldCondition{
					Key:   "header",
					Match: &qdrant.Match{MatchValue: &qdrant.Match_Keyword{Keyword: f.Header}},
				},
			},
		})
	}
	if f.SourceSHA != "" {
		conds = append(conds, &qdrant.Condition{
			ConditionOneOf: &qdrant.Condition_Field{
				Field: &qdrant.FieldCondition{
					Key:   "source_sha",
					Match: &qdrant.Match{MatchValue: &qdrant.Match_Keyword{Keyword: f.SourceSHA}},
				},
			},
		})
	}
	return conds
}

func toQdrantFilter(conds []*qdrant.Condition) *qdrant.Filter {
	if len(conds) == 0 {
		return nil
	}
	return &qdrant.Filter{Must: conds}
}

func (s *dependencies) HybridSearch(ctx context.Context, vector []float32, query string, topK int, filter *SearchFilter) ([]ScoredChunk, error) {
	return s.hybridSearchClient(ctx, vector, query, topK, filter)
}

func (s *dependencies) hybridSearchClient(ctx context.Context, vector []float32, query string, topK int, filter *SearchFilter) ([]ScoredChunk, error) {
	fetchN := topK * s.prefetchMul

	denseResults, err := s.searchWithFilter(ctx, vector, fetchN, filter)
	if err != nil {
		return nil, fmt.Errorf("dense search: %w", err)
	}

	bm25Results, err := s.KeywordSearch(ctx, query, fetchN, filter)
	if err != nil {
		s.log.Warn("bm25 leg failed, falling back to dense-only results", zap.Error(err))
		if len(denseResults) > topK {
			denseResults = denseResults[:topK]
		}
		return denseResults, nil
	}

	rrfK := 60.0
	denseRank := make(map[string]int)
	for i, r := range denseResults {
		denseRank[r.Key()] = i + 1
	}
	bm25Rank := make(map[string]int)
	for i, r := range bm25Results {
		bm25Rank[r.Key()] = i + 1
	}

	seen := make(map[string]ScoredChunk)
	for _, r := range denseResults {
		scored := r
		scored.Score = 0
		if dr, ok := denseRank[r.Key()]; ok {
			scored.Score += float32(1.0 / (rrfK + float64(dr)))
		}
		if br, ok := bm25Rank[r.Key()]; ok {
			scored.Score += float32(1.0 / (rrfK + float64(br)))
		}
		seen[r.Key()] = scored
	}
	for _, r := range bm25Results {
		if _, ok := seen[r.Key()]; ok {
			continue
		}
		scored := r
		scored.Score = 0
		if br, ok := bm25Rank[r.Key()]; ok {
			scored.Score += float32(1.0 / (rrfK + float64(br)))
		}
		seen[r.Key()] = scored
	}

	merged := make([]ScoredChunk, 0, len(seen))
	for _, c := range seen {
		merged = append(merged, c)
	}
	sortChunksByScore(merged)
	if len(merged) > topK {
		merged = merged[:topK]
	}
	return merged, nil
}

func (s *dependencies) searchWithFilter(ctx context.Context, vector []float32, topK int, filter *SearchFilter) ([]ScoredChunk, error) {
	limit := uint64(topK)
	qf := toQdrantFilter(buildFilterConditions(filter))
	resp, err := s.points.Query(ctx, &qdrant.QueryPoints{
		CollectionName: s.name,
		Query:          qdrant.NewQueryDense(vector),
		Limit:          &limit,
		Filter:         qf,
		WithPayload:    qdrant.NewWithPayload(true),
	})
	if err != nil {
		return nil, err
	}

	results := make([]ScoredChunk, len(resp.Result))
	for i, r := range resp.Result {
		results[i] = chunkFromPayload(r.Payload)
		results[i].Score = r.Score
	}
	return results, nil
}

func sortChunksByScore(chunks []ScoredChunk) {
	sort.Slice(chunks, func(i, j int) bool { return chunks[i].Score > chunks[j].Score })
}

func (s *dependencies) KeywordSearch(ctx context.Context, keyword string, topK int, filter *SearchFilter) ([]ScoredChunk, error) {
	conds := append([]*qdrant.Condition{qdrant.NewMatchText("text", keyword)}, buildFilterConditions(filter)...)
	resp, err := s.points.Scroll(ctx, &qdrant.ScrollPoints{
		CollectionName: s.name,
		Filter: &qdrant.Filter{
			Must: conds,
		},
		Limit:       qdrant.PtrOf(uint32(topK)),
		WithPayload: qdrant.NewWithPayload(true),
	})
	if err != nil {
		return nil, err
	}
	results := make([]ScoredChunk, len(resp.Result))
	for i, r := range resp.Result {
		results[i] = chunkFromPayload(r.Payload)
	}
	return results, nil
}

func (s *dependencies) GetAllFileSHAs(ctx context.Context) (map[string]string, error) {
	shas := make(map[string]string)
	var offset *qdrant.PointId
	pageSize := uint32(1000)
	for {
		resp, err := s.points.Scroll(ctx, &qdrant.ScrollPoints{
			CollectionName: s.name,
			Limit:          &pageSize,
			Offset:         offset,
			WithPayload:    qdrant.NewWithPayloadInclude("file_path", "source_sha"),
			WithVectors:    qdrant.NewWithVectors(false),
		})
		if err != nil {
			return nil, fmt.Errorf("scroll all file shas: %w", err)
		}
		for _, pt := range resp.Result {
			fp := pbStr(pt.Payload, "file_path")
			if fp != "" {
				shas[fp] = pbStr(pt.Payload, "source_sha")
			}
		}
		if resp.NextPageOffset == nil {
			break
		}
		offset = resp.NextPageOffset
	}
	return shas, nil
}

var chunkIDNamespace = uuid.MustParse("a3b4c5d6-e7f8-4a5b-9c0d-1e2f3a4b5c6d")

func chunkID(filePath string, lineStart, idx int) string {
	key := filePath + ":" + strconv.Itoa(lineStart) + ":" + strconv.Itoa(idx)
	return uuid.NewSHA1(chunkIDNamespace, []byte(key)).String()
}

func chunkFromPayload(p map[string]*qdrant.Value) ScoredChunk {
	return ScoredChunk{
		Text:       pbStr(p, "text"),
		WindowText: pbStr(p, "window_text"),
		FilePath:   pbStr(p, "file_path"),
		Header:     pbStr(p, "header"),
		LineStart:  int(pbInt(p, "line_start")),
		ChunkIndex: int(pbInt(p, "chunk_index")),
		SourceSHA:  pbStr(p, "source_sha"),
	}
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
