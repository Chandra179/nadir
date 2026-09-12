package generator

import (
	"net/http"
	"time"
)

// DependenciesConfig groups everything needed to construct the Ollama
// answer generator.
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

var _ Generator = (*dependencies)(nil)

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
