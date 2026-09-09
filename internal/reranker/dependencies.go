package reranker

import (
	"net/http"
	"time"
)

// DependenciesConfig groups everything needed to construct the HTTP
// cross-encoder reranker client.
type DependenciesConfig struct {
	Addr           string
	MaxConcurrent  int
	RequestTimeout time.Duration
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
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &dependencies{
		addr:   cfg.Addr,
		client: &http.Client{Timeout: timeout},
		sem:    make(chan struct{}, maxConcurrent),
	}
}
