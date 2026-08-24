package api

import (
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nadir/internal/ingest"
)

type ingestResponse struct {
	Processed int    `json:"processed"`
	Skipped   int    `json:"skipped"`
	Failed    int    `json:"failed"`
	Error     string `json:"error,omitempty"`
}

// Ingest accepts multipart/form-data uploads (field name "files", one or
// more) — the chat UI's file picker, or curl -F. Each file is chunked,
// embedded, and upserted; files whose content SHA-256 matches what's
// already stored are skipped. On an htmx request it responds with the
// attachment-chip fragment and sets HX-Trigger so live panels refresh
// immediately instead of waiting for their next poll.
func (d *dependencies) Ingest(c *gin.Context) {
	ctx := c.Request.Context()
	isHX := c.GetHeader("HX-Request") == "true"

	form, err := c.MultipartForm()
	if err != nil {
		d.respondIngestError(c, isHX, http.StatusBadRequest, "expected multipart/form-data with a \"files\" field")
		return
	}
	headers := form.File["files"]
	if len(headers) == 0 {
		d.respondIngestError(c, isHX, http.StatusBadRequest, "no files provided")
		return
	}

	names := make([]string, 0, len(headers))
	for _, fh := range headers {
		names = append(names, fh.Filename)
	}

	files := make([]ingest.UploadFile, 0, len(headers))
	for _, fh := range headers {
		f, err := fh.Open()
		if err != nil {
			d.respondIngestError(c, isHX, http.StatusBadRequest, fmt.Sprintf("open %s: %v", fh.Filename, err))
			return
		}
		data, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			d.respondIngestError(c, isHX, http.StatusBadRequest, fmt.Sprintf("read %s: %v", fh.Filename, err))
			return
		}
		files = append(files, ingest.UploadFile{Name: fh.Filename, Data: data})
	}

	result, err := d.ingest.Run(ctx, files)
	if err != nil {
		d.log.Error("ingest run failed", zap.Int("files", len(files)), zap.Error(err))
		d.respondIngestError(c, isHX, http.StatusInternalServerError, err.Error())
		return
	}

	if isHX {
		c.Header("HX-Trigger", "ingest-done")
		d.renderHTML(c, http.StatusOK, "chips-ok", names)
		return
	}
	c.JSON(http.StatusOK, ingestResponse{
		Processed: result.Processed,
		Skipped:   result.Skipped,
		Failed:    result.Failed,
	})
}

func (d *dependencies) respondIngestError(c *gin.Context, isHX bool, status int, msg string) {
	if isHX {
		c.Header("HX-Trigger", "ingest-done")
		d.renderHTML(c, status, "chips-error", struct{ Message string }{msg})
		return
	}
	c.JSON(status, ingestResponse{Error: msg})
}
