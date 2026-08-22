package embedder

import "net/http"

// DependenciesConfig groups everything needed to construct the Ollama
// embedder.
type DependenciesConfig struct {
	Addr       string
	Model      string
	Dimensions int
}

// dependencies embeds text via an Ollama embedding model.
type dependencies struct {
	addr       string
	model      string
	dimensions int
	client     *http.Client
}

func NewDependencies(cfg DependenciesConfig) *dependencies {
	return &dependencies{
		addr:       cfg.Addr,
		model:      cfg.Model,
		dimensions: cfg.Dimensions,
		client:     &http.Client{},
	}
}
