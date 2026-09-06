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

// feedbackView renders the "feedback" partial: an error message, or the
// success note when empty.
type feedbackView struct {
	Error string
}

// DeleteAllData permanently removes every indexed chunk by delegating to
// the store's DeleteAll (drops and recreates the collection, picking up any
// schema drift). Cache invalidation on reset is enforced by the store
// decorator wired in internal/server.
func (d *dependencies) DeleteAllData(c *gin.Context) {
	isHX := c.GetHeader("HX-Request") == "true"

	if err := d.store.DeleteAll(c.Request.Context()); err != nil {
		d.log.Error("delete all data failed", zap.Error(err))
		msg := fmt.Sprintf("delete failed: %s", err.Error())
		if isHX {
			d.render.HTML(c, http.StatusInternalServerError, "feedback", feedbackView{Error: msg})
			return
		}
		c.JSON(http.StatusInternalServerError, deleteAllResponse{Error: msg})
		return
	}

	if isHX {
		d.render.HTML(c, http.StatusOK, "feedback", feedbackView{})
		return
	}
	c.JSON(http.StatusOK, deleteAllResponse{Deleted: true})
}
