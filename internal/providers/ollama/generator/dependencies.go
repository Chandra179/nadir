package generator

import (
	"maps"
	"net/http"
	"time"

	conversationgeneration "nadir/internal/core/conversation/generation"
)

// DependenciesConfig groups the Ollama generation endpoint and timeout.
type DependenciesConfig struct {
	Addr           string
	Model          string
	RequestTimeout time.Duration
	KeepAlive      string
	Format         any
	Options        map[string]any
}

// dependencies streams RAG answers from an Ollama chat model.
type dependencies struct {
	addr      string
	model     string
	client    *http.Client
	keepAlive string
	format    any
	options   map[string]any
}

var _ conversationgeneration.Generator = (*dependencies)(nil)

// NewDependencies constructs an Ollama generation Adapter.
func NewDependencies(cfg DependenciesConfig) *dependencies {
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	return &dependencies{
		addr:      cfg.Addr,
		model:     cfg.Model,
		keepAlive: cfg.KeepAlive,
		format:    cfg.Format,
		options:   cloneOptions(cfg.Options),
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
	maps.Copy(copy, options)
	return copy
}
