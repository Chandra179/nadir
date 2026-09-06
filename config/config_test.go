package config

import "testing"

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
}
