package engine

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	"github.com/google/uuid"
	qdrant "github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// QdrantStore implements Store backed by Qdrant via gRPC.
type QdrantStore struct {
	conn         *grpc.ClientConn
	points       qdrant.PointsClient
	collection   qdrant.CollectionsClient
	name         string
	prefetchMul  int
	sparseScorer SparseScorer
	// sparseEmbedder, when set, enables server-side hybrid search via QueryPoints.
	// At query time, the query is embedded as a sparse vector and sent alongside the dense vector.
	sparseEmbedder SparseEmbedder
}

func NewQdrantStore(addr, collection string, prefetchMul int) (*QdrantStore, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("qdrant dial %s: %w", addr, err)
	}
	if prefetchMul <= 0 {
		prefetchMul = 5
	}
	return &QdrantStore{
		conn:         conn,
		points:       qdrant.NewPointsClient(conn),
		collection:   qdrant.NewCollectionsClient(conn),
		name:         collection,
		prefetchMul:  prefetchMul,
		sparseScorer: TFSparseScorer{},
	}, nil
}

// WithSparseScorer swaps the client-side BM25 leg scorer. Default: TFSparseScorer.
func (s *QdrantStore) WithSparseScorer(scorer SparseScorer) *QdrantStore {
	s.sparseScorer = scorer
	return s
}

// WithSparseEmbedder enables server-side hybrid search via Qdrant QueryPoints.
// Requires sparse vectors to have been stored at ingest time.
func (s *QdrantStore) WithSparseEmbedder(se SparseEmbedder) *QdrantStore {
	s.sparseEmbedder = se
	return s
}

const sparseVectorName = "sparse"

func (s *QdrantStore) EnsureCollection(ctx context.Context, dimensions int) error {
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
			SparseVectorsConfig: qdrant.NewSparseVectorsConfig(map[string]*qdrant.SparseVectorParams{
				sparseVectorName: {Modifier: qdrant.Modifier_Idf.Enum()},
			}),
		})
		if err != nil {
			return fmt.Errorf("qdrant create collection: %w", err)
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
	if err != nil {
		return fmt.Errorf("qdrant create text index: %w", err)
	}
	// Keyword index on file_path: eliminates full-collection scan in GetFileSHA and DeleteByFile.
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

func (s *QdrantStore) Upsert(ctx context.Context, chunks []ScoredChunk) error {
	points := make([]*qdrant.PointStruct, len(chunks))
	for i, c := range chunks {
		id := chunkID(c.FilePath, c.LineStart, c.ChunkIndex)
		var vectors *qdrant.Vectors
		if len(c.SparseIndices) > 0 {
			vectors = &qdrant.Vectors{
				VectorsOptions: &qdrant.Vectors_Vectors{
					Vectors: &qdrant.NamedVectors{
						Vectors: map[string]*qdrant.Vector{
							"": {
								Vector: &qdrant.Vector_Dense{
									Dense: &qdrant.DenseVector{Data: c.Vector},
								},
							},
							sparseVectorName: {
								Vector: &qdrant.Vector_Sparse{
									Sparse: &qdrant.SparseVector{
										Indices: c.SparseIndices,
										Values:  c.SparseValues,
									},
								},
							},
						},
					},
				},
			}
		} else {
			vectors = qdrant.NewVectors(c.Vector...)
		}
		points[i] = &qdrant.PointStruct{
			Id:      qdrant.NewIDUUID(id),
			Vectors: vectors,
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

// buildFilterConditions converts a SearchFilter into Qdrant Must conditions.
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

// HybridSearch combines dense and sparse retrieval. When a SparseEmbedder is wired
// (server-side path), the store issues a single Qdrant QueryPoints with dense+sparse
// prefetch legs and server-side RRF fusion. Without a SparseEmbedder, the store falls
// back to a client-side hybrid: dense ANN search + BM25 text search, fused via RRF.
func (s *QdrantStore) HybridSearch(ctx context.Context, vector []float32, query string, topK int, filter *SearchFilter) ([]ScoredChunk, error) {
	if s.sparseEmbedder != nil {
		return s.hybridSearchServer(ctx, vector, query, topK, filter)
	}
	return s.hybridSearchClient(ctx, vector, query, topK, filter)
}

// hybridSearchServer uses Qdrant QueryPoints with dense+sparse prefetch and server-side RRF.
func (s *QdrantStore) hybridSearchServer(ctx context.Context, vector []float32, query string, topK int, filter *SearchFilter) ([]ScoredChunk, error) {
	fetchN := uint64(topK * s.prefetchMul)

	sparseIdx, sparseVals, err := s.sparseEmbedder.EmbedSparse(ctx, query, "query")
	if err != nil {
		return nil, fmt.Errorf("sparse embed query: %w", err)
	}

	limit := uint64(topK)
	qf := toQdrantFilter(buildFilterConditions(filter))
	resp, err := s.points.Query(ctx, &qdrant.QueryPoints{
		CollectionName: s.name,
		Prefetch: []*qdrant.PrefetchQuery{
			{
				Query:  qdrant.NewQueryDense(vector),
				Limit:  &fetchN,
				Filter: qf,
			},
			{
				Query:  qdrant.NewQuerySparse(sparseIdx, sparseVals),
				Using:  qdrant.PtrOf(sparseVectorName),
				Limit:  &fetchN,
				Filter: qf,
			},
		},
		Query:       qdrant.NewQueryFusion(qdrant.Fusion_RRF),
		Limit:       &limit,
		WithPayload: qdrant.NewWithPayload(true),
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

// hybridSearchClient runs dense + BM25 text search locally, then fuses via RRF.
// This is the fallback path when no SparseEmbedder is wired.
func (s *QdrantStore) hybridSearchClient(ctx context.Context, vector []float32, query string, topK int, filter *SearchFilter) ([]ScoredChunk, error) {
	fetchN := topK * s.prefetchMul

	denseResults, err := s.searchWithFilter(ctx, vector, fetchN, filter)
	if err != nil {
		return nil, fmt.Errorf("dense search: %w", err)
	}

	bm25Results, err := s.KeywordSearch(ctx, query, fetchN, filter)
	if err != nil {
		return nil, fmt.Errorf("bm25 search: %w", err)
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

// searchWithFilter runs a dense ANN search with an optional payload filter.
// Uses QueryPoints with a single dense leg for filter support (SearchPoints lacks filters).
func (s *QdrantStore) searchWithFilter(ctx context.Context, vector []float32, topK int, filter *SearchFilter) ([]ScoredChunk, error) {
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

func (s *QdrantStore) KeywordSearch(ctx context.Context, keyword string, topK int, filter *SearchFilter) ([]ScoredChunk, error) {
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
		results[i] = chunkFromPayload(r.Payload)
		results[i].Score = r.Score
	}
	return results, nil
}

func (s *QdrantStore) GetAllFileSHAs(ctx context.Context) (map[string]string, error) {
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

// chunkIDNamespace is a private UUID namespace for deterministic chunk point IDs.
var chunkIDNamespace = uuid.MustParse("a3b4c5d6-e7f8-4a5b-9c0d-1e2f3a4b5c6d")

func chunkID(filePath string, lineStart, idx int) string {
	key := filePath + ":" + strconv.Itoa(lineStart) + ":" + strconv.Itoa(idx)
	return uuid.NewSHA1(chunkIDNamespace, []byte(key)).String()
}

func chunkFromPayload(p map[string]*qdrant.Value) ScoredChunk {
	return ScoredChunk{
		DocumentChunk: DocumentChunk{
			Text:       pbStr(p, "text"),
			WindowText: pbStr(p, "window_text"),
			FilePath:   pbStr(p, "file_path"),
			Header:     pbStr(p, "header"),
			LineStart:  int(pbInt(p, "line_start")),
			ChunkIndex: int(pbInt(p, "chunk_index")),
		},
		SourceSHA: pbStr(p, "source_sha"),
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
