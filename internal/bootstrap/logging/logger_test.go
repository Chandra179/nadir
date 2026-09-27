package logger

import (
	"context"
	"log/slog"
	"testing"
)

func TestLegacyLevelMapping(t *testing.T) {
	for _, tt := range []struct {
		name              string
		debug, info, warn bool
	}{
		{name: "dev", info: true, warn: true},
		{name: "debug", debug: true, info: true, warn: true},
		{name: "warn", warn: true},
		{name: "unknown", info: true, warn: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			logger, err := New(tt.name)
			if err != nil {
				t.Fatal(err)
			}
			if logger.Enabled(context.Background(), slog.LevelDebug) != tt.debug ||
				logger.Enabled(context.Background(), slog.LevelInfo) != tt.info ||
				logger.Enabled(context.Background(), slog.LevelWarn) != tt.warn {
				t.Fatalf("logger level mapping for %q changed", tt.name)
			}
		})
	}
}
