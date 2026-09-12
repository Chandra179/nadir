package reranker

import (
	"net/http"
	"time"

	"go.uber.org/zap"
)

// DependenciesConfig groups everything needed to construct the HTTP
// cross-encoder reranker client.
type DependenciesConfig struct {
	Addr           string
	MaxConcurrent  int
	RequestTimeout time.Duration
	Log            *zap.Logger
}

type dependencies struct {
	addr   string
	client *http.Client
	sem    chan struct{}
	log    *zap.Logger
}

var _ Reranker = (*dependencies)(nil)

func NewDependencies(cfg DependenciesConfig) *dependencies {
	maxConcurrent := cfg.MaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = 10
	}
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}
	return &dependencies{
		addr:   cfg.Addr,
		client: &http.Client{Timeout: timeout},
		sem:    make(chan struct{}, maxConcurrent),
		log:    log,
	}
}
