package ingest

import (
	"context"
	"testing"

	"go.uber.org/zap"
	"nadir/internal/chunker"
	"nadir/internal/embedder"
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

func TestRunUsesDocumentIntakeBeforeIndexingPDF(t *testing.T) {
	intake := &fakeIntake{}
	chunkerFake := &fakeChunker{}
	storeFake := &fakeStore{}
	d := NewDependencies(DependenciesConfig{
		Chunker:  chunkerFake,
		Embedder: fakeEmbedder{},
		Store:    storeFake,
		Retry:    RetryConfig{},
		Log:      zap.NewNop(),
	}).WithDocumentConverter(intake)

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
