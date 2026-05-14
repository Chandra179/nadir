package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"nadir/config"
	"nadir/internal/httpserver"
)

func main() {
	cfg, err := config.Load("config/config.yaml")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	httpserver.Server(ctx, cfg)
}
