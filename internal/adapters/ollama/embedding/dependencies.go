package embedder

import (
	"net/http"
	"time"
)

// DependenciesConfig groups everything needed to construct the Ollama
// embedder.
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

var _ Embedder = (*dependencies)(nil)
var _ BatchEmbedder = (*dependencies)(nil)

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
