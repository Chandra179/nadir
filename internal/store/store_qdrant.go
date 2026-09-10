package store

import (
	"context"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	qdrant "github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"nadir/internal/qdrantutil"
)

// sparseVectorName is the named sparse vector next to the unnamed dense
// one. Qdrant's Idf modifier applies corpus-wide IDF server-side over the
// raw term counts, giving a real BM25-style ranked leg.
const sparseVectorName = "bm25"

func (s *dependencies) EnsureCollection(ctx context.Context, dimensions int) error {
	s.dimensions = dimensions
	info, err := s.collection.Get(ctx, &qdrant.GetCollectionInfoRequest{CollectionName: s.name})
	if err != nil {
		if status.Code(err) != codes.NotFound {
			return fmt.Errorf("qdrant get collection: %w", err)
		}
		return s.createCollection(ctx, dimensions)
	}
	if err := qdrantutil.ValidateDenseCollection(s.name, info.GetResult(), dimensions); err != nil {
		return err
	}
	if !qdrantutil.HasSparseVector(info.GetResult(), sparseVectorName) {
		return fmt.Errorf("qdrant collection %q is missing sparse vector %q; reset/recreate it", s.name, sparseVectorName)
	}
	return nil
}

// createCollection creates the collection (dense + BM25 sparse vectors) and
// payload field indexes from scratch. Qdrant fixes a collection's named
// vectors at creation time, so a new named vector can only arrive via
// drop + recreate, never in place.
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
			Lowercase: new(true),
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
			"file_path":   qdrantutil.StringValue(c.FilePath),
			"header":      qdrantutil.StringValue(c.Header),
			"line_start":  qdrantutil.IntValue(int64(c.LineStart)),
			"chunk_index": qdrantutil.IntValue(int64(c.ChunkIndex)),
			"text":        qdrantutil.StringValue(c.Text),
			"window_text": qdrantutil.StringValue(c.WindowText),
			"source_sha":  qdrantutil.StringValue(c.SourceSHA),
			"ingested_at": qdrantutil.StringValue(ingestedAt),
		}
		if c.HypeQuestion != "" {
			payload["hype_question"] = qdrantutil.StringValue(c.HypeQuestion)
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

// DeleteAll drops the collection and recreates it (dense + bm25 sparse
// vectors, payload indexes). Qdrant fixes named vectors at creation time,
// so a schema change can only be picked up by drop + recreate, not point
// deletes.
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
		Limit:       new(uint32(topK)),
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
			fp := qdrantutil.StringFromPayload(pt.Payload, "file_path")
			if fp != "" {
				shas[fp] = qdrantutil.StringFromPayload(pt.Payload, "source_sha")
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
		Text:       qdrantutil.StringFromPayload(p, "text"),
		WindowText: qdrantutil.StringFromPayload(p, "window_text"),
		FilePath:   qdrantutil.StringFromPayload(p, "file_path"),
		Header:     qdrantutil.StringFromPayload(p, "header"),
		LineStart:  int(qdrantutil.IntFromPayload(p, "line_start")),
		ChunkIndex: int(qdrantutil.IntFromPayload(p, "chunk_index")),
		SourceSHA:  qdrantutil.StringFromPayload(p, "source_sha"),
		IngestedAt: qdrantutil.StringFromPayload(p, "ingested_at"),
	}
}

// vectorizeSparse builds a term-frequency sparse vector for the BM25 leg.
// Terms are hashed into a fixed index space (no persisted vocabulary), so
// ingest and query-time encodings match; IDF is applied server-side by
// Qdrant's Idf modifier.
func vectorizeSparse(text string) (indices []uint32, values []float32) {
	counts := make(map[uint32]float32)
	for _, tok := range tokenize(text) {
		h := fnv.New32a()
		h.Write([]byte(tok))
		counts[h.Sum32()]++
	}

	indices = make([]uint32, 0, len(counts))
	values = make([]float32, 0, len(counts))
	for idx, c := range counts {
		indices = append(indices, idx)
		values = append(values, c)
	}
	return indices, values
}

func tokenize(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}
