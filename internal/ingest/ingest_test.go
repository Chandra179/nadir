package ingest

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
	"nadir/internal/chunker"
	"nadir/internal/embedder"
	"nadir/internal/enrichment"
	"nadir/internal/store"
)

type fakeIntake struct {
	called int
	data   []byte
}

func (f *fakeIntake) Convert(_ context.Context, name string, data []byte) ([]byte, error) {
	f.called++
	f.data = append([]byte(nil), data...)
	return []byte("# converted " + name), nil
}

type fakeChunker struct {
	text string
}

func (f *fakeChunker) Chunk(text, filePath string) ([]chunker.Chunk, error) {
	f.text = text
	return []chunker.Chunk{{Text: text, FilePath: filePath, LineStart: 1, ChunkIndex: 0}}, nil
}

func (f *fakeChunker) ContextualText(c chunker.Chunk) string { return c.FilePath + "\n" + c.Text }

type fakeEmbedder struct{}

func (fakeEmbedder) Embed(context.Context, string) ([]float32, error) { return []float32{1, 2}, nil }
func (fakeEmbedder) Dimensions() int                                  { return 2 }

type fakeStore struct {
	replacedPath string
	replacedSHA  string
	replaced     []store.ScoredChunk
}

func (f *fakeStore) ReplaceDocument(_ context.Context, filePath, sourceSHA string, chunks []store.ScoredChunk) error {
	f.replacedPath = filePath
	f.replacedSHA = sourceSHA
	f.replaced = append(f.replaced, chunks...)
	return nil
}
func (f *fakeStore) DeleteAll(context.Context) error { return nil }
func (f *fakeStore) HybridSearch(context.Context, []float32, string, int, *store.SearchFilter) ([]store.ScoredChunk, error) {
	return nil, nil
}
func (f *fakeStore) KeywordSearch(context.Context, string, int, *store.SearchFilter) ([]store.ScoredChunk, error) {
	return nil, nil
}
func (f *fakeStore) GetAllFileSHAs(context.Context) (map[string]string, error) {
	return map[string]string{}, nil
}
func (f *fakeStore) Stats(context.Context) (store.Stats, error) { return store.Stats{}, nil }

var _ embedder.Embedder = fakeEmbedder{}
var _ store.Store = (*fakeStore)(nil)

type fakeEnricher struct {
	hypeCalls       int
	contextualCalls int
}

func (f *fakeEnricher) HypotheticalQuestions(context.Context, string, string, int) ([]string, error) {
	f.hypeCalls++
	return []string{"what does this document explain?"}, nil
}

func (f *fakeEnricher) ContextualIntro(context.Context, string, string) (string, error) {
	f.contextualCalls++
	return "This is contextual background.", nil
}

var _ enrichment.Enricher = (*fakeEnricher)(nil)

func TestRunUsesDocumentIntakeBeforeIndexingPDF(t *testing.T) {
	intake := &fakeIntake{}
	chunkerFake := &fakeChunker{}
	storeFake := &fakeStore{}
	d := NewDependencies(DependenciesConfig{
		Chunker:           chunkerFake,
		Embedder:          fakeEmbedder{},
		Store:             storeFake,
		DocumentConverter: intake,
		Retry:             RetryConfig{},
		Log:               zap.NewNop(),
	})

	result, err := d.Run(context.Background(), []UploadFile{{Name: "report.pdf", Data: []byte("pdf bytes")}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Processed != 1 || result.Failed != 0 || intake.called != 1 {
		t.Fatalf("result=%+v intake_calls=%d", result, intake.called)
	}
	if string(intake.data) != "pdf bytes" || chunkerFake.text != "# converted report.pdf" {
		t.Fatalf("document intake did not feed Markdown to indexing: data=%q text=%q", intake.data, chunkerFake.text)
	}
	if storeFake.replacedPath != "report.pdf" || storeFake.replacedSHA == "" || len(storeFake.replaced) != 1 || storeFake.replaced[0].FilePath != "report.pdf" {
		t.Fatalf("source identity was not preserved: path=%q sha=%q chunks=%+v", storeFake.replacedPath, storeFake.replacedSHA, storeFake.replaced)
	}
}

func TestEnrichmentFeatureFlagsAreIndependent(t *testing.T) {
	tests := []struct {
		name             string
		hypeEnabled      bool
		contextualEnable bool
		wantHypeCalls    int
		wantContextCalls int
		wantChunks       int
	}{
		{name: "both disabled", wantChunks: 1},
		{name: "hype only", hypeEnabled: true, wantHypeCalls: 1, wantChunks: 2},
		{name: "contextual only", contextualEnable: true, wantContextCalls: 1, wantChunks: 1},
		{name: "both enabled", hypeEnabled: true, contextualEnable: true, wantHypeCalls: 1, wantContextCalls: 1, wantChunks: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enricher := &fakeEnricher{}
			d := NewDependencies(DependenciesConfig{
				Chunker:           &fakeChunker{},
				Embedder:          fakeEmbedder{},
				Enricher:          enricher,
				HypeEnabled:       tt.hypeEnabled,
				HypeQuestions:     1,
				ContextualEnabled: tt.contextualEnable,
				Log:               zap.NewNop(),
			})

			plan, err := d.planFile(context.Background(), "notes.md", "body", "sha")
			if err != nil {
				t.Fatal(err)
			}
			if enricher.hypeCalls != tt.wantHypeCalls {
				t.Fatalf("HyPE calls = %d, want %d", enricher.hypeCalls, tt.wantHypeCalls)
			}
			if enricher.contextualCalls != tt.wantContextCalls {
				t.Fatalf("contextual calls = %d, want %d", enricher.contextualCalls, tt.wantContextCalls)
			}
			if len(plan.chunks) != tt.wantChunks {
				t.Fatalf("planned chunks = %d, want %d", len(plan.chunks), tt.wantChunks)
			}
		})
	}
}

type serialStore struct {
	active      atomic.Int32
	maxActive   atomic.Int32
	getCalls    atomic.Int32
	firstStart  chan struct{}
	secondStart chan struct{}
	release     chan struct{}
	firstOnce   sync.Once
	secondOnce  sync.Once
}

func (s *serialStore) ReplaceDocument(context.Context, string, string, []store.ScoredChunk) error {
	active := s.active.Add(1)
	for {
		max := s.maxActive.Load()
		if active <= max || s.maxActive.CompareAndSwap(max, active) {
			break
		}
	}
	s.firstOnce.Do(func() { close(s.firstStart) })
	<-s.release
	s.active.Add(-1)
	return nil
}

func (s *serialStore) DeleteAll(context.Context) error { return nil }
func (s *serialStore) HybridSearch(context.Context, []float32, string, int, *store.SearchFilter) ([]store.ScoredChunk, error) {
	return nil, nil
}
func (s *serialStore) KeywordSearch(context.Context, string, int, *store.SearchFilter) ([]store.ScoredChunk, error) {
	return nil, nil
}
func (s *serialStore) GetAllFileSHAs(context.Context) (map[string]string, error) {
	if s.getCalls.Add(1) == 2 {
		s.secondOnce.Do(func() { close(s.secondStart) })
	}
	return map[string]string{}, nil
}
func (s *serialStore) Stats(context.Context) (store.Stats, error) { return store.Stats{}, nil }

func TestRunSerializesOverlappingIndexingPasses(t *testing.T) {
	storeFake := &serialStore{
		firstStart:  make(chan struct{}),
		secondStart: make(chan struct{}),
		release:     make(chan struct{}),
	}
	d := NewDependencies(DependenciesConfig{
		Chunker:  &fakeChunker{},
		Embedder: fakeEmbedder{},
		Store:    storeFake,
		Workers:  1,
		Log:      zap.NewNop(),
	})

	firstDone := make(chan struct{})
	go func() {
		_, _ = d.Run(context.Background(), []UploadFile{{Name: "same.md", Data: []byte("first")}})
		close(firstDone)
	}()
	select {
	case <-storeFake.firstStart:
	case <-time.After(time.Second):
		t.Fatal("first indexing pass did not reach the store")
	}

	secondDone := make(chan struct{})
	go func() {
		_, _ = d.Run(context.Background(), []UploadFile{{Name: "same.md", Data: []byte("second")}})
		close(secondDone)
	}()
	select {
	case <-storeFake.secondStart:
		t.Fatal("second indexing pass entered while first pass was active")
	case <-time.After(50 * time.Millisecond):
	}

	close(storeFake.release)
	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("first indexing pass did not finish")
	}
	select {
	case <-secondDone:
	case <-time.After(time.Second):
		t.Fatal("second indexing pass did not finish after first pass")
	}
	if got := storeFake.maxActive.Load(); got != 1 {
		t.Fatalf("maximum concurrent replacements = %d, want 1", got)
	}
}
