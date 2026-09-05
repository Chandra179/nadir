package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

const (
	RouteIngest          = "/ingest"
	RouteStoreReset      = "/store/reset"
	RouteRetrieval       = "/retrieval"
	RouteRetrievalSearch = "/retrieval/search"
	RouteSettings        = "/settings"
	RouteHistorySessions = "/history/sessions"
	RouteHistorySession  = "/history/sessions/:id"
	RouteHealth          = "/healthz"
)

// NewRouter registers the API endpoints on engine. Global middleware
// (recovery, request ID, timeout, request log) is expected to already be
// attached to engine via engine.Use(...) before this is called.
func NewRouter(engine *gin.Engine, deps *dependencies) *gin.Engine {
	engine.POST(RouteIngest, deps.Ingest)
	engine.POST(RouteStoreReset, deps.DeleteAllData)
	engine.GET(RouteRetrieval, deps.Retrieval)
	engine.POST(RouteRetrievalSearch, deps.RetrievalSearch)
	engine.GET(RouteSettings, deps.Settings)
	engine.GET(RouteHistorySessions, deps.HistorySessions)
	engine.GET(RouteHistorySession, deps.HistorySession)
	engine.DELETE(RouteHistorySession, deps.HistorySessionDelete)
	engine.GET(RouteHealth, func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	return engine
}
