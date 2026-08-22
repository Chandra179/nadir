// Package logger initializes the zap logger used across the application.
package logger

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// New builds a production zap logger at the given level (e.g. "info", "debug").
func New(level string) (*zap.Logger, error) {
	zapLevel := zapcore.InfoLevel
	_ = zapLevel.UnmarshalText([]byte(level))
	cfg := zap.NewProductionConfig()
	cfg.Level = zap.NewAtomicLevelAt(zapLevel)
	return cfg.Build()
}
