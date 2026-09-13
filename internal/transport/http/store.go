package api

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type deleteAllResponse struct {
	Deleted bool   `json:"deleted"`
	Error   string `json:"error,omitempty"`
}

// DeleteAllData permanently removes every indexed chunk by delegating to the
// store's DeleteAll, which publishes an empty collection generation and then
// retires the previous one. Cache invalidation on reset is enforced by the
// reset coordinator wired in internal/knowledge/indexing.
func (d *dependencies) DeleteAllData(c *gin.Context) {
	if d.reset == nil {
		d.log.Error("delete all data unavailable")
		c.JSON(http.StatusServiceUnavailable, deleteAllResponse{Error: "delete is unavailable"})
		return
	}
	if err := d.reset(c.Request.Context()); err != nil {
		d.log.Error("delete all data failed", zap.Error(err))
		msg := fmt.Sprintf("delete failed: %s", err.Error())
		c.JSON(http.StatusInternalServerError, deleteAllResponse{Error: msg})
		return
	}

	c.JSON(http.StatusOK, deleteAllResponse{Deleted: true})
}
