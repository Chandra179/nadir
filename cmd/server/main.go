package main

import (
	"context"
	"log"
	"net/http"
	_ "net/http/pprof" // registers /debug/pprof/* on the default mux
	"os"
	"os/signal"
	"syscall"

	"nadir/config"
	"nadir/internal/server"
)

// pprofAddr is the dedicated debug listener consumed by external profiling
// tools (e.g. a pipeline.Analyze dashboard fetching /debug/pprof/heap live).
const pprofAddr = ":6063"

func main() {
	cfg, err := config.Load("config/config.yaml")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	go func() {
		log.Printf("pprof listening on %s", pprofAddr)
		if err := http.ListenAndServe(pprofAddr, nil); err != nil {
			log.Printf("pprof server: %v", err)
		}
	}()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	server.Server(ctx, cfg)
}
