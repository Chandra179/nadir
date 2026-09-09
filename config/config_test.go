package config

import (
	"strings"
	"testing"
)

// TestLoadShippedYAML parses the real config.yaml so a value the decoder
// rejects (e.g. a bare 0 where yaml.v3 expects a duration string like 0s)
// breaks the build's tests instead of server startup.
func TestLoadShippedYAML(t *testing.T) {
	cfg, err := Load("config.yaml")
	if err != nil {
		t.Fatalf("Load(config.yaml): %v", err)
	}
	if cfg.HTTP.WriteTimeout != 0 {
		t.Fatalf("HTTP.WriteTimeout = %v, want 0 (disabled)", cfg.HTTP.WriteTimeout)
	}
	if cfg.Qdrant.TopK <= 0 || cfg.Embedder.Model == "" {
		t.Fatalf("shipped config must validate into a usable state, got %+v", cfg)
	}
}

func TestApplyEnvOverrides(t *testing.T) {
	t.Setenv("QDRANT_ADDR", "qdrant:6334")
	t.Setenv("RERANKER_ENABLED", "false") // explicit false still counts as an env override
	t.Setenv("SEMANTIC_CACHE_THRESHOLD", "0.95")
	t.Setenv("REWRITE_ENABLED", "true")
	t.Setenv("SOURCE_PATHS", "/app/source, /app/extra")

	var cfg Config
	cfg.applyEnv()

	if cfg.Qdrant.Addr != "qdrant:6334" {
		t.Fatalf("Qdrant.Addr = %q, want qdrant:6334", cfg.Qdrant.Addr)
	}
	if cfg.Reranker.Enabled {
		t.Fatal("Reranker.Enabled = true, want false from RERANKER_ENABLED=false")
	}
	if cfg.SemanticCache.Threshold != 0.95 {
		t.Fatalf("SemanticCache.Threshold = %v, want 0.95", cfg.SemanticCache.Threshold)
	}
	if !cfg.Rewriter.Enabled {
		t.Fatal("Rewriter.Enabled = false, want true from REWRITE_ENABLED=true")
	}
	if len(cfg.Source.Paths) != 2 || cfg.Source.Paths[0] != "/app/source" || cfg.Source.Paths[1] != "/app/extra" {
		t.Fatalf("Source.Paths = %#v, want two trimmed paths", cfg.Source.Paths)
	}
}

func TestEndpointResolutionUsesRoleFallbacks(t *testing.T) {
	cfg := Config{
		Embedder:  EmbedderConfig{OllamaAddr: "http://embedder:11434"},
		Generator: GeneratorConfig{Model: "answer-model"},
		Rewriter:  RewriterConfig{},
		Enrichment: EnrichmentConfig{Contextual: ContextualConfig{
			Enabled:    true,
			OllamaAddr: "http://contextual:11434",
			Model:      "contextual-model",
		}},
	}

	if got := cfg.GeneratorEndpoint(); got.Addr != "http://embedder:11434" || got.Model != "answer-model" {
		t.Fatalf("GeneratorEndpoint() = %+v, want embedder addr and generator model", got)
	}
	if got := cfg.RewriterEndpoint(); got.Addr != "http://embedder:11434" || got.Model != "answer-model" {
		t.Fatalf("RewriterEndpoint() = %+v, want generator fallback model and embedder addr", got)
	}
	if got := cfg.EnrichmentEndpoint(); got.Addr != "http://contextual:11434" || got.Model != "contextual-model" {
		t.Fatalf("EnrichmentEndpoint() = %+v, want contextual override", got)
	}
}

func TestDoclingRequiresAddressWhenEnabled(t *testing.T) {
	cfg := Config{
		Qdrant:   QdrantConfig{Addr: "qdrant:6334", Collection: "documents", TopK: 1},
		Embedder: EmbedderConfig{Model: "embed", Dimensions: 3},
		Chunker:  ChunkerConfig{ChunkSize: 10},
		Docling:  DoclingConfig{Enabled: true},
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "docling.addr") {
		t.Fatal("Validate() succeeded with enabled Docling and empty address")
	}
}
