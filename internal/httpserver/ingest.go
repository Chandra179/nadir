package httpserver

import (
	"encoding/json"
	"net/http"

	"nadir/internal/cache"
	"nadir/internal/ingest"
	"nadir/internal/store"

	"github.com/Chandra179/gosdk/logger"
)

type IngestHandler struct {
	svc   *ingest.Service
	cache *cache.SemanticCache
	log   logger.Logger
}

func NewIngestHandler(roots []string, ignorePatterns []string, processor ingest.Processor, s store.Store, c *cache.SemanticCache, log logger.Logger) *IngestHandler {
	return &IngestHandler{
		svc:   ingest.NewService(roots, ignorePatterns, processor, s, log),
		cache: c,
		log:   log,
	}
}

type ingestResponse struct {
	Processed int    `json:"processed"`
	Skipped   int    `json:"skipped"`
	Failed    int    `json:"failed"`
	Error     string `json:"error,omitempty"`
}

func (h *IngestHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if h.cache != nil {
		if err := h.cache.Clear(ctx); err != nil {
			h.log.Warn(ctx, "failed to clear semantic cache before ingest", logger.Field{Key: "error", Value: err.Error()})
		}
	}

	result, err := h.svc.Run(ctx)

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
