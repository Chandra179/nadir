package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

const (
	RouteSearch = "/search"
	RouteIngest = "/ingest"
	RouteHealth = "/healthz"
)

// NewRouter registers Search, Ingest, and Healthz on engine. Global
// middleware (recovery, request ID, request log, metrics) is expected to
// already be attached to engine via engine.Use(...) before this is called.
func NewRouter(engine *gin.Engine, deps *dependencies) *gin.Engine {
	engine.POST(RouteSearch, deps.Search)
	engine.POST(RouteIngest, deps.Ingest)
	engine.GET(RouteHealth, func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	return engine
}
