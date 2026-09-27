package server

import (
	"encoding/json"
	"net/http"

	"nadir/internal/core/observability"
)

func metricsHandler(recorder *observability.Recorder) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(recorder.Snapshot())
	})
}
