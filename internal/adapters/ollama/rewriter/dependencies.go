package rewriter

import (
	"context"
	"net/http"
	"time"

	"nadir/internal/platform/inference"
	retrievalrewriting "nadir/internal/retrieval/rewriting"
)

// DependenciesConfig groups the Ollama rewrite endpoint and timeout.
type DependenciesConfig struct {
	Addr           string        // Ollama base addr, e.g. http://localhost:11434
	Model          string        // instruct LLM used for rewriting
	RequestTimeout time.Duration // timeout for one rewrite request
	KeepAlive      string
	Gate           *inference.Gate
	Admission      func(context.Context) (func(), error)
}

// dependencies rewrites conversational follow-ups over Ollama.
type dependencies struct {
	addr      string
	model     string
	client    *http.Client
	keepAlive string
	gate      *inference.Gate
	admission func(context.Context) (func(), error)
}

var _ retrievalrewriting.Rewriter = (*dependencies)(nil)

// NewDependencies constructs an Ollama rewriting Adapter.
func NewDependencies(cfg DependenciesConfig) *dependencies {
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	gate := cfg.Gate
	if gate == nil {
		gate = inference.NewGate(1, 30*time.Second)
	}
	return &dependencies{
		addr:      cfg.Addr,
		model:     cfg.Model,
		keepAlive: cfg.KeepAlive,
		gate:      gate,
		admission: cfg.Admission,
		client:    &http.Client{Timeout: timeout},
	}
}
