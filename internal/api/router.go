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
	RouteTurnEvents      = "/retrieval/turns/:id/events"
	RouteTurnCancel      = "/retrieval/turns/:id/cancel"
	RouteHistorySessions = "/history/sessions"
	RouteHistorySession  = "/history/sessions/:id"
	RouteHealth          = "/healthz"
)

// NewRouter registers the API endpoints on engine. Global middleware
// (recovery, request ID, timeout, request log) is expected to already be
// attached to engine via engine.Use(...) before this is called. Handlers
// live in the feature transports (turns, history) and on the page shell.
func NewRouter(engine *gin.Engine, deps *dependencies) *gin.Engine {
	engine.POST(RouteIngest, deps.Ingest)
	engine.POST(RouteStoreReset, deps.DeleteAllData)
	engine.GET(RouteRetrieval, deps.Retrieval)
	engine.POST(RouteRetrievalSearch, deps.turns.RetrievalSearch)
	engine.GET(RouteTurnEvents, deps.turns.RetrievalAnswer)
	engine.POST(RouteTurnCancel, deps.turns.RetrievalTurnCancel)
	engine.GET(RouteHistorySessions, deps.hist.HistorySessions)
	engine.GET(RouteHistorySession, deps.HistorySession)
	engine.DELETE(RouteHistorySession, deps.hist.HistorySessionDelete)
	engine.GET(RouteHealth, func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	return engine
}
