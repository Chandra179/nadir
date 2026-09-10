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

func TestEndpointResolutionKeepsRoleConfigurationExplicit(t *testing.T) {
	cfg := Config{
		Embedder:  EmbedderConfig{OllamaAddr: "http://embedder:11434"},
		Generator: GeneratorConfig{OllamaAddr: "http://generator:11434", Model: "answer-model"},
		Rewriter:  RewriterConfig{OllamaAddr: "http://rewriter:11434", Model: "rewrite-model"},
		Enrichment: EnrichmentConfig{Contextual: ContextualConfig{
			Enabled:    true,
			OllamaAddr: "http://contextual:11434",
			Model:      "contextual-model",
		}},
	}

	if got := cfg.GeneratorEndpoint(); got.Addr != "http://generator:11434" || got.Model != "answer-model" {
		t.Fatalf("GeneratorEndpoint() = %+v, want explicitly configured generator endpoint", got)
	}
	if got := cfg.RewriterEndpoint(); got.Addr != "http://rewriter:11434" || got.Model != "rewrite-model" {
		t.Fatalf("RewriterEndpoint() = %+v, want explicitly configured rewriter endpoint", got)
	}
	if got := cfg.HypeEndpoint(); got.Addr != "" || got.Model != "" {
		t.Fatalf("HypeEndpoint() = %+v, want empty because Hype is not configured", got)
	}
	if got := cfg.ContextualEndpoint(); got.Addr != "http://contextual:11434" || got.Model != "contextual-model" {
		t.Fatalf("ContextualEndpoint() = %+v, want explicitly configured contextual endpoint", got)
	}
}

func TestRewriterRequiresExplicitEndpointWhenEnabled(t *testing.T) {
	cfg := Config{
		Qdrant:   QdrantConfig{Addr: "qdrant:6334", Collection: "documents", TopK: 1},
		Embedder: EmbedderConfig{Model: "embed", Dimensions: 3},
		Chunker:  ChunkerConfig{ChunkSize: 10},
		Rewriter: RewriterConfig{Enabled: true, Model: "rewrite-model"},
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "rewriter.ollama_addr") {
		t.Fatal("Validate() succeeded with enabled rewriter and empty address")
	}

	cfg.Rewriter.OllamaAddr = "http://rewriter:11434"
	cfg.Rewriter.Model = ""
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "rewriter.model") {
		t.Fatal("Validate() succeeded with enabled rewriter and empty model")
	}
}

func TestEnabledRolesRequireExplicitConfiguration(t *testing.T) {
	tests := []struct {
		name string
		want string
		edit func(*Config)
	}{
		{
			name: "generator address",
			want: "generator.ollama_addr",
			edit: func(c *Config) { c.Generator.Enabled = true; c.Generator.Model = "answer-model" },
		},
		{
			name: "generator model",
			want: "generator.model",
			edit: func(c *Config) {
				c.Generator.Enabled = true
				c.Generator.OllamaAddr = "http://generator:11434"
			},
		},
		{
			name: "hype address",
			want: "enrichment.hype.ollama_addr",
			edit: func(c *Config) { c.Enrichment.Hype.Enabled = true; c.Enrichment.Hype.Model = "hype-model" },
		},
		{
			name: "contextual model",
			want: "enrichment.contextual.model",
			edit: func(c *Config) {
				c.Enrichment.Contextual.Enabled = true
				c.Enrichment.Contextual.OllamaAddr = "http://contextual:11434"
			},
		},
		{
			name: "reranker address",
			want: "reranker.addr",
			edit: func(c *Config) { c.Reranker.Enabled = true; c.Reranker.Model = "reranker-model" },
		},
		{
			name: "reranker model",
			want: "reranker.model",
			edit: func(c *Config) { c.Reranker.Enabled = true; c.Reranker.Addr = "http://reranker:5002" },
		},
		{
			name: "history collection",
			want: "history.collection",
			edit: func(c *Config) { c.History.Enabled = true },
		},
		{
			name: "semantic cache collection",
			want: "semantic_cache.collection",
			edit: func(c *Config) { c.SemanticCache.Enabled = true },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				Qdrant:   QdrantConfig{Addr: "qdrant:6334", Collection: "documents", TopK: 1},
				Embedder: EmbedderConfig{Model: "embed", Dimensions: 3},
				Chunker:  ChunkerConfig{ChunkSize: 10},
			}
			tt.edit(&cfg)
			if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate() error = %v, want %q", err, tt.want)
			}
		})
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
