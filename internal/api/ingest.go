package api

import (
	"fmt"
	"html"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type ingestRequest struct {
	// Path scopes the run to a single .md file or a directory, absolute or
	// relative to a configured source.paths root. Empty runs the full
	// configured sweep.
	Path string `json:"path" form:"path"`
}

type ingestResponse struct {
	Processed int    `json:"processed"`
	Skipped   int    `json:"skipped"`
	Failed    int    `json:"failed"`
	Error     string `json:"error,omitempty"`
}

// Ingest handles both the plain JSON API (curl, cmd/eval-adjacent tooling)
// and the dashboard's htmx form post (application/x-www-form-urlencoded).
// On an htmx request it responds with a small HTML feedback fragment and
// sets HX-Trigger so the dashboard's live panels refresh immediately
// instead of waiting for their next poll.
func (d *dependencies) Ingest(c *gin.Context) {
	ctx := c.Request.Context()
	isHX := c.GetHeader("HX-Request") == "true"

	var req ingestRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBind(&req); err != nil {
			d.respondIngestError(c, isHX, http.StatusBadRequest, "bad request")
			return
		}
	}

	result, err := d.ingest.Run(ctx, req.Path)
	if err != nil {
		d.log.Error("ingest run failed", zap.String("path", req.Path), zap.Error(err))
		d.respondIngestError(c, isHX, http.StatusInternalServerError, err.Error())
		return
	}

	if isHX {
		c.Header("HX-Trigger", "ingest-done")
		c.String(http.StatusOK, ingestFeedbackHTML(true, fmt.Sprintf(
			"Processed %d, skipped %d, failed %d.", result.Processed, result.Skipped, result.Failed,
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
		c.String(status, ingestFeedbackHTML(false, msg))
		return
	}
	c.JSON(status, ingestResponse{Error: msg})
}

func ingestFeedbackHTML(ok bool, msg string) string {
	escaped := html.EscapeString(msg)
	if ok {
		return fmt.Sprintf(`<div class="feedback feedback-ok">%s</div>`, escaped)
	}
	return fmt.Sprintf(`<div class="feedback feedback-err">%s</div>`, escaped)
}
