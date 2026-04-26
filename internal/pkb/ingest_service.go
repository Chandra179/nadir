package pkb

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"

	"nadir/pkg/otel"

	"github.com/Chandra179/gosdk/logger"
)

const ingestWorkers = 8

// IngestResult holds per-run ingest counters.
type IngestResult struct {
	Processed int
	Skipped   int
	Failed    int
}

// IngestService runs the full ingest loop: list → SHA dedup → concurrent fetch+pipeline.
type IngestService struct {
	lister   FileLister
	pipeline *Pipeline
	fetcher  Fetcher
	store    Store
	log      logger.Logger
	metrics  *otel.Metrics
}

func NewIngestService(lister FileLister, pipeline *Pipeline, fetcher Fetcher, store Store, log logger.Logger) *IngestService {
	return &IngestService{
		lister:   lister,
		pipeline: pipeline,
		fetcher:  fetcher,
		store:    store,
		log:      log,
	}
}

func (s *IngestService) WithMetrics(m *otel.Metrics) *IngestService {
	s.metrics = m
	return s
}

func (s *IngestService) Run(ctx context.Context) (IngestResult, error) {
	files, err := s.lister.ListMarkdownFiles(ctx, "")
	if err != nil {
		return IngestResult{}, err
	}

	storedSHAs, err := s.store.GetAllFileSHAs(ctx)
	if err != nil {
		return IngestResult{}, err
	}

	var processed, skipped, failed atomic.Int64
	sem := make(chan struct{}, ingestWorkers)
	var wg sync.WaitGroup

	for _, f := range files {
		if f.SHA != "" && storedSHAs[f.Path] == f.SHA {
			skipped.Add(1)
			s.metrics.RecordIngestFile(ctx, "skipped")
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(f FileEntry) {
			defer wg.Done()
			defer func() { <-sem }()

			fetchPath := f.Path
			if f.Root != "" {
				fetchPath = filepath.Join(f.Root, f.Path)
			}
			text, err := s.fetcher.FetchFile(ctx, fetchPath, "")
			if err != nil {
				s.log.Error(ctx, "fetch file failed", logger.Field{Key: "path", Value: f.Path}, logger.Field{Key: "error", Value: err.Error()})
				failed.Add(1)
				s.metrics.RecordIngestFile(ctx, "failed")
				return
			}
			if err := s.pipeline.Ingest(ctx, f.Path, text, f.SHA); err != nil {
				s.log.Error(ctx, "ingest failed", logger.Field{Key: "path", Value: f.Path}, logger.Field{Key: "error", Value: err.Error()})
				failed.Add(1)
				s.metrics.RecordIngestFile(ctx, "failed")
				return
			}
			processed.Add(1)
			s.metrics.RecordIngestFile(ctx, "processed")
		}(f)
	}
	wg.Wait()

	return IngestResult{
		Processed: int(processed.Load()),
		Skipped:   int(skipped.Load()),
		Failed:    int(failed.Load()),
	}, nil
}
