package embedding

import (
	"net/http"
	"time"

	"nadir/internal/core/embedding"
	"nadir/internal/core/observability"
)

// DependenciesConfig groups the Ollama embedding endpoint and vector shape.
type DependenciesConfig struct {
	Addr           string
	Model          string
	Dimensions     int
	NumGPU         *int
	RequestTimeout time.Duration
	KeepAlive      string
	// Telemetry receives Ollama's own embedding and model-load timings.
	// Nil disables the recording.
	Telemetry *observability.Recorder
}

// dependencies embeds text via an Ollama embedding model.
type dependencies struct {
	addr       string
	model      string
	dimensions int
	client     *http.Client
	keepAlive  string
	numGPU     *int
	telemetry  *observability.Recorder
}

var _ embedding.Embedder = (*dependencies)(nil)

// NewDependencies constructs an Ollama embedding Adapter.
func NewDependencies(cfg DependenciesConfig) *dependencies {
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	var numGPU *int
	if cfg.NumGPU != nil {
		layers := *cfg.NumGPU
		numGPU = &layers
	}
	return &dependencies{
		addr:       cfg.Addr,
		model:      cfg.Model,
		dimensions: cfg.Dimensions,
		client:     &http.Client{Timeout: timeout},
		keepAlive:  cfg.KeepAlive,
		numGPU:     numGPU,
		telemetry:  cfg.Telemetry,
	}
}
