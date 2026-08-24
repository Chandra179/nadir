package store

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	qdrant "github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// sparseVectorName is the named sparse vector field alongside the default
// (unnamed) dense vector. Qdrant's Idf modifier applies corpus-wide IDF
// weighting server-side to the raw term counts vectorizeSparse produces,
// giving a real BM25-style ranked leg instead of an unranked text filter.
const sparseVectorName = "bm25"

func (s *dependencies) EnsureCollection(ctx context.Context, dimensions int) error {
	s.dimensions = dimensions
	_, err := s.collection.Get(ctx, &qdrant.GetCollectionInfoRequest{CollectionName: s.name})
	if err != nil {
		if status.Code(err) != codes.NotFound {
			return fmt.Errorf("qdrant get collection: %w", err)
		}
		return s.createCollection(ctx, dimensions)
	}
	return nil
}

// createCollection creates the collection (dense + BM25 sparse vectors) and
// its payload field indexes from scratch. Qdrant fixes a collection's named
// vectors at creation time — an existing collection can't gain a new named
// vector (e.g. the bm25 sparse leg) via an in-place update, only by dropping
// and recreating it.
func (s *dependencies) createCollection(ctx context.Context, dimensions int) error {
	idf := qdrant.Modifier_Idf
	_, err := s.collection.Create(ctx, &qdrant.CreateCollection{
		CollectionName: s.name,
		VectorsConfig: &qdrant.VectorsConfig{
			Config: &qdrant.VectorsConfig_Params{
				Params: &qdrant.VectorParams{
					Size:     uint64(dimensions),
					Distance: qdrant.Distance_Cosine,
				},
			},
		},
		SparseVectorsConfig: qdrant.NewSparseVectorsConfig(map[string]*qdrant.SparseVectorParams{
			sparseVectorName: {Modifier: &idf},
		}),
	})
	if err != nil {
		return fmt.Errorf("qdrant create collection: %w", err)
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
	for _, field := range []string{"file_path", "header", "source_sha"} {
		fk := qdrant.FieldType_FieldTypeKeyword
		_, err = s.points.CreateFieldIndex(ctx, &qdrant.CreateFieldIndexCollection{
			CollectionName: s.name,
			FieldName:      field,
			FieldType:      &fk,
		})
		if err != nil {
			return fmt.Errorf("qdrant create %s index: %w", field, err)
		}
	}
	return nil
}

func (s *dependencies) Upsert(ctx context.Context, chunks []ScoredChunk) error {
	points := make([]*qdrant.PointStruct, len(chunks))
	for i, c := range chunks {
		id := pointID(c)
		sparseSrc := c.SparseText
		if sparseSrc == "" {
			sparseSrc = contextualSparseText(c.FilePath, c.Header, c.Text)
		}
		sparseIdx, sparseVal := vectorizeSparse(sparseSrc)
		ingestedAt := c.IngestedAt
		if ingestedAt == "" {
			ingestedAt = time.Now().UTC().Format(time.RFC3339)
		}
		payload := map[string]*qdrant.Value{
			"file_path":   strVal(c.FilePath),
			"header":      strVal(c.Header),
			"line_start":  intVal(int64(c.LineStart)),
			"chunk_index": intVal(int64(c.ChunkIndex)),
			"text":        strVal(c.Text),
			"window_text": strVal(c.WindowText),
			"source_sha":  strVal(c.SourceSHA),
			"ingested_at": strVal(ingestedAt),
		}
		if c.HypeQuestion != "" {
			payload["hype_question"] = strVal(c.HypeQuestion)
		}
		points[i] = &qdrant.PointStruct{
			Id: qdrant.NewIDUUID(id),
			Vectors: qdrant.NewVectorsMap(map[string]*qdrant.Vector{
				"":               qdrant.NewVectorDense(c.Vector),
				sparseVectorName: qdrant.NewVectorSparse(sparseIdx, sparseVal),
			}),
			Payload: payload,
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

// DeleteAll drops the collection entirely and recreates it from scratch
// (dense + bm25 sparse vectors, payload field indexes). A point-only delete
// isn't enough to fix a collection whose schema has drifted from what the
// current code expects (e.g. a collection created before the bm25 sparse
// vector was added) — Qdrant fixes named vectors at creation time, so the
// only way to pick up a schema change is to drop and recreate.
func (s *dependencies) DeleteAll(ctx context.Context) error {
	_, err := s.collection.Delete(ctx, &qdrant.DeleteCollection{CollectionName: s.name})
	if err != nil && status.Code(err) != codes.NotFound {
		return fmt.Errorf("qdrant delete collection: %w", err)
	}
	return s.createCollection(ctx, s.dimensions)
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

// HybridSearch runs dense and BM25-style sparse legs as Qdrant-native
// prefetches and fuses them server-side with RRF in a single round trip,
// rather than issuing two separate queries and re-implementing RRF in Go.
func (s *dependencies) HybridSearch(ctx context.Context, vector []float32, query string, topK int, filter *SearchFilter) ([]ScoredChunk, error) {
	fetchN := uint64(topK * s.prefetchMul)
	limit := uint64(topK)
	qf := toQdrantFilter(buildFilterConditions(filter))
	sparseIdx, sparseVal := vectorizeSparse(query)

	prefetch := []*qdrant.PrefetchQuery{
		{
			Query:  qdrant.NewQueryDense(vector),
			Filter: qf,
			Limit:  &fetchN,
		},
	}
	if len(sparseIdx) > 0 {
		sparseName := sparseVectorName
		prefetch = append(prefetch, &qdrant.PrefetchQuery{
			Query:  qdrant.NewQuerySparse(sparseIdx, sparseVal),
			Using:  &sparseName,
			Filter: qf,
			Limit:  &fetchN,
		})
	}

	resp, err := s.points.Query(ctx, &qdrant.QueryPoints{
		CollectionName: s.name,
		Prefetch:       prefetch,
		Query:          qdrant.NewQueryFusion(qdrant.Fusion_RRF),
		Filter:         qf,
		Limit:          &limit,
		WithPayload:    qdrant.NewWithPayload(true),
	})
	if err != nil {
		return nil, fmt.Errorf("hybrid search: %w", err)
	}

	results := make([]ScoredChunk, len(resp.Result))
	for i, r := range resp.Result {
		results[i] = chunkFromPayload(r.Payload)
		results[i].Score = r.Score
	}
	return results, nil
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

func (s *dependencies) Stats(ctx context.Context) (Stats, error) {
	shas, err := s.GetAllFileSHAs(ctx)
	if err != nil {
		return Stats{}, fmt.Errorf("stats file count: %w", err)
	}

	info, err := s.collection.Get(ctx, &qdrant.GetCollectionInfoRequest{CollectionName: s.name})
	if err != nil {
		return Stats{}, fmt.Errorf("stats collection info: %w", err)
	}

	return Stats{
		Documents: len(shas),
		Chunks:    int(info.GetResult().GetPointsCount()),
	}, nil
}

var chunkIDNamespace = uuid.MustParse("a3b4c5d6-e7f8-4a5b-9c0d-1e2f3a4b5c6d")

// pointID derives a stable UUID for a chunk (or HyPE sibling) from its
// identity fields; siblings get ":hype:<n>" appended so they never collide
// with their parent.
func pointID(c ScoredChunk) string {
	key := c.FilePath + ":" + strconv.Itoa(c.LineStart) + ":" + strconv.Itoa(c.ChunkIndex)
	if c.HypeQuestion != "" {
		key += ":hype:" + strconv.Itoa(c.HypeIndex)
	}
	return uuid.NewSHA1(chunkIDNamespace, []byte(key)).String()
}

// contextualSparseText mirrors the chunker's ContextualText format so the
// BM25 leg indexes the same context the dense leg embeds.
func contextualSparseText(filePath, header, text string) string {
	var sb strings.Builder
	sb.WriteString(filePath)
	if header != "" {
		sb.WriteString(" > ")
		sb.WriteString(header)
	}
	sb.WriteString("\n")
	sb.WriteString(text)
	return sb.String()
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
		IngestedAt: pbStr(p, "ingested_at"),
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
