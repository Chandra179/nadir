package embedding

import (
	"net/http"
	"time"

	"nadir/internal/embedding"
)

// DependenciesConfig groups everything needed to construct the Ollama
// embedder.
// DependenciesConfig groups the Ollama embedding endpoint and vector shape.
type DependenciesConfig struct {
	Addr           string
	Model          string
	Dimensions     int
	RequestTimeout time.Duration
}

// dependencies embeds text via an Ollama embedding model.
type dependencies struct {
	addr       string
	model      string
	dimensions int
	client     *http.Client
}

var _ embedding.Embedder = (*dependencies)(nil)

// NewDependencies constructs an Ollama embedding Adapter.
func NewDependencies(cfg DependenciesConfig) *dependencies {
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &dependencies{
		addr:       cfg.Addr,
		model:      cfg.Model,
		dimensions: cfg.Dimensions,
		client:     &http.Client{Timeout: timeout},
	}
}
