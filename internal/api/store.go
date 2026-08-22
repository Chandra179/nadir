package api

import (
	"fmt"
	"html"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type deleteAllResponse struct {
	Deleted bool   `json:"deleted"`
	Error   string `json:"error,omitempty"`
}

// DeleteAllData permanently removes every indexed chunk from the store's
// collection (leaving the collection and its indexes intact) and, if a
// semantic cache is configured, clears it too so it can't keep serving
// results for content that no longer exists.
func (d *dependencies) DeleteAllData(c *gin.Context) {
	ctx := c.Request.Context()
	isHX := c.GetHeader("HX-Request") == "true"

	if err := d.store.DeleteAll(ctx); err != nil {
		d.log.Error("delete all data failed", zap.Error(err))
		d.respondDeleteAllError(c, isHX, err)
		return
	}

	if d.cache != nil {
		if err := d.cache.Clear(ctx); err != nil {
			d.log.Warn("clear semantic cache after delete-all failed", zap.Error(err))
		}
	}

	if isHX {
		// Reuse the same trigger name POST /ingest fires: the dashboard's
		// stats/live-run/history panels already listen for it.
		c.Header("HX-Trigger", "ingest-done")
		c.String(http.StatusOK, `<div class="feedback feedback-ok">All indexed data deleted.</div>`)
		return
	}
	c.JSON(http.StatusOK, deleteAllResponse{Deleted: true})
}

func (d *dependencies) respondDeleteAllError(c *gin.Context, isHX bool, err error) {
	msg := fmt.Sprintf("delete failed: %s", err.Error())
	if isHX {
		c.Header("HX-Trigger", "ingest-done")
		c.String(http.StatusInternalServerError, fmt.Sprintf(`<div class="feedback feedback-err">%s</div>`, html.EscapeString(msg)))
		return
	}
	c.JSON(http.StatusInternalServerError, deleteAllResponse{Error: msg})
}
