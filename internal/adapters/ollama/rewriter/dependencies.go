package rewriter

import (
	"net/http"
	"time"

	retrievalrewriting "nadir/internal/retrieval/rewriting"
)

// DependenciesConfig groups everything needed to construct the rewriter.
type DependenciesConfig struct {
	Addr           string        // Ollama base addr, e.g. http://localhost:11434
	Model          string        // instruct LLM used for rewriting
	RequestTimeout time.Duration // timeout for one rewrite request
}

// dependencies rewrites conversational follow-ups over Ollama.
type dependencies struct {
	addr   string
	model  string
	client *http.Client
}

var _ retrievalrewriting.Rewriter = (*dependencies)(nil)

func NewDependencies(cfg DependenciesConfig) *dependencies {
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	return &dependencies{
		addr:   cfg.Addr,
		model:  cfg.Model,
		client: &http.Client{Timeout: timeout},
	}
}
