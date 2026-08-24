package enrichment

import (
	"net/http"
	"time"
)

// DependenciesConfig groups everything needed to construct the enrichment
// client.
type DependenciesConfig struct {
	Addr  string // Ollama base addr, e.g. http://localhost:11434
	Model string // instruct LLM used for generation
}

// dependencies performs index-time LLM enrichment over Ollama.
type dependencies struct {
	addr   string
	model  string
	client *http.Client
}

var _ Enricher = (*dependencies)(nil)

func NewDependencies(cfg DependenciesConfig) *dependencies {
	return &dependencies{
		addr:   cfg.Addr,
		model:  cfg.Model,
		client: &http.Client{Timeout: 120 * time.Second},
	}
}
