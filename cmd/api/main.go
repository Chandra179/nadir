package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"nadir/internal/platform/configuration"
	"nadir/internal/platform/profiling"
	"nadir/internal/platform/server"
)

func main() {
	cfg, err := config.Load("config/config.yaml")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	stopProfiling, err := profiling.Start(ctx, cfg.Profiling)
	if err != nil {
		log.Fatalf("start profiling: %v", err)
	}
	defer func() {
		if err := stopProfiling(); err != nil {
			log.Printf("stop profiling: %v", err)
		}
	}()

	if err := server.Server(ctx, cfg); err != nil {
		log.Fatalf("server: %v", err)
	}
}
