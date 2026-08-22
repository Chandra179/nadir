package generator

import (
	"net/http"
	"time"
)

// DependenciesConfig groups everything needed to construct the Ollama
// answer generator.
type DependenciesConfig struct {
	Addr             string
	Model            string
	MaxContextTokens int
}

// dependencies streams RAG answers from an Ollama chat model.
type dependencies struct {
	addr             string
	model            string
	maxContextTokens int
	client           *http.Client
}

func NewDependencies(cfg DependenciesConfig) *dependencies {
	maxContextTokens := cfg.MaxContextTokens
	if maxContextTokens <= 0 {
		maxContextTokens = 2800
	}
	return &dependencies{
		addr:             cfg.Addr,
		model:            cfg.Model,
		maxContextTokens: maxContextTokens,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}
