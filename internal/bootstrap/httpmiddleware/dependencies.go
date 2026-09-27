package middleware

import (
	"log/slog"
)

// DependenciesConfig groups everything needed to construct the stateful
// middleware (structured logging).
type DependenciesConfig struct {
	Logger *slog.Logger
}

type dependencies struct {
	logger *slog.Logger
}

// NewDependencies constructs the HTTP middleware dependencies.
func NewDependencies(cfg DependenciesConfig) *dependencies {
	return &dependencies{logger: cfg.Logger}
}
