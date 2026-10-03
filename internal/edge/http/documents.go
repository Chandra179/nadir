package api

import (
	"context"
	"net/http"
	"sort"
	"time"

	"nadir/internal/core/documents/indexing"
	"nadir/internal/edge/http/respond"
)

type documentVersion struct {
	FilePath  string `json:"file_path"`
	SourceSHA string `json:"source_sha"`
}

type importSummary struct {
	CompletedAt string         `json:"completed_at"`
	Result      ingestResponse `json:"result"`
}

type documentsResponse struct {
	Documents       []documentVersion `json:"documents"`
	Count           int               `json:"count"`
	LastImport      *importSummary    `json:"last_import,omitempty"`
	LastImportScope string            `json:"last_import_scope"`
}

func (d *dependencies) Documents(w http.ResponseWriter, r *http.Request) {
	if d.documentVersions == nil {
		respond.JSON(w, http.StatusServiceUnavailable, map[string]string{"error": "document inventory is unavailable"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), d.readinessTimeout)
	defer cancel()
	versions, err := d.documentVersions(ctx)
	if err != nil {
		respond.JSON(w, http.StatusServiceUnavailable, map[string]string{"error": "could not load document inventory"})
		return
	}
	documents := make([]documentVersion, 0, len(versions))
	for path, sha := range versions {
		documents = append(documents, documentVersion{FilePath: path, SourceSHA: sha})
	}
	sort.Slice(documents, func(i, j int) bool { return documents[i].FilePath < documents[j].FilePath })
	d.importMu.RLock()
	last := d.lastImport
	d.importMu.RUnlock()
	respond.JSON(w, http.StatusOK, documentsResponse{Documents: documents, Count: len(documents), LastImport: last, LastImportScope: "since_process_start"})
}

func (d *dependencies) recordImport(result indexing.Result, err error) {
	summary := &importSummary{CompletedAt: time.Now().UTC().Format(time.RFC3339), Result: ingestResponse{
		OperationID: result.OperationID, Processed: result.Processed, Skipped: result.Skipped,
		Failed: result.Failed, Removed: result.Removed, Files: result.Files,
	}}
	if err != nil {
		summary.Result.Error = err.Error()
	}
	d.importMu.Lock()
	d.lastImport = summary
	d.importMu.Unlock()
}

// A failed replacement must not appear attached as a successful import.
// Duplicate names in one request can otherwise make a failed file look skipped.
func importedNames(files []indexing.FileResult) []string {
	failed := make(map[string]bool)
	for _, file := range files {
		if file.Status == "failed" && !file.Published {
			failed[file.Name] = true
		}
	}
	names := make([]string, 0, len(files))
	seen := make(map[string]bool)
	for _, file := range files {
		if !seen[file.Name] && !failed[file.Name] && (file.Status == "processed" || file.Status == "skipped" || file.Published) {
			names = append(names, file.Name)
			seen[file.Name] = true
		}
	}
	return names
}
