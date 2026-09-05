package config

import "testing"

func TestApplyEnvRecordsOverrides(t *testing.T) {
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

	for key, want := range map[string]string{
		"qdrant.addr":              "QDRANT_ADDR",
		"reranker.enabled":         "RERANKER_ENABLED",
		"semantic_cache.threshold": "SEMANTIC_CACHE_THRESHOLD",
		"rewriter.enabled":         "REWRITE_ENABLED",
	} {
		if got := cfg.Overridden[key]; got != want {
			t.Fatalf("Overridden[%q] = %q, want %q", key, got, want)
		}
	}
	if _, ok := cfg.Overridden["embedder.model"]; ok {
		t.Fatal("Overridden[embedder.model] recorded without EMBEDDER env override")
	}
}
