package docling

import (
	"time"

	"go.uber.org/zap"
)

// DependenciesConfig groups the Docling endpoint and request timeout.
type DependenciesConfig struct {
	Addr           string
	RequestTimeout time.Duration
	Log            *zap.Logger
}
