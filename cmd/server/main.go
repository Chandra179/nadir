package main

import (
	"log"

	"nadir/config"
	"nadir/internal/httpserver"
)

func main() {
	cfg, err := config.Load("config/config.yaml")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	httpserver.Server(cfg)
}
