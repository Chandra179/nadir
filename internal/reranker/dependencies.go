package reranker

import (
	"net/http"
	"time"
)

// DependenciesConfig groups everything needed to construct the HTTP
// cross-encoder reranker client.
type DependenciesConfig struct {
	Addr          string
	MaxConcurrent int
}

type dependencies struct {
	addr   string
	client *http.Client
	sem    chan struct{}
}

func NewDependencies(cfg DependenciesConfig) *dependencies {
	maxConcurrent := cfg.MaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = 10
	}
	return &dependencies{
		addr:   cfg.Addr,
		client: &http.Client{Timeout: 30 * time.Second},
		sem:    make(chan struct{}, maxConcurrent),
	}
}
