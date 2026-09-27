package api

import (
	"io"
	"net/http"
)

const (
	RouteDocuments      = "/api/v1/documents"
	RouteDocumentsReset = "/api/v1/documents/reset"
	RouteTurns          = "/api/v1/turns"
	RouteTurnEvents     = "/api/v1/turns/{id}/events"
	RouteTurnCancel     = "/api/v1/turns/{id}/cancel"
	RouteSessions       = "/api/v1/sessions"
	RouteSession        = "/api/v1/sessions/{id}"
	RouteHealth         = "/api/v1/health"
	RouteReady          = "/api/v1/ready"
)

// NewRouter maps HTTP requests to the use-case transport. The method check
// preserves the previous router's 404 for an unsupported method.
func NewRouter(mux *http.ServeMux, deps *dependencies) *http.ServeMux {
	mux.HandleFunc(RouteDocuments, method(http.MethodPost, deps.Ingest))
	mux.HandleFunc(RouteDocumentsReset, method(http.MethodPost, deps.DeleteAllData))
	mux.HandleFunc(RouteTurns, method(http.MethodPost, deps.turns.StartTurn))
	mux.HandleFunc(RouteTurnEvents, method(http.MethodGet, deps.turns.StreamTurn))
	mux.HandleFunc(RouteTurnCancel, method(http.MethodPost, deps.turns.CancelTurn))
	mux.HandleFunc(RouteSessions, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			deps.hist.ListSessions(w, r)
		case http.MethodDelete:
			deps.hist.DeleteAllSessions(w, r)
		default:
			legacyNotFound(w, r)
		}
	})
	mux.HandleFunc(RouteSession, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			deps.hist.GetSession(w, r)
		case http.MethodDelete:
			deps.hist.DeleteSession(w, r)
		default:
			legacyNotFound(w, r)
		}
	})
	mux.HandleFunc(RouteHealth, method(http.MethodGet, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	mux.HandleFunc(RouteReady, method(http.MethodGet, deps.readinessHandler))
	mux.HandleFunc("/", legacyNotFound)
	return mux
}

func method(allowed string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != allowed {
			legacyNotFound(w, r)
			return
		}
		handler(w, r)
	}
}

func legacyNotFound(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusNotFound)
	_, _ = io.WriteString(w, "404 page not found")
}
