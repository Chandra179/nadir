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
	"google.golang.org/grpc/credentials/insecure"
)

// QdrantStore implements Store backed by Qdrant via gRPC.
type QdrantStore struct {
	points        qdrant.PointsClient
	collection    qdrant.CollectionsClient
	name          string
	sparseScorer  SparseScorer
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

// WithSparseScorer swaps the BM25 leg scorer (e.g. SPLADE). Default: TFSparseScorer.
func (s *QdrantStore) WithSparseScorer(scorer SparseScorer) *QdrantStore {
	s.sparseScorer = scorer
	return s
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

// HybridSearch combines dense vector search with client-side BM25 via RRF.
// Qdrant's payload full-text filter is unscored, so we fetch candidates from both
// modalities separately and merge using Reciprocal Rank Fusion (k=60, Cormack 2009).
func (s *QdrantStore) HybridSearch(ctx context.Context, vector []float32, query string, topK int) ([]ScoredChunk, error) {
	fetchN := topK * 5

	// Dense retrieval leg.
	denseResp, err := s.points.Search(ctx, &qdrant.SearchPoints{
		CollectionName: s.name,
		Vector:         vector,
		Limit:          uint64(fetchN),
		WithPayload:    qdrant.NewWithPayload(true),
	})
	if err != nil {
		return nil, fmt.Errorf("dense search: %w", err)
	}

	// BM25 leg: fetch text-matched candidates and score with TF-IDF approximation.
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

	// Build chunk map and ranked lists for RRF.
	type entry struct {
		chunk    ScoredChunk
		denseRnk int // 1-indexed rank; 0 = not present
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
		bm25Hits = append(bm25Hits, bm25Hit{key: k, score: s.sparseScorer.Score(query, c.Text)})
	}
	// Sort BM25 hits by TF score descending to assign ranks.
	sort.Slice(bm25Hits, func(i, j int) bool { return bm25Hits[i].score > bm25Hits[j].score })
	for rank, h := range bm25Hits {
		byID[h.key].bm25Rnk = rank + 1
	}

	// RRF fusion (k=60).
	const rrfK = 60.0
	type scored struct {
		key   string
		score float64
	}
	var fused []scored
	for k, e := range byID {
		var s float64
		if e.denseRnk > 0 {
			s += 1.0 / (rrfK + float64(e.denseRnk))
		}
		if e.bm25Rnk > 0 {
			s += 1.0 / (rrfK + float64(e.bm25Rnk))
		}
		fused = append(fused, scored{key: k, score: s})
	}
	// Sort by RRF score descending.
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
