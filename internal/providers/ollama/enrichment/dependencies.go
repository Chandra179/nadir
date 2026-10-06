package enrichment

import (
	"net/http"
	"time"

	knowledgeenrichment "nadir/internal/core/documents/enrichment"
)

// DependenciesConfig groups the role-specific Ollama endpoints used for
// index-time enrichment.
type DependenciesConfig struct {
	ContextualAddr  string        // Ollama base addr for contextual retrieval
	ContextualModel string        // instruct LLM used for contextual retrieval
	Think           *bool         // nil leaves the model default unchanged
	RequestTimeout  time.Duration // timeout for one enrichment request
	KeepAlive       string
}

// dependencies performs index-time LLM enrichment over Ollama.
type dependencies struct {
	contextualAddr  string
	contextualModel string
	think           *bool
	client          *http.Client
	keepAlive       string
}

var _ knowledgeenrichment.Enricher = (*dependencies)(nil)

// NewDependencies constructs an Ollama enrichment Adapter.
func NewDependencies(cfg DependenciesConfig) *dependencies {
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	return &dependencies{
		contextualAddr:  cfg.ContextualAddr,
		contextualModel: cfg.ContextualModel,
		think:           cfg.Think,
		client:          &http.Client{Timeout: timeout},
		keepAlive:       cfg.KeepAlive,
	}
}
