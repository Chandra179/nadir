package generator

import (
	"context"
	"net/http"
	"time"

	conversationgeneration "nadir/internal/conversation/generation"
	"nadir/internal/platform/inference"
)

// DependenciesConfig groups the Ollama generation endpoint and timeout.
type DependenciesConfig struct {
	Addr           string
	Model          string
	RequestTimeout time.Duration
	KeepAlive      string
	Gate           *inference.Gate
	Admission      func(context.Context) (func(), error)
}

// dependencies streams RAG answers from an Ollama chat model.
type dependencies struct {
	addr      string
	model     string
	client    *http.Client
	keepAlive string
	gate      *inference.Gate
	admission func(context.Context) (func(), error)
}

var _ conversationgeneration.Generator = (*dependencies)(nil)

// NewDependencies constructs an Ollama generation Adapter.
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
		addr:      cfg.Addr,
		model:     cfg.Model,
		keepAlive: cfg.KeepAlive,
		gate:      gate,
		admission: cfg.Admission,
		client: &http.Client{
			Timeout: timeout,
		},
	}
}
