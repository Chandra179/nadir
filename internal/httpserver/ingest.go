package httpserver

import (
	"encoding/json"
	"net/http"
	"time"

	"nadir/internal/pkb"
	"nadir/pkg/otel"

	"github.com/Chandra179/gosdk/logger"
)

type IngestHandler struct {
	svc     *pkb.IngestService
	log     logger.Logger
	metrics *otel.Metrics
}

func NewIngestHandler(lister pkb.FileLister, pipeline *pkb.Pipeline, fetcher pkb.Fetcher, store pkb.Store, log logger.Logger) *IngestHandler {
	return &IngestHandler{
		svc: pkb.NewIngestService(lister, pipeline, fetcher, store, log),
		log: log,
	}
}

func (h *IngestHandler) WithMetrics(m *otel.Metrics) *IngestHandler {
	h.metrics = m
	h.svc.WithMetrics(m)
	return h
}

type ingestResponse struct {
	Processed int    `json:"processed"`
	Skipped   int    `json:"skipped"`
	Failed    int    `json:"failed"`
	Error     string `json:"error,omitempty"`
}

func (h *IngestHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	start := time.Now()

	result, err := h.svc.Run(ctx)
	h.metrics.RecordIngestRun(ctx, time.Since(start))

	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		h.log.Error(ctx, "ingest run failed", logger.Field{Key: "error", Value: err.Error()})
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ingestResponse{Error: err.Error()})
		return
	}
	json.NewEncoder(w).Encode(ingestResponse{
		Processed: result.Processed,
		Skipped:   result.Skipped,
		Failed:    result.Failed,
	})
}
