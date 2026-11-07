package rewriter

import (
	"context"
	"net/http"
	"time"

	conversationrewriting "nadir/internal/core/conversation/rewriting"
)

// DependenciesConfig groups the Ollama rewrite endpoint and timeout.
type DependenciesConfig struct {
	Addr           string        // Ollama base addr, e.g. http://localhost:11434
	Model          string        // instruct LLM used for rewriting
	Think          *bool         // nil leaves the model default unchanged
	RequestTimeout time.Duration // timeout for one rewrite request
	KeepAlive      string
}

// dependencies rewrites conversational follow-ups over Ollama.
type dependencies struct {
	addr      string
	model     string
	think     *bool
	client    *http.Client
	keepAlive string
	// complete, when set, replaces the Ollama transport. Prompts and output
	// cleanup are provider-independent; only the completion call differs.
	complete Completer
}

// Completer runs one system+user chat exchange and returns the reply text.
type Completer func(ctx context.Context, system, user string) (string, error)

var _ conversationrewriting.Rewriter = (*dependencies)(nil)

// NewDependencies constructs an Ollama rewriting Adapter.
func NewDependencies(cfg DependenciesConfig) *dependencies {
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	return &dependencies{
		addr:      cfg.Addr,
		model:     cfg.Model,
		think:     cfg.Think,
		keepAlive: cfg.KeepAlive,
		client:    &http.Client{Timeout: timeout},
	}
}

// NewWithCompleter constructs a rewriter that sends its prompt through complete
// (for example a hosted provider) instead of Ollama.
func NewWithCompleter(complete Completer) *dependencies {
	return &dependencies{complete: complete}
}
