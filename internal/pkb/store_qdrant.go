package pkb

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"strconv"

	qdrant "github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// QdrantStore implements Store backed by Qdrant via gRPC.
type QdrantStore struct {
	points       qdrant.PointsClient
	collection   qdrant.CollectionsClient
	name         string
	sparseScorer SparseScorer
	// sparseEmbedder, when set, enables server-side hybrid search via QueryPoints.
	// At query time, the query is embedded as a sparse vector and sent alongside the dense vector.
	sparseEmbedder SparseEmbedder
}

func NewQdrantStore(addr, collection string) (*QdrantStore, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("qdrant dial %s: %w", addr, err)
	}
	return &QdrantStore{
		points:       qdrant.NewPointsClient(conn),
		collection:   qdrant.NewCollectionsClient(conn),
		name:         collection,
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
	return nil
}

func (s *QdrantStore) Upsert(ctx context.Context, chunks []ScoredChunk) error {
	points := make([]*qdrant.PointStruct, len(chunks))
	for i, c := range chunks {
		id := chunkID(c.FilePath, c.LineStart)
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
			Id:      qdrant.NewIDNum(id),
			Vectors: vectors,
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

// HybridSearch combines dense and sparse retrieval via RRF.
// When sparseEmbedder is set, uses Qdrant server-side QueryPoints with prefetch (true hybrid).
// Otherwise falls back to client-side scroll+rescore with sparseScorer.
func (s *QdrantStore) HybridSearch(ctx context.Context, vector []float32, query string, topK int) ([]ScoredChunk, error) {
	if s.sparseEmbedder != nil {
		return s.hybridSearchServer(ctx, vector, query, topK)
	}
	return s.hybridSearchClient(ctx, vector, query, topK)
}

// hybridSearchServer uses Qdrant QueryPoints with dense+sparse prefetch legs and server-side RRF.
func (s *QdrantStore) hybridSearchServer(ctx context.Context, vector []float32, query string, topK int) ([]ScoredChunk, error) {
	fetchN := uint64(topK * 5)

	sparseIdx, sparseVals, err := s.sparseEmbedder.EmbedSparse(ctx, query, "query")
	if err != nil {
		return nil, fmt.Errorf("sparse embed query: %w", err)
	}

	limit := uint64(topK)
	resp, err := s.points.Query(ctx, &qdrant.QueryPoints{
		CollectionName: s.name,
		Prefetch: []*qdrant.PrefetchQuery{
			{
				Query: qdrant.NewQueryDense(vector),
				Limit: &fetchN,
			},
			{
				Query: qdrant.NewQuerySparse(sparseIdx, sparseVals),
				Using: qdrant.PtrOf(sparseVectorName),
				Limit: &fetchN,
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

// hybridSearchClient fetches dense + text-filtered candidates, reranks sparse leg client-side, fuses via RRF.
func (s *QdrantStore) hybridSearchClient(ctx context.Context, vector []float32, query string, topK int) ([]ScoredChunk, error) {
	fetchN := topK * 5

	denseResp, err := s.points.Search(ctx, &qdrant.SearchPoints{
		CollectionName: s.name,
		Vector:         vector,
		Limit:          uint64(fetchN),
		WithPayload:    qdrant.NewWithPayload(true),
	})
	if err != nil {
		return nil, fmt.Errorf("dense search: %w", err)
	}

	scrollLimit := uint32(fetchN)
	bm25Resp, err := s.points.Scroll(ctx, &qdrant.ScrollPoints{
		CollectionName: s.name,
		Filter: &qdrant.Filter{
			Must: []*qdrant.Condition{
				qdrant.NewMatchText("text", query),
			},
		},
		Limit:       &scrollLimit,
		WithPayload: qdrant.NewWithPayload(true),
	})
	if err != nil {
		return nil, fmt.Errorf("bm25 search: %w", err)
	}

	type entry struct {
		chunk    ScoredChunk
		denseRnk int
		bm25Rnk  int
	}
	byID := make(map[string]*entry)

	toChunk := func(p map[string]*qdrant.Value) ScoredChunk {
		return ScoredChunk{
			DocumentChunk: DocumentChunk{
				Text:      pbStr(p, "text"),
				FilePath:  pbStr(p, "file_path"),
				Header:    pbStr(p, "header"),
				LineStart: int(pbInt(p, "line_start")),
			},
			SourceSHA: pbStr(p, "source_sha"),
		}
	}
	chunkKey := func(c ScoredChunk) string {
		return c.FilePath + ":" + strconv.Itoa(c.LineStart)
	}

	for rank, r := range denseResp.Result {
		c := toChunk(r.Payload)
		k := chunkKey(c)
		if byID[k] == nil {
			byID[k] = &entry{chunk: c}
		}
		byID[k].denseRnk = rank + 1
	}
	type bm25Hit struct {
		key   string
		score float64
	}
	var bm25Hits []bm25Hit
	for _, r := range bm25Resp.Result {
		c := toChunk(r.Payload)
		k := chunkKey(c)
		if byID[k] == nil {
			byID[k] = &entry{chunk: c}
		}
		score, err := s.sparseScorer.Score(ctx, query, c.Text)
		if err != nil {
			return nil, fmt.Errorf("sparse score: %w", err)
		}
		bm25Hits = append(bm25Hits, bm25Hit{key: k, score: score})
	}
	sort.Slice(bm25Hits, func(i, j int) bool { return bm25Hits[i].score > bm25Hits[j].score })
	for rank, h := range bm25Hits {
		byID[h.key].bm25Rnk = rank + 1
	}

	const rrfK = 60.0
	type scored struct {
		key   string
		score float64
	}
	var fused []scored
	for k, e := range byID {
		var sc float64
		if e.denseRnk > 0 {
			sc += 1.0 / (rrfK + float64(e.denseRnk))
		}
		if e.bm25Rnk > 0 {
			sc += 1.0 / (rrfK + float64(e.bm25Rnk))
		}
		fused = append(fused, scored{key: k, score: sc})
	}
	sort.Slice(fused, func(i, j int) bool { return fused[i].score > fused[j].score })

	n := topK
	if n > len(fused) {
		n = len(fused)
	}
	results := make([]ScoredChunk, n)
	for i := 0; i < n; i++ {
		e := byID[fused[i].key]
		e.chunk.Score = float32(fused[i].score)
		results[i] = e.chunk
	}
	return results, nil
}

func (s *QdrantStore) KeywordSearch(ctx context.Context, keyword string, topK int) ([]ScoredChunk, error) {
	resp, err := s.points.Scroll(ctx, &qdrant.ScrollPoints{
		CollectionName: s.name,
		Filter: &qdrant.Filter{
			Must: []*qdrant.Condition{
				qdrant.NewMatchText("text", keyword),
			},
		},
		Limit:       qdrant.PtrOf(uint32(topK)),
		WithPayload: qdrant.NewWithPayload(true),
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
	key := filePath + ":" + strconv.Itoa(lineStart)
	sum := sha256.Sum256([]byte(key))
	return binary.LittleEndian.Uint64(sum[:8])
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
