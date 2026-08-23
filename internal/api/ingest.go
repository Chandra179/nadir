package api

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"

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
// already stored are skipped. On an htmx request it responds with a small
// HTML feedback fragment and sets HX-Trigger so live panels refresh
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
		c.String(http.StatusOK, ingestFeedbackHTML(true, names, fmt.Sprintf(
			"processed %d, skipped %d, failed %d", result.Processed, result.Skipped, result.Failed,
		)))
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
		c.String(status, ingestFeedbackHTML(false, nil, msg))
		return
	}
	c.JSON(status, ingestResponse{Error: msg})
}

// ingestFeedbackHTML renders one dismissible attachment chip per uploaded
// file into the composer (above the input), so an import is visible right
// where the file was added. On failure it renders a single error chip
// instead, since a request-level failure has no per-file names to attach to.
func ingestFeedbackHTML(ok bool, names []string, msg string) string {
	if !ok {
		escaped := html.EscapeString(msg)
		return fmt.Sprintf(`<div class="relative flex items-center gap-2.5 w-[196px] flex-none bg-[#f7e6e3] border border-[#e3c4bd] rounded-[14px] pl-2.5 pr-7 py-2" data-chip>
  <div class="w-8 h-8 rounded-[9px] bg-[#f3d2cc] text-[#b04a3f] flex items-center justify-center flex-none">%s</div>
  <div class="min-w-0"><div class="text-[12px] font-semibold text-[#8a3a30] truncate">Import failed</div><div class="text-[10.5px] text-[#b04a3f] truncate">%s</div></div>
  %s
</div>`, docIconSVG, escaped, dismissChipButton)
	}

	var b strings.Builder
	for _, name := range names {
		escapedName := html.EscapeString(name)
		fmt.Fprintf(&b, `<div class="relative flex items-center gap-2.5 w-[196px] flex-none bg-[#eeece3] border border-[#e3e2d8] rounded-[14px] pl-2.5 pr-7 py-2" data-chip data-filename="%s">
  <div class="w-8 h-8 rounded-[9px] bg-[#dce8fb] text-[#2f5db0] flex items-center justify-center flex-none">%s</div>
  <div class="min-w-0"><div class="text-[12px] font-semibold text-[#20241f] truncate">%s</div><div class="text-[10.5px] text-[#8b8f81]">File</div></div>
  %s
</div>`, escapedName, docIconSVG, escapedName, dismissChipButton)
	}
	return b.String()
}

const docIconSVG = `<svg width="16" height="16" viewBox="0 0 24 24" fill="none"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" stroke="currentColor" stroke-width="1.8"/><path d="M14 2v6h6M8 13h8M8 17h8" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"/></svg>`

const dismissChipButton = `<button type="button" onclick="this.closest('[data-chip]').remove()" aria-label="Dismiss" class="absolute top-1 right-1 w-4 h-4 rounded-full bg-[#20241f] text-white flex items-center justify-center text-[9px] leading-none hover:bg-black">✕</button>`
