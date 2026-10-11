package assembly_test

import (
	"context"
	"hash/fnv"
	"math"
	"sort"
	"strings"
	"sync"
	"testing"

	"nadir/internal/bootstrap/assembly"
	config "nadir/internal/bootstrap/configuration"
	"nadir/internal/core/documents/indexing"
	"nadir/internal/core/retrieval/search"
)

const fakeDims = 512

// bagEmbedder hashes lowercase words into a fixed-size unit vector, so texts
// that share words are close without any model.
type bagEmbedder struct{}

func (bagEmbedder) Dimensions() int { return fakeDims }

func (bagEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	vector := make([]float32, fakeDims)
	for _, word := range strings.Fields(strings.ToLower(text)) {
		h := fnv.New32a()
		_, _ = h.Write([]byte(strings.Trim(word, ".,:;!?#*-")))
		vector[h.Sum32()%fakeDims]++
	}
	var norm float64
	for _, v := range vector {
		norm += float64(v * v)
	}
	if norm > 0 {
		scale := float32(1 / math.Sqrt(norm))
		for i := range vector {
			vector[i] *= scale
		}
	}
	return vector, nil
}

// memoryStore is the smallest Store that honors versioned replacement.
type memoryStore struct {
	mu     sync.Mutex
	chunks map[string][]indexing.IndexedChunk
}

func (s *memoryStore) EnsureCollection(context.Context, int) error { return nil }

func (s *memoryStore) GetAllFileSHAs(context.Context) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	shas := make(map[string]string, len(s.chunks))
	for path, chunks := range s.chunks {
		shas[path] = chunks[0].SourceSHA
	}
	return shas, nil
}

func (s *memoryStore) ReplaceDocument(_ context.Context, path, _ string, chunks []indexing.IndexedChunk) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.chunks == nil {
		s.chunks = map[string][]indexing.IndexedChunk{}
	}
	s.chunks[path] = chunks
	return nil
}

func (s *memoryStore) DeleteDocument(_ context.Context, path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.chunks, path)
	return nil
}

func (s *memoryStore) DeleteAll(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chunks = nil
	return nil
}

func (s *memoryStore) HybridSearch(_ context.Context, vector []float32, _ string, topK int, _ *search.Filter) (search.HybridSearchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ranked []search.SearchCandidate
	for _, chunks := range s.chunks {
		for _, c := range chunks {
			var dot float32
			for i := range vector {
				dot += vector[i] * c.Vector[i]
			}
			ranked = append(ranked, search.SearchCandidate{
				Text: c.Text, WindowText: c.WindowText, FilePath: c.FilePath, Header: c.Header,
				SectionPath: c.SectionPath, LineStart: c.LineStart, ChunkIndex: c.ChunkIndex,
				SourceSHA: c.SourceSHA, Score: dot,
			})
		}
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].Score > ranked[j].Score })
	if len(ranked) > topK {
		ranked = ranked[:topK]
	}
	return search.HybridSearchResult{Fused: ranked, Dense: ranked}, nil
}

func (s *memoryStore) KeywordSearch(context.Context, string, int, *search.Filter) ([]search.SearchCandidate, error) {
	return nil, nil
}

func TestAssembleIngestsAndRetrievesThroughInjectedProviders(t *testing.T) {
	cfg, err := config.Load("../configuration/config.yaml")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.SemanticCache.Enabled = false
	cfg.Reranker.Enabled = false
	cfg.Embedder.QueryPrefix, cfg.Embedder.DocumentPrefix = "", ""

	graph, err := assembly.Assemble(context.Background(), cfg, nil, assembly.Providers{
		Store:    &memoryStore{},
		Embedder: bagEmbedder{},
	})
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	t.Cleanup(func() { _ = graph.Close() })

	files := []indexing.UploadFile{
		{Name: "pets.md", Data: []byte("# Pets\n\nCats purr and sleep all day in the sun.\n")},
		{Name: "space.md", Data: []byte("# Space\n\nRockets burn fuel to reach orbit around the planet.\n")},
	}
	result, err := graph.Ingest.Run(context.Background(), files, indexing.RunOptions{})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if result.Processed != 2 || result.Failed != 0 {
		t.Fatalf("ingest result = %+v, want 2 processed and 0 failed", result)
	}

	got, err := graph.Searcher.Query(context.Background(), search.Request{Query: "rockets orbit planet", TopK: 1})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(got.Chunks) != 1 || got.Chunks[0].FilePath != "space.md" {
		t.Fatalf("top chunk = %+v, want one chunk from space.md", got.Chunks)
	}

	versions, err := graph.DocumentVersions(context.Background())
	if err != nil || len(versions) != 2 {
		t.Fatalf("document versions = %v, %v; want 2 files", versions, err)
	}
	if err := graph.Reset(context.Background()); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if versions, _ = graph.DocumentVersions(context.Background()); len(versions) != 0 {
		t.Fatalf("versions after reset = %v, want none", versions)
	}
}

func TestAssembleRequiresStoreAndEmbedder(t *testing.T) {
	cfg, err := config.Load("../configuration/config.yaml")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if _, err := assembly.Assemble(context.Background(), cfg, nil, assembly.Providers{}); err == nil {
		t.Fatal("Assemble without providers succeeded, want an error")
	}
}
