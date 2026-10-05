package main

import (
	"nadir/internal/bootstrap/configuration"
	"testing"
)

func TestStartupConfigurationUsesEffectiveSettings(t *testing.T) {
	cfg := &config.Config{
		HTTP:      config.HTTPConfig{Addr: ":8200"},
		Reranker:  config.RerankerConfig{Enabled: false, Model: "custom-ranker", Addr: "http://localhost:5102"},
		Embedder:  config.EmbedderConfig{APIKey: "must-never-be-printed"},
		Documents: config.DocumentsConfig{Paths: []string{"/private/source"}},
	}
	got := startupConfiguration(cfg)
	if got["api_url"] != "http://127.0.0.1:8200" || got["reranker_enabled"] != false || got["reranker_model"] != "custom-ranker" {
		t.Fatalf("effective startup settings: %v", got)
	}
	if got["documents_paths_configured"] != true {
		t.Fatalf("configured sources must trigger ingestion: %v", got)
	}
	if len(got) != 5 {
		t.Fatalf("startup output includes unnecessary fields: %v", got)
	}
	cfg.Documents.Paths = nil
	if got := startupConfiguration(cfg); got["documents_paths_configured"] != false {
		t.Fatalf("upload-only startup must skip ingestion without sources: %v", got)
	}
}
