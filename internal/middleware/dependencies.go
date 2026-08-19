package middleware

import (
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

// DependenciesConfig groups everything needed to construct the stateful
// middleware (structured logging + Prometheus metrics).
type DependenciesConfig struct {
	Logger   *zap.Logger
	Registry *prometheus.Registry
}

type dependencies struct {
	logger              *zap.Logger
	registry            *prometheus.Registry
	httpRequestDuration *prometheus.HistogramVec
	httpRequestsTotal   *prometheus.CounterVec
}

func NewDependencies(cfg DependenciesConfig) *dependencies {
	httpRequestDuration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "http_request_duration_seconds",
		Help: "HTTP request duration in seconds.",
	}, []string{"method", "route", "status"})

	httpRequestsTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total number of HTTP requests.",
	}, []string{"method", "route", "status"})

	cfg.Registry.MustRegister(httpRequestDuration, httpRequestsTotal)

	return &dependencies{
		logger:              cfg.Logger,
		registry:            cfg.Registry,
		httpRequestDuration: httpRequestDuration,
		httpRequestsTotal:   httpRequestsTotal,
	}
}
