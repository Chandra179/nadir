package docling

import (
	"time"

	"log/slog"
)

// DependenciesConfig groups the Docling endpoint and request timeout.
type DependenciesConfig struct {
	Addr           string
	RequestTimeout time.Duration
	Log            *slog.Logger
}
