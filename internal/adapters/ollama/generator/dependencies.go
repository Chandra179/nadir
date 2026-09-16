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
	Format         any
	Options        map[string]any
	Gate           *inference.Gate
	Admission      func(context.Context) (func(), error)
}

// dependencies streams RAG answers from an Ollama chat model.
type dependencies struct {
	addr      string
	model     string
	client    *http.Client
	keepAlive string
	format    any
	options   map[string]any
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
		format:    cfg.Format,
		options:   cloneOptions(cfg.Options),
		gate:      gate,
		admission: cfg.Admission,
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

func cloneOptions(options map[string]any) map[string]any {
	if len(options) == 0 {
		return nil
	}
	copy := make(map[string]any, len(options))
	for key, value := range options {
		copy[key] = value
	}
	return copy
}
