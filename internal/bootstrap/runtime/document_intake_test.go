package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"

	config "nadir/internal/bootstrap/configuration"
	"nadir/internal/core/documents/indexing"
)

type intakeStore struct{}

func (intakeStore) GetAllFileSHAs(context.Context) (map[string]string, error) {
	return map[string]string{}, nil
}
func (intakeStore) ReplaceDocument(context.Context, string, string, []indexing.IndexedChunk) error {
	return errors.New("unexpected publication")
}
func (intakeStore) DeleteDocument(context.Context, string) error {
	return errors.New("unexpected deletion")
}

func TestDisabledDoclingSurvivesPDFImportAtRuntimeCompositionSeam(t *testing.T) {
	converter := documentIntake(config.DoclingConfig{Enabled: false}, nil)
	if converter != nil {
		t.Fatal("disabled Docling became a non-nil interface; a PDF upload would panic in its worker")
	}
	ingest := indexing.NewDependencies(indexing.DependenciesConfig{Store: intakeStore{}, DocumentConverter: converter})
	result, err := ingest.Run(context.Background(), []indexing.UploadFile{{Name: "disabled.pdf", Data: []byte("pdf")}}, indexing.RunOptions{})
	if err != nil || result.Failed != 1 || result.Processed != 0 || len(result.Files) != 1 || !strings.Contains(result.Files[0].Error, "PDF intake is disabled") {
		t.Fatalf("disabled PDF must return a per-file failure: %+v err=%v", result, err)
	}
}
