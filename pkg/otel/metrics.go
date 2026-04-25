package otel

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const instrScope = "nadir/pkb"

// Metrics holds all RAG-specific OTEL instruments.
// Zero value is a no-op (all instruments are nil-safe via the OTEL API).
type Metrics struct {
	// search
	searchDuration   metric.Float64Histogram
	searchResultsN   metric.Int64Histogram

	// cache
	cacheHits   metric.Int64Counter
	cacheMisses metric.Int64Counter

	// embedding
	embedDuration      metric.Float64Histogram
	embedBatchSize     metric.Int64Histogram

	// rerank
	rerankDuration  metric.Float64Histogram
	rerankDelta     metric.Float64Histogram // score[0] before - after rerank

	// ingest
	ingestProcessed metric.Int64Counter
	ingestSkipped   metric.Int64Counter
	ingestFailed    metric.Int64Counter
	ingestDuration  metric.Float64Histogram
}

// New registers all instruments against meter and returns a Metrics.
// Call once at startup; share the returned *Metrics across handlers.
func New(meter metric.Meter) (*Metrics, error) {
	m := &Metrics{}
	var err error

	if m.searchDuration, err = meter.Float64Histogram(
		"pkb.search.duration_seconds",
		metric.WithDescription("end-to-end search latency (embed+retrieve+rerank)"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.01, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0),
	); err != nil {
		return nil, err
	}

	if m.searchResultsN, err = meter.Int64Histogram(
		"pkb.search.results_returned",
		metric.WithDescription("number of chunks returned per search"),
		metric.WithExplicitBucketBoundaries(0, 1, 3, 5, 10, 20),
	); err != nil {
		return nil, err
	}

	if m.cacheHits, err = meter.Int64Counter(
		"pkb.cache.hits_total",
		metric.WithDescription("semantic cache hits"),
	); err != nil {
		return nil, err
	}

	if m.cacheMisses, err = meter.Int64Counter(
		"pkb.cache.misses_total",
		metric.WithDescription("semantic cache misses"),
	); err != nil {
		return nil, err
	}

	if m.embedDuration, err = meter.Float64Histogram(
		"pkb.embed.duration_seconds",
		metric.WithDescription("embedding call latency"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1.0),
	); err != nil {
		return nil, err
	}

	if m.embedBatchSize, err = meter.Int64Histogram(
		"pkb.embed.batch_size",
		metric.WithDescription("texts per batch embed call"),
		metric.WithExplicitBucketBoundaries(1, 5, 10, 20, 50, 100),
	); err != nil {
		return nil, err
	}

	if m.rerankDuration, err = meter.Float64Histogram(
		"pkb.rerank.duration_seconds",
		metric.WithDescription("cross-encoder rerank latency"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.05, 0.1, 0.25, 0.5, 1.0, 2.0),
	); err != nil {
		return nil, err
	}

	if m.rerankDelta, err = meter.Float64Histogram(
		"pkb.rerank.score_delta",
		metric.WithDescription("top-1 score change after reranking (post minus pre)"),
		metric.WithExplicitBucketBoundaries(-0.5, -0.25, -0.1, 0, 0.1, 0.25, 0.5),
	); err != nil {
		return nil, err
	}

	if m.ingestProcessed, err = meter.Int64Counter(
		"pkb.ingest.files_processed_total",
		metric.WithDescription("files ingested successfully"),
	); err != nil {
		return nil, err
	}

	if m.ingestSkipped, err = meter.Int64Counter(
		"pkb.ingest.files_skipped_total",
		metric.WithDescription("files skipped (SHA unchanged)"),
	); err != nil {
		return nil, err
	}

	if m.ingestFailed, err = meter.Int64Counter(
		"pkb.ingest.files_failed_total",
		metric.WithDescription("files that failed ingest"),
	); err != nil {
		return nil, err
	}

	if m.ingestDuration, err = meter.Float64Histogram(
		"pkb.ingest.duration_seconds",
		metric.WithDescription("total ingest run latency"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(1, 5, 10, 30, 60, 120, 300),
	); err != nil {
		return nil, err
	}

	return m, nil
}

// --- Search ---

func (m *Metrics) RecordSearch(ctx context.Context, dur time.Duration, n int, mode string) {
	if m == nil {
		return
	}
	attrs := metric.WithAttributes(attribute.String("mode", mode))
	m.searchDuration.Record(ctx, dur.Seconds(), attrs)
	m.searchResultsN.Record(ctx, int64(n), attrs)
}

// --- Cache ---

func (m *Metrics) RecordCacheHit(ctx context.Context) {
	if m == nil {
		return
	}
	m.cacheHits.Add(ctx, 1)
}

func (m *Metrics) RecordCacheMiss(ctx context.Context) {
	if m == nil {
		return
	}
	m.cacheMisses.Add(ctx, 1)
}

// --- Embed ---

func (m *Metrics) RecordEmbed(ctx context.Context, dur time.Duration, batchSize int) {
	if m == nil {
		return
	}
	m.embedDuration.Record(ctx, dur.Seconds())
	m.embedBatchSize.Record(ctx, int64(batchSize))
}

// --- Rerank ---

func (m *Metrics) RecordRerank(ctx context.Context, dur time.Duration, scoreBefore, scoreAfter float32) {
	if m == nil {
		return
	}
	m.rerankDuration.Record(ctx, dur.Seconds())
	m.rerankDelta.Record(ctx, float64(scoreAfter-scoreBefore))
}

// --- Ingest ---

func (m *Metrics) RecordIngestFile(ctx context.Context, outcome string) {
	if m == nil {
		return
	}
	switch outcome {
	case "processed":
		m.ingestProcessed.Add(ctx, 1)
	case "skipped":
		m.ingestSkipped.Add(ctx, 1)
	case "failed":
		m.ingestFailed.Add(ctx, 1)
	}
}

func (m *Metrics) RecordIngestRun(ctx context.Context, dur time.Duration) {
	if m == nil {
		return
	}
	m.ingestDuration.Record(ctx, dur.Seconds())
}
