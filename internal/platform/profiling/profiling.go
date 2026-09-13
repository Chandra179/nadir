// Package profiling owns the optional local pprof listener used for runtime
// diagnostics. It is deliberately separate from the public API server.
package profiling

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"net/http/pprof"
	"sync"
	"time"

	config "nadir/internal/platform/configuration"
)

const shutdownTimeout = 2 * time.Second

// Start starts the pprof listener when profiling is explicitly enabled. The
// configuration validator guarantees that enabled profiling binds to
// loopback. The returned stop function is safe to call more than once.
func Start(ctx context.Context, cfg config.ProfilingConfig) (func() error, error) {
	if !cfg.Enabled {
		return func() error { return nil }, nil
	}

	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return nil, err
	}
	server := &http.Server{Addr: cfg.Addr, Handler: handler()}
	serveDone := make(chan struct{})
	watchDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("pprof server: %v", err)
		}
	}()

	var once sync.Once
	var stopErr error
	stop := func() error {
		once.Do(func() {
			close(watchDone)
			shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
			defer cancel()
			stopErr = server.Shutdown(shutdownCtx)
			if stopErr != nil {
				_ = server.Close()
			}
			<-serveDone
		})
		return stopErr
	}

	go func() {
		select {
		case <-ctx.Done():
			if err := stop(); err != nil {
				log.Printf("pprof shutdown: %v", err)
			}
		case <-watchDone:
		}
	}()
	return stop, nil
}

func handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	for _, name := range []string{"allocs", "block", "goroutine", "heap", "mutex", "threadcreate"} {
		mux.Handle("/debug/pprof/"+name, pprof.Handler(name))
	}
	return mux
}
