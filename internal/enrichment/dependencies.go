package enrichment

import (
	"net/http"
	"time"
)

// DependenciesConfig groups everything needed to construct the enrichment
// client.
type DependenciesConfig struct {
	HypeAddr        string        // Ollama base addr for HyPE
	HypeModel       string        // instruct LLM used for HyPE
	ContextualAddr  string        // Ollama base addr for contextual retrieval
	ContextualModel string        // instruct LLM used for contextual retrieval
	RequestTimeout  time.Duration // timeout for one enrichment request
}

// dependencies performs index-time LLM enrichment over Ollama.
type dependencies struct {
	hypeAddr        string
	hypeModel       string
	contextualAddr  string
	contextualModel string
	client          *http.Client
}

var _ Enricher = (*dependencies)(nil)

func NewDependencies(cfg DependenciesConfig) *dependencies {
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	return &dependencies{
		hypeAddr:        cfg.HypeAddr,
		hypeModel:       cfg.HypeModel,
		contextualAddr:  cfg.ContextualAddr,
		contextualModel: cfg.ContextualModel,
		client:          &http.Client{Timeout: timeout},
	}
}
