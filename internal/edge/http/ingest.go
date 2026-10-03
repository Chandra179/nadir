package api

import (
	"errors"
	"fmt"
	"io"
	"nadir/internal/edge/http/respond"
	"net/http"

	"log/slog"

	config "nadir/internal/bootstrap/configuration"
	"nadir/internal/core/documents/indexing"
)

type ingestResponse struct {
	OperationID string                `json:"operation_id,omitempty"`
	Processed   int                   `json:"processed"`
	Skipped     int                   `json:"skipped"`
	Failed      int                   `json:"failed"`
	Removed     int                   `json:"removed"`
	Names       []string              `json:"names,omitempty"`
	Files       []indexing.FileResult `json:"files,omitempty"`
	Error       string                `json:"error,omitempty"`
}

// Ingest accepts multipart/form-data uploads (field "files") — the chat
// UI's file picker or curl -F. Files are chunked, embedded, upserted;
// SHA-256 duplicates are skipped. The endpoint always returns JSON.
func (d *dependencies) Ingest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if d.maxUploadBytes > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, d.maxUploadBytes)
	}

	err := r.ParseMultipartForm(32 << 20)
	form := r.MultipartForm
	if form != nil {
		defer func() { _ = form.RemoveAll() }()
	}
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			d.respondIngestError(w, http.StatusRequestEntityTooLarge, "upload exceeds the configured size limit")
			return
		}
		if len(d.documentsPaths) == 0 {
			d.respondIngestError(w, http.StatusBadRequest, "expected multipart/form-data with a \"files\" field")
			return
		}
	}

	var files []indexing.UploadFile
	var names []string
	if form != nil {
		headers := form.File["files"]
		if len(headers) > 0 {
			names = make([]string, 0, len(headers))
			files = make([]indexing.UploadFile, 0, len(headers))
			for _, fh := range headers {
				names = append(names, fh.Filename)
				f, err := fh.Open()
				if err != nil {
					d.respondIngestError(w, http.StatusBadRequest, fmt.Sprintf("open %s: %v", fh.Filename, err))
					return
				}
				data, err := io.ReadAll(f)
				_ = f.Close()
				if err != nil {
					d.respondIngestError(w, http.StatusBadRequest, fmt.Sprintf("read %s: %v", fh.Filename, err))
					return
				}
				files = append(files, indexing.UploadFile{Name: fh.Filename, Data: data})
			}
		}
	}
	fromConfiguredSources := len(files) == 0
	if len(files) == 0 {
		if len(d.documentsPaths) == 0 {
			d.respondIngestError(w, http.StatusBadRequest, "no files provided")
			return
		}
		var err error
		files, err = indexing.DiscoverFiles(d.documentsPaths, d.documentsIgnorePatterns, d.maxDocumentFileBytes)
		if err != nil {
			d.respondIngestError(w, http.StatusBadRequest, err.Error())
			return
		}
		if len(files) == 0 && d.documentsMode != config.DocumentsModeMirror {
			d.respondIngestError(w, http.StatusBadRequest, "no supported documents found in configured sources")
			return
		}
		names = make([]string, len(files))
		for i := range files {
			names[i] = files[i].Name
		}
	}

	options := indexing.RunOptions{}
	if fromConfiguredSources && d.documentsMode == config.DocumentsModeMirror {
		options = indexing.RunOptions{MirrorSources: true, SourceRoots: d.documentsPaths}
	}
	result, err := d.ingest.Run(ctx, files, options)
	d.recordImport(result, err)
	if err != nil {
		d.log.Error("ingest run failed", slog.Int("files", len(files)), slog.Any("error", err))
		d.respondIngestError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if result.Files != nil {
		names = importedNames(result.Files)
	}
	respond.JSON(w, http.StatusOK, ingestResponse{
		OperationID: result.OperationID,
		Processed:   result.Processed,
		Skipped:     result.Skipped,
		Failed:      result.Failed,
		Removed:     result.Removed,
		Names:       names,
		Files:       result.Files,
	})
}

func (d *dependencies) respondIngestError(w http.ResponseWriter, status int, msg string) {
	respond.JSON(w, status, ingestResponse{Error: msg})
}
