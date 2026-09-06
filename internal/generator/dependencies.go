package generator

import (
	"net/http"
	"time"
)

// DependenciesConfig groups everything needed to construct the Ollama
// answer generator.
type DependenciesConfig struct {
	Addr  string
	Model string
}

// dependencies streams RAG answers from an Ollama chat model.
type dependencies struct {
	addr   string
	model  string
	client *http.Client
}

func NewDependencies(cfg DependenciesConfig) *dependencies {
	return &dependencies{
		addr:  cfg.Addr,
		model: cfg.Model,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}
