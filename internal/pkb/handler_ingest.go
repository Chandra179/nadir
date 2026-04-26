package pkb

import (
	"encoding/json"
	"net/http"
	"time"

	"nadir/pkg/otel"

	"github.com/Chandra179/gosdk/logger"
)

// IngestHandler handles POST /ingest.
type IngestHandler struct {
	svc     *IngestService
	log     logger.Logger
	metrics *otel.Metrics
}

func NewIngestHandler(lister FileLister, pipeline *Pipeline, fetcher Fetcher, store Store, log logger.Logger) *IngestHandler {
	return &IngestHandler{
		svc: NewIngestService(lister, pipeline, fetcher, store, log),
		log: log,
	}
}

// WithMetrics attaches an otel.Metrics recorder.
func (h *IngestHandler) WithMetrics(m *otel.Metrics) *IngestHandler {
	h.metrics = m
	h.svc.WithMetrics(m)
	return h
}

type IngestResponse struct {
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
		json.NewEncoder(w).Encode(IngestResponse{Error: err.Error()})
		return
	}
	json.NewEncoder(w).Encode(IngestResponse{
		Processed: result.Processed,
		Skipped:   result.Skipped,
		Failed:    result.Failed,
	})
}
