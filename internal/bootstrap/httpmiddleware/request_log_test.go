package middleware

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestLogUsesRoutePatternAndOmitsQuery(t *testing.T) {
	var output bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&output, nil))
	deps := NewDependencies(DependenciesConfig{Logger: log})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/turns/{id}/events", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	request := httptest.NewRequest("GET", "/api/v1/turns/private-id/events?token=secret", nil)
	response := httptest.NewRecorder()
	deps.RequestLog(mux).ServeHTTP(response, request)
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["path"] != "GET /api/v1/turns/{id}/events" {
		t.Fatalf("logged path = %v", record["path"])
	}
	if _, ok := record["query"]; ok {
		t.Fatal("request query was logged")
	}
	if bytes.Contains(output.Bytes(), []byte("private-id")) || bytes.Contains(output.Bytes(), []byte("secret")) {
		t.Fatal("request parameter leaked into log")
	}
}
