package main

import (
	"log"

	"brook/config"
	"brook/internal/grpcserver"
)

func main() {
	cfg, err := config.Load("config/config.yaml")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	grpcserver.Server(cfg)
}
