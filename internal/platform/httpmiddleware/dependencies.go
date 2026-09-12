package middleware

import (
	"go.uber.org/zap"
)

// DependenciesConfig groups everything needed to construct the stateful
// middleware (structured logging).
type DependenciesConfig struct {
	Logger *zap.Logger
}

type dependencies struct {
	logger *zap.Logger
}

func NewDependencies(cfg DependenciesConfig) *dependencies {
	return &dependencies{logger: cfg.Logger}
}
