package main

import (
	"nadir/internal/bootstrap/configuration"
	"testing"
)

func TestStartupConfigurationUsesEffectiveSettings(t *testing.T) {
	cfg := &config.Config{
		HTTP:     config.HTTPConfig{Addr: ":8200"},
		Reranker: config.RerankerConfig{Enabled: false, Model: "custom-ranker", Addr: "http://localhost:5102"},
		Embedder: config.EmbedderConfig{APIKey: "must-never-be-printed"},
	}
	got := startupConfiguration(cfg)
	if got["api_url"] != "http://127.0.0.1:8200" || got["reranker_enabled"] != false || got["reranker_model"] != "custom-ranker" {
		t.Fatalf("effective startup settings: %v", got)
	}
	if len(got) != 4 {
		t.Fatalf("startup output includes unnecessary fields: %v", got)
	}
}
