package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"
)

const defaultShutdownTimeout = 10 * time.Second

// runHTTPServer owns the process lifecycle around one HTTP server. It stops
// accepting new work, gracefully closes HTTP connections, and then drains the
// domain lifecycle before returning so shared dependencies can be closed by
// the composition root.
//
// serve is injected to keep the lifecycle contract testable with an ephemeral
// listener without constructing the production dependency graph.
func runHTTPServer(
	ctx context.Context,
	srv *http.Server,
	shutdownTimeout time.Duration,
	serve func() error,
	drain func(context.Context) error,
	log *zap.Logger,
) error {
	if srv == nil || serve == nil {
		return fmt.Errorf("http server and serve function are required")
	}
	if log == nil {
		log = zap.NewNop()
	}
	if shutdownTimeout <= 0 {
		shutdownTimeout = defaultShutdownTimeout
	}

	lifecycleCtx, cancelLifecycle := context.WithCancel(ctx)
	defer cancelLifecycle()
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-lifecycleCtx.Done()
		log.Info("http server shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Error("http server shutdown error", zap.Error(err))
		}
		if drain != nil {
			if err := drain(shutdownCtx); err != nil {
				log.Error("chat lifecycle drain failed", zap.Error(err))
			}
		}
	}()

	serveErr := serve()
	// A serve error must not leave the shutdown goroutine waiting forever. This
	// also covers a listener that fails before the process receives SIGTERM.
	cancelLifecycle()
	<-shutdownDone
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		log.Error("http server error", zap.Error(serveErr))
		return fmt.Errorf("http server: %w", serveErr)
	}
	return nil
}
