package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nadir/internal/knowledge/indexing"
)

type ingestResponse struct {
	Processed int      `json:"processed"`
	Skipped   int      `json:"skipped"`
	Failed    int      `json:"failed"`
	Names     []string `json:"names,omitempty"`
	Error     string   `json:"error,omitempty"`
}

// Ingest accepts multipart/form-data uploads (field "files") — the chat
// UI's file picker or curl -F. Files are chunked, embedded, upserted;
// SHA-256 duplicates are skipped. The endpoint always returns JSON.
func (d *dependencies) Ingest(c *gin.Context) {
	ctx := c.Request.Context()
	if d.maxUploadBytes > 0 {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, d.maxUploadBytes)
	}

	form, err := c.MultipartForm()
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			d.respondIngestError(c, http.StatusRequestEntityTooLarge, "upload exceeds the configured size limit")
			return
		}
		if len(d.sourcePaths) == 0 {
			d.respondIngestError(c, http.StatusBadRequest, "expected multipart/form-data with a \"files\" field")
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
					d.respondIngestError(c, http.StatusBadRequest, fmt.Sprintf("open %s: %v", fh.Filename, err))
					return
				}
				data, err := io.ReadAll(f)
				f.Close()
				if err != nil {
					d.respondIngestError(c, http.StatusBadRequest, fmt.Sprintf("read %s: %v", fh.Filename, err))
					return
				}
				files = append(files, indexing.UploadFile{Name: fh.Filename, Data: data})
			}
		}
	}
	if len(files) == 0 {
		if len(d.sourcePaths) == 0 {
			d.respondIngestError(c, http.StatusBadRequest, "no files provided")
			return
		}
		var err error
		files, err = indexing.DiscoverFiles(d.sourcePaths, d.sourceIgnorePatterns, d.maxSourceFileBytes)
		if err != nil {
			d.respondIngestError(c, http.StatusBadRequest, err.Error())
			return
		}
		if len(files) == 0 {
			d.respondIngestError(c, http.StatusBadRequest, "no supported documents found in configured sources")
			return
		}
		names = make([]string, len(files))
		for i := range files {
			names[i] = files[i].Name
		}
	}

	result, err := d.ingest.Run(ctx, files)
	if err != nil {
		d.log.Error("ingest run failed", zap.Int("files", len(files)), zap.Error(err))
		d.respondIngestError(c, http.StatusInternalServerError, err.Error())
		return
	}

	c.JSON(http.StatusOK, ingestResponse{
		Processed: result.Processed,
		Skipped:   result.Skipped,
		Failed:    result.Failed,
		Names:     names,
	})
}

func (d *dependencies) respondIngestError(c *gin.Context, status int, msg string) {
	c.JSON(status, ingestResponse{Error: msg})
}
