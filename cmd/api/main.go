package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"nadir/internal/bootstrap/configuration"
	"nadir/internal/bootstrap/profiling"
	"nadir/internal/bootstrap/server"
)

func main() {
	configPath := flag.String("config", config.DefaultPath, "path to configuration file")
	printStartup := flag.Bool("startup-config", false, "print the effective local startup settings and exit")
	flag.Parse()
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if *printStartup {
		if err := json.NewEncoder(os.Stdout).Encode(startupConfiguration(cfg)); err != nil {
			log.Fatalf("write startup config: %v", err)
		}
		return
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

// Only fields needed by the local launcher are exposed; provider credentials
// and the complete environment never enter its output.
func startupConfiguration(cfg *config.Config) map[string]any {
	host, port, err := net.SplitHostPort(cfg.HTTP.Addr)
	if err != nil {
		return map[string]any{"error": fmt.Sprintf("invalid HTTP listen address: %v", err)}
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return map[string]any{
		"api_url":          "http://" + net.JoinHostPort(host, port),
		"reranker_enabled": cfg.Reranker.Enabled,
		"reranker_model":   cfg.Reranker.Model,
		"reranker_addr":    cfg.Reranker.Addr,
	}
}
