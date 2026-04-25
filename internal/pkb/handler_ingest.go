package pkb

import (
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/Chandra179/gosdk/logger"
)

const ingestWorkers = 8

// IngestHandler handles POST /ingest to ingest all markdown files from the submodule.
type IngestHandler struct {
	lister   FileLister
	pipeline *Pipeline
	fetcher  Fetcher
	store    Store
	log      logger.Logger
}

func NewIngestHandler(lister FileLister, pipeline *Pipeline, fetcher Fetcher, store Store, log logger.Logger) *IngestHandler {
	return &IngestHandler{
		lister:   lister,
		pipeline: pipeline,
		fetcher:  fetcher,
		store:    store,
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

	files, err := h.lister.ListMarkdownFiles(ctx, "")
	if err != nil {
		h.log.Error(ctx, "list files failed", logger.Field{Key: "error", Value: err.Error()})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(IngestResponse{Error: err.Error()})
		return
	}

	storedSHAs, err := h.store.GetAllFileSHAs(ctx)
	if err != nil {
		h.log.Error(ctx, "get all file shas failed", logger.Field{Key: "error", Value: err.Error()})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(IngestResponse{Error: err.Error()})
		return
	}

	var processed, skipped, failed atomic.Int64
	sem := make(chan struct{}, ingestWorkers)
	var wg sync.WaitGroup

	for _, f := range files {
		if f.SHA != "" && storedSHAs[f.Path] == f.SHA {
			skipped.Add(1)
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(f FileEntry) {
			defer wg.Done()
			defer func() { <-sem }()

			text, err := h.fetcher.FetchFile(ctx, f.Path, "")
			if err != nil {
				h.log.Error(ctx, "fetch file failed", logger.Field{Key: "path", Value: f.Path}, logger.Field{Key: "error", Value: err.Error()})
				failed.Add(1)
				return
			}
			if err := h.pipeline.Ingest(ctx, f.Path, text, f.SHA); err != nil {
				h.log.Error(ctx, "ingest failed", logger.Field{Key: "path", Value: f.Path}, logger.Field{Key: "error", Value: err.Error()})
				failed.Add(1)
				return
			}
			processed.Add(1)
		}(f)
	}
	wg.Wait()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(IngestResponse{
		Processed: int(processed.Load()),
		Skipped:   int(skipped.Load()),
		Failed:    int(failed.Load()),
	})
}
