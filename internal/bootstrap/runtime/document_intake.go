package runtime

import (
	"context"
	"log/slog"

	config "nadir/internal/bootstrap/configuration"
	"nadir/internal/providers/docling"
)

func documentIntake(cfg config.DoclingConfig, log *slog.Logger) interface {
	Convert(context.Context, string, []byte) ([]byte, error)
} {
	if !cfg.Enabled {
		// Return a nil interface, never a typed nil *Converter.
		return nil
	}
	return docling.NewDependencies(docling.DependenciesConfig{Addr: cfg.Addr, RequestTimeout: cfg.RequestTimeout, Log: log})
}
