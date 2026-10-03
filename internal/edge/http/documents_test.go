package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"nadir/internal/core/documents/indexing"
)

type partialIngest struct{}

func (partialIngest) Run(context.Context, []indexing.UploadFile, indexing.RunOptions) (indexing.Result, error) {
	return indexing.Result{Processed: 1, Failed: 1, Files: []indexing.FileResult{
		{Name: "good.md", Status: "processed"}, {Name: "bad.pdf", Status: "failed", Error: "PDF intake is disabled"},
	}}, nil
}

func TestDocumentsExposeActiveSourcesAndPartialImportOutcomes(t *testing.T) {
	versions := map[string]string{"z.md": "sha-z", "good.md": "sha-good"}
	d := NewDependencies(DependenciesConfig{Ingest: partialIngest{}, DocumentVersions: func(context.Context) (map[string]string, error) { return versions, nil }})
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for _, name := range []string{"good.md", "bad.pdf"} {
		part, _ := form.CreateFormFile("files", name)
		_, _ = part.Write([]byte("source"))
	}
	_ = form.Close()
	req := httptest.NewRequest(http.MethodPost, RouteDocuments, &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	recorder := httptest.NewRecorder()
	NewRouter(http.NewServeMux(), d).ServeHTTP(recorder, req)
	var imported ingestResponse
	_ = json.Unmarshal(recorder.Body.Bytes(), &imported)
	if recorder.Code != 200 || !reflect.DeepEqual(imported.Names, []string{"good.md"}) || len(imported.Files) != 2 || imported.Files[1].Error == "" {
		t.Fatalf("failed source appeared imported or failure reason was lost: %d %s", recorder.Code, recorder.Body.String())
	}
	recorder = httptest.NewRecorder()
	NewRouter(http.NewServeMux(), d).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, RouteDocuments, nil))
	var status documentsResponse
	_ = json.Unmarshal(recorder.Body.Bytes(), &status)
	if status.Count != 2 || status.Documents[0].FilePath != "good.md" || status.Documents[0].SourceSHA != "sha-good" || status.LastImport == nil || status.LastImport.Result.Failed != 1 || status.LastImportScope != "since_process_start" {
		t.Fatalf("inventory or last import is incorrect: %s", recorder.Body.String())
	}
	// Inventory comes from persistence; the recent import summary deliberately
	// expires with the API process and must not masquerade as durable history.
	fresh := NewDependencies(DependenciesConfig{DocumentVersions: d.documentVersions})
	recorder = httptest.NewRecorder()
	fresh.Documents(recorder, httptest.NewRequest(http.MethodGet, RouteDocuments, nil))
	_ = json.Unmarshal(recorder.Body.Bytes(), &status)
	if bytes.Contains(recorder.Body.Bytes(), []byte("last_import\"")) || status.Count != 2 {
		t.Fatalf("restart inventory: %s", recorder.Body.String())
	}
}

func TestDocumentsDependencyFailureIsVisible(t *testing.T) {
	d := NewDependencies(DependenciesConfig{DocumentVersions: func(context.Context) (map[string]string, error) { return nil, errors.New("offline") }})
	recorder := httptest.NewRecorder()
	d.Documents(recorder, httptest.NewRequest(http.MethodGet, RouteDocuments, nil))
	if recorder.Code != 503 {
		t.Fatalf("inventory failure hidden: %d", recorder.Code)
	}
}

func TestFailedDuplicateCannotAppearImported(t *testing.T) {
	got := importedNames([]indexing.FileResult{{Name: "bad.pdf", Status: "failed"}, {Name: "bad.pdf", Status: "skipped"}, {Name: "visible.md", Status: "failed", Published: true}})
	if !reflect.DeepEqual(got, []string{"visible.md"}) {
		t.Fatalf("import names=%v", got)
	}
}
