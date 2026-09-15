package enrichment

import (
	"context"
	"net/http"
	"time"

	knowledgeenrichment "nadir/internal/knowledge/enrichment"
	"nadir/internal/platform/inference"
)

// DependenciesConfig groups the role-specific Ollama endpoints used for
// index-time enrichment.
type DependenciesConfig struct {
	HypeAddr        string        // Ollama base addr for HyPE
	HypeModel       string        // instruct LLM used for HyPE
	ContextualAddr  string        // Ollama base addr for contextual retrieval
	ContextualModel string        // instruct LLM used for contextual retrieval
	RequestTimeout  time.Duration // timeout for one enrichment request
	KeepAlive       string
	Gate            *inference.Gate
	Admission       func(context.Context) (func(), error)
}

// dependencies performs index-time LLM enrichment over Ollama.
type dependencies struct {
	hypeAddr        string
	hypeModel       string
	contextualAddr  string
	contextualModel string
	client          *http.Client
	keepAlive       string
	gate            *inference.Gate
	admission       func(context.Context) (func(), error)
}

var _ knowledgeenrichment.Enricher = (*dependencies)(nil)

// NewDependencies constructs an Ollama enrichment Adapter.
func NewDependencies(cfg DependenciesConfig) *dependencies {
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	gate := cfg.Gate
	if gate == nil {
		gate = inference.NewGate(1, 30*time.Second)
	}
	return &dependencies{
		hypeAddr:        cfg.HypeAddr,
		hypeModel:       cfg.HypeModel,
		contextualAddr:  cfg.ContextualAddr,
		contextualModel: cfg.ContextualModel,
		client:          &http.Client{Timeout: timeout},
		keepAlive:       cfg.KeepAlive,
		gate:            gate,
		admission:       cfg.Admission,
	}
}
