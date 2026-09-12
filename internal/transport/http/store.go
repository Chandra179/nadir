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
// store decorator wired in internal/platform/lifecycle.
func (d *dependencies) DeleteAllData(c *gin.Context) {
	if err := d.store.DeleteAll(c.Request.Context()); err != nil {
		d.log.Error("delete all data failed", zap.Error(err))
		msg := fmt.Sprintf("delete failed: %s", err.Error())
		c.JSON(http.StatusInternalServerError, deleteAllResponse{Error: msg})
		return
	}

	c.JSON(http.StatusOK, deleteAllResponse{Deleted: true})
}
