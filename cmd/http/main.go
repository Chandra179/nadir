package main

import (
	"log"

	"brook/config"
	"brook/internal/httpserver"
)

func main() {
	cfg, err := config.Load("config/config.yaml")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	httpserver.Server(cfg)
}
