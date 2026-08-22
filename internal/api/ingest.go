package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type ingestResponse struct {
	Processed int    `json:"processed"`
	Skipped   int    `json:"skipped"`
	Failed    int    `json:"failed"`
	Error     string `json:"error,omitempty"`
}

func (d *dependencies) Ingest(c *gin.Context) {
	ctx := c.Request.Context()

	result, err := d.ingest.Run(ctx)
	if err != nil {
		d.log.Error("ingest run failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, ingestResponse{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, ingestResponse{
		Processed: result.Processed,
		Skipped:   result.Skipped,
		Failed:    result.Failed,
	})
}
