package rewriter

import (
	"net/http"
	"time"
)

// DependenciesConfig groups everything needed to construct the rewriter.
type DependenciesConfig struct {
	Addr  string // Ollama base addr, e.g. http://localhost:11434
	Model string // instruct LLM used for rewriting
}

// dependencies rewrites conversational follow-ups over Ollama.
type dependencies struct {
	addr   string
	model  string
	client *http.Client
}

var _ Rewriter = (*dependencies)(nil)

func NewDependencies(cfg DependenciesConfig) *dependencies {
	return &dependencies{
		addr:   cfg.Addr,
		model:  cfg.Model,
		client: &http.Client{Timeout: 8 * time.Second},
	}
}
