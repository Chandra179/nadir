package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

const (
	RouteDocuments      = "/api/v1/documents"
	RouteDocumentsReset = "/api/v1/documents/reset"
	RouteTurns          = "/api/v1/turns"
	RouteTurnEvents     = "/api/v1/turns/:id/events"
	RouteTurnCancel     = "/api/v1/turns/:id/cancel"
	RouteSessions       = "/api/v1/sessions"
	RouteSession        = "/api/v1/sessions/:id"
	RouteHealth         = "/api/v1/health"
	RouteReady          = "/api/v1/ready"
)

// NewRouter registers the API endpoints on engine. Global middleware
// (recovery, request ID, timeout, request log) is expected to already be
// attached to engine via engine.Use(...) before this is called. Handlers live
// in the feature transports (documents, turns, history).
func NewRouter(engine *gin.Engine, deps *dependencies) *gin.Engine {
	engine.POST(RouteDocuments, deps.Ingest)
	engine.POST(RouteDocumentsReset, deps.DeleteAllData)
	engine.POST(RouteTurns, deps.turns.StartTurn)
	engine.GET(RouteTurnEvents, deps.turns.StreamTurn)
	engine.POST(RouteTurnCancel, deps.turns.CancelTurn)
	engine.GET(RouteSessions, deps.hist.ListSessions)
	engine.DELETE(RouteSessions, deps.hist.DeleteAllSessions)
	engine.GET(RouteSession, deps.hist.GetSession)
	engine.DELETE(RouteSession, deps.hist.DeleteSession)
	engine.GET(RouteHealth, func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	engine.GET(RouteReady, deps.readinessHandler)
	return engine
}
