package embedding

import (
	"net/http"
	"time"

	"nadir/internal/embedding"
	"nadir/internal/platform/inference"
)

// DependenciesConfig groups everything needed to construct the Ollama
// embedder.
// DependenciesConfig groups the Ollama embedding endpoint and vector shape.
type DependenciesConfig struct {
	Addr           string
	Model          string
	Dimensions     int
	RequestTimeout time.Duration
	KeepAlive      string
	Gate           *inference.Gate
}

// dependencies embeds text via an Ollama embedding model.
type dependencies struct {
	addr       string
	model      string
	dimensions int
	client     *http.Client
	keepAlive  string
	gate       *inference.Gate
}

var _ embedding.Embedder = (*dependencies)(nil)

// NewDependencies constructs an Ollama embedding Adapter.
func NewDependencies(cfg DependenciesConfig) *dependencies {
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	gate := cfg.Gate
	if gate == nil {
		gate = inference.NewGate(1, 30*time.Second)
	}
	return &dependencies{
		addr:       cfg.Addr,
		model:      cfg.Model,
		dimensions: cfg.Dimensions,
		client:     &http.Client{Timeout: timeout},
		keepAlive:  cfg.KeepAlive,
		gate:       gate,
	}
}
