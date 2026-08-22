package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

const (
	RouteSearch        = "/search"
	RouteIngest        = "/ingest"
	RouteIngestStatus  = "/ingest/status"
	RouteIngestHistory = "/ingest/history"
	RouteStats         = "/stats"
	RouteDashboard     = "/dashboard"
	RouteHealth        = "/healthz"
	RouteStoreReset    = "/store/reset"
)

// NewRouter registers Search, Ingest, the dashboard, and Healthz on engine.
// Global middleware (recovery, request ID, request log, metrics) is
// expected to already be attached to engine via engine.Use(...) before
// this is called.
func NewRouter(engine *gin.Engine, deps *dependencies) *gin.Engine {
	engine.POST(RouteSearch, deps.Search)
	engine.POST(RouteIngest, deps.Ingest)
	engine.POST(RouteStoreReset, deps.DeleteAllData)
	engine.GET(RouteIngestStatus, deps.IngestStatus)
	engine.GET(RouteIngestHistory, deps.IngestHistory)
	engine.GET(RouteStats, deps.Stats)
	engine.GET(RouteDashboard, deps.Dashboard)
	engine.GET(RouteHealth, func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	return engine
}
