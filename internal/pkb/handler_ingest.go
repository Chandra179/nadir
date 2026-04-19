package pkb

import (
	"encoding/json"
	"net/http"

	"github.com/Chandra179/gosdk/logger"
)

// IngestHandler handles POST /ingest to ingest all markdown files from the submodule.
type IngestHandler struct {
	lister   FileLister
	pipeline *Pipeline
	fetcher  Fetcher
	store    Store
	embedder Embedder
	log      logger.Logger
}

func NewIngestHandler(lister FileLister, pipeline *Pipeline, fetcher Fetcher, store Store, embedder Embedder, log logger.Logger) *IngestHandler {
	return &IngestHandler{
		lister:   lister,
		pipeline: pipeline,
		fetcher:  fetcher,
		store:    store,
		embedder: embedder,
		log:      log,
	}
}

type IngestResponse struct {
	Processed int    `json:"processed"`
	Skipped   int    `json:"skipped"`
	Failed    int    `json:"failed"`
	Error     string `json:"error,omitempty"`
}

func (h *IngestHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if err := h.store.EnsureCollection(ctx, h.embedder.Dimensions()); err != nil {
		h.log.Error(ctx, "ensure collection failed", logger.Field{Key: "error", Value: err.Error()})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(IngestResponse{Error: err.Error()})
		return
	}

	files, err := h.lister.ListMarkdownFiles(ctx, "")
	if err != nil {
		h.log.Error(ctx, "list files failed", logger.Field{Key: "error", Value: err.Error()})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(IngestResponse{Error: err.Error()})
		return
	}

	processed := 0
	skipped := 0
	failed := 0
	for _, f := range files {
		if f.SHA != "" {
			stored, err := h.store.GetFileSHA(ctx, f.Path)
			if err == nil && stored == f.SHA {
				skipped++
				continue
			}
		}
		text, err := h.fetcher.FetchFile(ctx, f.Path, "")
		if err != nil {
			h.log.Error(ctx, "fetch file failed", logger.Field{Key: "path", Value: f.Path}, logger.Field{Key: "error", Value: err.Error()})
			failed++
			continue
		}
		if err := h.pipeline.Ingest(ctx, f.Path, text, f.SHA); err != nil {
			h.log.Error(ctx, "ingest failed", logger.Field{Key: "path", Value: f.Path}, logger.Field{Key: "error", Value: err.Error()})
			failed++
			continue
		}
		processed++
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(IngestResponse{Processed: processed, Skipped: skipped, Failed: failed})
}
