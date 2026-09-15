// Package observability provides bounded, provider-neutral operation telemetry.
// It deliberately uses only the standard library plus the repository logger:
// structured logs carry trace/operation IDs, while a small in-process recorder
// exposes counters and duration summaries for local operations and benchmarks.
package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

type contextKey uint8

const (
	requestIDKey contextKey = iota
	traceIDKey
	operationIDKey
)

// Metric is one bounded operation/outcome aggregate.
type Metric struct {
	Operation     string  `json:"operation"`
	Outcome       string  `json:"outcome"`
	Count         uint64  `json:"count"`
	DurationSumMS float64 `json:"duration_ms_sum"`
	DurationMaxMS float64 `json:"duration_ms_max"`
}

// Snapshot is the JSON representation returned by Handler.
type Snapshot struct {
	GeneratedAt string             `json:"generated_at"`
	Operations  []Metric           `json:"operations"`
	Gauges      map[string]float64 `json:"gauges,omitempty"`
}

type metricKey struct {
	operation string
	outcome   string
}

type metricValue struct {
	count         atomic.Uint64
	durationNanos atomic.Int64
	maxNanos      atomic.Int64
}

// Recorder aggregates bounded operation metrics. Callers must use stable
// operation and outcome names; request data must never be used as a label.
type Recorder struct {
	mu      sync.RWMutex
	metrics map[metricKey]*metricValue
	gauges  map[string]float64
}

// NewRecorder constructs an empty in-process recorder.
func NewRecorder() *Recorder {
	return &Recorder{
		metrics: make(map[metricKey]*metricValue),
		gauges:  make(map[string]float64),
	}
}

// Record adds one operation outcome and duration to the bounded aggregate.
func (r *Recorder) Record(operation, outcome string, duration time.Duration) {
	if r == nil || operation == "" || outcome == "" {
		return
	}
	key := metricKey{operation: operation, outcome: outcome}
	r.mu.RLock()
	value := r.metrics[key]
	r.mu.RUnlock()
	if value == nil {
		r.mu.Lock()
		value = r.metrics[key]
		if value == nil {
			value = &metricValue{}
			r.metrics[key] = value
		}
		r.mu.Unlock()
	}
	value.count.Add(1)
	nanos := duration.Nanoseconds()
	value.durationNanos.Add(nanos)
	for {
		old := value.maxNanos.Load()
		if nanos <= old || value.maxNanos.CompareAndSwap(old, nanos) {
			break
		}
	}
}

// SetGauge sets a bounded operational gauge, such as active admission slots.
func (r *Recorder) SetGauge(name string, value float64) {
	if r == nil || name == "" {
		return
	}
	r.mu.Lock()
	r.gauges[name] = value
	r.mu.Unlock()
}

// Snapshot returns a stable, sorted copy suitable for JSON or tests.
func (r *Recorder) Snapshot() Snapshot {
	result := Snapshot{GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano), Operations: []Metric{}, Gauges: map[string]float64{}}
	if r == nil {
		return result
	}
	r.mu.RLock()
	metrics := make([]struct {
		key   metricKey
		value *metricValue
	}, 0, len(r.metrics))
	for key, value := range r.metrics {
		metrics = append(metrics, struct {
			key   metricKey
			value *metricValue
		}{key: key, value: value})
	}
	maps.Copy(result.Gauges, r.gauges)
	r.mu.RUnlock()
	sort.Slice(metrics, func(i, j int) bool {
		if metrics[i].key.operation != metrics[j].key.operation {
			return metrics[i].key.operation < metrics[j].key.operation
		}
		return metrics[i].key.outcome < metrics[j].key.outcome
	})
	for _, item := range metrics {
		result.Operations = append(result.Operations, Metric{
			Operation:     item.key.operation,
			Outcome:       item.key.outcome,
			Count:         item.value.count.Load(),
			DurationSumMS: float64(item.value.durationNanos.Load()) / float64(time.Millisecond),
			DurationMaxMS: float64(item.value.maxNanos.Load()) / float64(time.Millisecond),
		})
	}
	if len(result.Gauges) == 0 {
		result.Gauges = nil
	}
	return result
}

// Handler exposes the current bounded metrics snapshot as JSON.
func (r *Recorder) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(r.Snapshot())
	})
}

// WithRequestID attaches the incoming request correlation ID and starts a
// trace rooted at that ID. It is safe to call from non-HTTP entry points too.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if requestID == "" {
		requestID = NewID("req")
	}
	ctx = context.WithValue(ctx, requestIDKey, requestID)
	if TraceID(ctx) == "" {
		ctx = context.WithValue(ctx, traceIDKey, requestID)
	}
	return ctx
}

// RequestID returns the request correlation ID attached to ctx.
func RequestID(ctx context.Context) string { return contextString(ctx, requestIDKey) }

// TraceID returns the trace root attached to ctx.
func TraceID(ctx context.Context) string { return contextString(ctx, traceIDKey) }

// OperationID returns the currently active child operation ID.
func OperationID(ctx context.Context) string { return contextString(ctx, operationIDKey) }

func contextString(ctx context.Context, key contextKey) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(key).(string)
	return value
}

// NewID creates a short opaque ID with a stable prefix. IDs are for
// correlation, not authentication.
func NewID(prefix string) string {
	bytes := make([]byte, 12)
	if _, err := rand.Read(bytes); err != nil {
		value := hex.EncodeToString([]byte(time.Now().UTC().Format("150405.000000000")))
		if prefix == "" {
			return value
		}
		return prefix + "-" + value
	}
	if prefix == "" {
		return hex.EncodeToString(bytes)
	}
	return prefix + "-" + hex.EncodeToString(bytes)
}

// Span records one domain operation. A Span is ended exactly once.
type Span struct {
	recorder    *Recorder
	log         *zap.Logger
	operation   string
	operationID string
	traceID     string
	requestID   string
	parentID    string
	started     time.Time
	once        sync.Once
}

// Start creates a child operation context and emits a structured start log.
func Start(ctx context.Context, recorder *Recorder, log *zap.Logger, operation string, fields ...zap.Field) (context.Context, *Span) {
	if ctx == nil {
		ctx = context.Background()
	}
	traceID := TraceID(ctx)
	if traceID == "" {
		traceID = NewID("trace")
		ctx = context.WithValue(ctx, traceIDKey, traceID)
	}
	span := &Span{
		recorder:    recorder,
		log:         log,
		operation:   operation,
		operationID: NewID("op"),
		traceID:     traceID,
		requestID:   RequestID(ctx),
		parentID:    OperationID(ctx),
		started:     time.Now(),
	}
	ctx = context.WithValue(ctx, operationIDKey, span.operationID)
	if log != nil {
		log.Debug("operation started", append(span.identityFields(), fields...)...)
	}
	return ctx, span
}

// ID returns the child operation ID.
func (s *Span) ID() string {
	if s == nil {
		return ""
	}
	return s.operationID
}

// End records and logs the operation outcome. Error strings are bounded to an
// operational label; the caller can attach a full error to a separate log if
// it is safe to do so.
func (s *Span) End(outcome string, err error, fields ...zap.Field) {
	if s == nil {
		return
	}
	s.once.Do(func() {
		if outcome == "" {
			outcome = "success"
		}
		duration := time.Since(s.started)
		if s.recorder != nil {
			s.recorder.Record(s.operation, outcome, duration)
		}
		fields = append(s.identityFields(), fields...)
		fields = append(fields,
			zap.String("outcome", outcome),
			zap.Int64("duration_ms", duration.Milliseconds()),
		)
		if err != nil {
			fields = append(fields, zap.String("error_label", ErrorLabel(err)))
		}
		if s.log == nil {
			return
		}
		if err != nil || outcome == "error" {
			s.log.Warn("operation completed", fields...)
		} else {
			s.log.Debug("operation completed", fields...)
		}
	})
}

func (s *Span) identityFields() []zap.Field {
	fields := []zap.Field{
		zap.String("operation", s.operation),
		zap.String("operation_id", s.operationID),
		zap.String("trace_id", s.traceID),
	}
	if s.requestID != "" {
		fields = append(fields, zap.String("request_id", s.requestID))
	}
	if s.parentID != "" {
		fields = append(fields, zap.String("parent_operation_id", s.parentID))
	}
	return fields
}

// Stage records an existing stage. It is kept for adapter-level logging where
// introducing a child operation would add noise; prefer Start for domain use.
func Stage(log *zap.Logger, stage, outcome string, started time.Time, err error, fields ...zap.Field) {
	if log == nil {
		return
	}
	fields = append(fields,
		zap.String("stage", stage),
		zap.String("outcome", outcome),
		zap.Int64("duration_ms", time.Since(started).Milliseconds()),
	)
	if err != nil {
		fields = append(fields, zap.String("error_label", ErrorLabel(err)))
		log.Warn("stage completed", fields...)
		return
	}
	log.Debug("stage completed", fields...)
}

// StageContext is Stage with the current correlation fields attached.
func StageContext(ctx context.Context, log *zap.Logger, stage, outcome string, started time.Time, err error, fields ...zap.Field) {
	identity := []zap.Field{}
	if requestID := RequestID(ctx); requestID != "" {
		identity = append(identity, zap.String("request_id", requestID))
	}
	if traceID := TraceID(ctx); traceID != "" {
		identity = append(identity, zap.String("trace_id", traceID))
	}
	if operationID := OperationID(ctx); operationID != "" {
		identity = append(identity, zap.String("operation_id", operationID))
	}
	Stage(log, stage, outcome, started, err, append(identity, fields...)...)
}

// ErrorLabel maps arbitrary provider errors to a bounded operational label.
func ErrorLabel(err error) string {
	if err == nil {
		return "none"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "generation"):
		return "generation_error"
	case strings.Contains(message, "status "):
		return "remote_status"
	case strings.Contains(message, "decode"), strings.Contains(message, "unmarshal"), strings.Contains(message, "malformed"):
		return "malformed_response"
	case strings.Contains(message, "missing"), strings.Contains(message, "mismatch"), strings.Contains(message, "empty"):
		return "response_shape"
	case strings.Contains(message, "qdrant"), strings.Contains(message, "ollama"), strings.Contains(message, "sidecar"):
		return "dependency_error"
	default:
		return "unknown"
	}
}
