// Package logger configures structured standard-library logging.
package logger

import (
	"log/slog"
	"os"
	"strings"
)

// New preserves the legacy logger-level interpretation. In particular the
// shipped "dev" value fell back to INFO in the previous Zap constructor.
func New(level string) (*slog.Logger, error) {
	parsed := slog.LevelInfo
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		parsed = slog.LevelDebug
	case "warn", "warning":
		parsed = slog.LevelWarn
	case "error":
		parsed = slog.LevelError
	case "dpanic", "panic", "fatal":
		parsed = slog.LevelError + 4
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: parsed})), nil
}
