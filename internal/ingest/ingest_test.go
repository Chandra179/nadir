package ingest

import (
	"context"
	"testing"

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
	deleted  string
	upserted []store.ScoredChunk
}

func (f *fakeStore) Upsert(_ context.Context, chunks []store.ScoredChunk) error {
	f.upserted = append(f.upserted, chunks...)
	return nil
}
func (f *fakeStore) DeleteByFile(_ context.Context, filePath string) error {
	f.deleted = filePath
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
	if storeFake.deleted != "report.pdf" || len(storeFake.upserted) != 1 || storeFake.upserted[0].FilePath != "report.pdf" {
		t.Fatalf("source identity was not preserved: deleted=%q chunks=%+v", storeFake.deleted, storeFake.upserted)
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
