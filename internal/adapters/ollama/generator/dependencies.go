package generator

import (
	"net/http"
	"time"

	conversationgeneration "nadir/internal/conversation/generation"
)

// DependenciesConfig groups the Ollama generation endpoint and timeout.
type DependenciesConfig struct {
	Addr           string
	Model          string
	RequestTimeout time.Duration
}

// dependencies streams RAG answers from an Ollama chat model.
type dependencies struct {
	addr   string
	model  string
	client *http.Client
}

var _ conversationgeneration.Generator = (*dependencies)(nil)

// NewDependencies constructs an Ollama generation Adapter.
func NewDependencies(cfg DependenciesConfig) *dependencies {
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	return &dependencies{
		addr:  cfg.Addr,
		model: cfg.Model,
		client: &http.Client{
			Timeout: timeout,
		},
	}
}
