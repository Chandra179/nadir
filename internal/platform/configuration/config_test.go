package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLoadShippedYAML parses the shipped config/config.yaml so a value the decoder
// rejects (e.g. a bare 0 where yaml.v3 expects a duration string like 0s)
// breaks the build's tests instead of server startup.
func TestLoadShippedYAML(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "..", "..", "config", "config.yaml"))
	if err != nil {
		t.Fatalf("Load(config/config.yaml): %v", err)
	}
	if cfg.HTTP.WriteTimeout != 0 {
		t.Fatalf("HTTP.WriteTimeout = %v, want 0 (disabled)", cfg.HTTP.WriteTimeout)
	}
	if cfg.Qdrant.TopK <= 0 || cfg.Embedder.Model == "" {
		t.Fatalf("shipped config must validate into a usable state, got %+v", cfg)
	}
	if cfg.Generator.MaxOutputTokens != 512 {
		t.Fatalf("shipped generator.max_output_tokens = %d, want 512", cfg.Generator.MaxOutputTokens)
	}
	if cfg.Source.Mode != SourceModeUploadOnly {
		t.Fatalf("shipped config source mode = %q, want %q", cfg.Source.Mode, SourceModeUploadOnly)
	}
	if cfg.Inference.Profile != "local" || cfg.Inference.Ollama.MaxConcurrent != 1 ||
		cfg.Inference.Reranker.Device != "cpu" || cfg.Inference.Reranker.MaxConcurrent != 1 {
		t.Fatalf("shipped config must use the conservative local inference profile, got %+v", cfg.Inference)
	}
	if cfg.Search.Fusion.Enabled || cfg.Search.Fusion.RRFK != 60 || cfg.Search.Fusion.DenseWeight != 1 || cfg.Search.Fusion.BM25Weight != 1 {
		t.Fatalf("shipped config must keep calibrated fusion opt-in with explicit baseline knobs, got %+v", cfg.Search.Fusion)
	}
	if cfg.Reranker.AdaptiveEnabled {
		t.Fatal("shipped config must keep adaptive reranking opt-in until release-gated quality evidence exists")
	}
	if cfg.History.SessionPageSize != 50 || cfg.History.TurnPageSize != 500 {
		t.Fatalf("shipped history page sizes = %+v, want 50/500", cfg.History)
	}
	if cfg.Admission.Retrieval.MaxConcurrent != 16 || cfg.Admission.Indexing.MaxConcurrent != 1 || cfg.Admission.Destructive.MaxConcurrent != 1 {
		t.Fatalf("shipped admission budgets = %+v, want explicit process-wide limits", cfg.Admission)
	}
}

func TestApplyEnvOverrides(t *testing.T) {
	t.Setenv("QDRANT_ADDR", "qdrant:6334")
	t.Setenv("EMBEDDER_MODEL", "hf.co/ggml-org/embeddinggemma-300M-GGUF:Q8_0")
	t.Setenv("EMBEDDER_DIMENSIONS", "768")
	t.Setenv("EMBEDDER_QUERY_PREFIX", "task: search result | query: ")
	t.Setenv("EMBEDDER_DOCUMENT_PREFIX", "title: none | text: ")
	t.Setenv("GENERATOR_MAX_OUTPUT_TOKENS", "256")
	t.Setenv("RERANKER_ENABLED", "false") // explicit false still counts as an env override
	t.Setenv("RERANKER_ADAPTIVE_ENABLED", "true")
	t.Setenv("RERANKER_ADAPTIVE_MARGIN_THRESHOLD", "0.02")
	t.Setenv("SEMANTIC_CACHE_THRESHOLD", "0.95")
	t.Setenv("REWRITE_ENABLED", "true")
	t.Setenv("SOURCE_PATHS", "/app/source, /app/extra")
	t.Setenv("SOURCE_MODE", "mirror")
	t.Setenv("FUSION_ENABLED", "true")
	t.Setenv("FUSION_RRF_K", "30")
	t.Setenv("FUSION_DENSE_WEIGHT", "1.5")
	t.Setenv("FUSION_BM25_WEIGHT", "0.75")
	t.Setenv("FUSION_EXACT_MATCH_BOOST", "0.02")
	t.Setenv("FUSION_HEADER_MATCH_BOOST", "0.01")
	t.Setenv("FUSION_MIN_EXACT_TOKENS", "2")
	t.Setenv("FUSION_MIN_HEADER_TOKENS", "1")
	t.Setenv("INFERENCE_OLLAMA_MAX_CONCURRENT", "2")
	t.Setenv("INFERENCE_OLLAMA_KEEP_ALIVE", "90s")
	t.Setenv("RERANKER_DEVICE", "cuda")
	t.Setenv("HISTORY_SESSION_PAGE_SIZE", "75")
	t.Setenv("ADMISSION_RETRIEVAL_MAX_CONCURRENT", "4")
	t.Setenv("ADMISSION_RETRIEVAL_QUEUE_TIMEOUT", "2s")

	var cfg Config
	if err := cfg.applyEnv(); err != nil {
		t.Fatal(err)
	}

	if cfg.Qdrant.Addr != "qdrant:6334" {
		t.Fatalf("Qdrant.Addr = %q, want qdrant:6334", cfg.Qdrant.Addr)
	}
	if cfg.Embedder.Model != "hf.co/ggml-org/embeddinggemma-300M-GGUF:Q8_0" ||
		cfg.Embedder.Dimensions != 768 ||
		cfg.Embedder.QueryPrefix != "task: search result | query: " ||
		cfg.Embedder.DocumentPrefix != "title: none | text: " {
		t.Fatalf("Embedder overrides = %+v, want explicit model, dimensions, and prompts", cfg.Embedder)
	}
	if cfg.Generator.MaxOutputTokens != 256 {
		t.Fatalf("Generator.MaxOutputTokens = %d, want 256", cfg.Generator.MaxOutputTokens)
	}
	if cfg.Reranker.Enabled {
		t.Fatal("Reranker.Enabled = true, want false from RERANKER_ENABLED=false")
	}
	if !cfg.Reranker.AdaptiveEnabled || cfg.Reranker.AdaptiveMarginThreshold != 0.02 {
		t.Fatalf("adaptive reranker overrides = %+v, want enabled and threshold 0.02", cfg.Reranker)
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
	if cfg.Source.Mode != SourceModeMirror {
		t.Fatalf("Source.Mode = %q, want mirror", cfg.Source.Mode)
	}
	if !cfg.Search.Fusion.Enabled || cfg.Search.Fusion.RRFK != 30 || cfg.Search.Fusion.DenseWeight != 1.5 || cfg.Search.Fusion.BM25Weight != 0.75 ||
		cfg.Search.Fusion.ExactMatchBoost != 0.02 || cfg.Search.Fusion.HeaderMatchBoost != 0.01 || cfg.Search.Fusion.MinExactTokens != 2 {
		t.Fatalf("Fusion overrides = %+v, want explicit calibrated knobs", cfg.Search.Fusion)
	}
	if cfg.Inference.Ollama.MaxConcurrent != 2 || cfg.Inference.Ollama.KeepAlive != 90*time.Second {
		t.Fatalf("Ollama resource overrides = %+v, want max=2 and keep_alive=90s", cfg.Inference.Ollama)
	}
	if cfg.Inference.Reranker.Device != "cuda" {
		t.Fatalf("Reranker.Device = %q, want cuda", cfg.Inference.Reranker.Device)
	}
	if cfg.History.SessionPageSize != 75 || cfg.Admission.Retrieval.MaxConcurrent != 4 || cfg.Admission.Retrieval.QueueTimeout != 2*time.Second {
		t.Fatalf("operational overrides = history=%+v admission=%+v", cfg.History, cfg.Admission.Retrieval)
	}
}

func TestApplyEnvRejectsMalformedValues(t *testing.T) {
	for _, tt := range []struct {
		name  string
		env   string
		value string
	}{
		{name: "bool", env: "RERANKER_ENABLED", value: "sometimes"},
		{name: "float", env: "SEMANTIC_CACHE_THRESHOLD", value: "high"},
		{name: "non-finite float", env: "SEMANTIC_CACHE_THRESHOLD", value: "NaN"},
		{name: "int", env: "REWRITE_TURNS", value: "many"},
		{name: "duration", env: "INFERENCE_OLLAMA_KEEP_ALIVE", value: "soon"},
		{name: "embedder dimensions", env: "EMBEDDER_DIMENSIONS", value: "wide"},
		{name: "generator max output tokens", env: "GENERATOR_MAX_OUTPUT_TOKENS", value: "many"},
		{name: "adaptive margin", env: "RERANKER_ADAPTIVE_MARGIN_THRESHOLD", value: "wide"},
		{name: "admission timeout", env: "ADMISSION_INDEXING_QUEUE_TIMEOUT", value: "soon"},
		{name: "history page size", env: "HISTORY_SESSION_PAGE_SIZE", value: "many"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.env, tt.value)
			var cfg Config
			if err := cfg.applyEnv(); err == nil || !strings.Contains(err.Error(), tt.env) {
				t.Fatalf("applyEnv() error = %v, want malformed %s error", err, tt.env)
			}
		})
	}
}

func TestValidateRejectsInvalidAdaptiveRerankMargin(t *testing.T) {
	cfg := Config{
		Qdrant:   QdrantConfig{Addr: "qdrant:6334", Collection: "documents", TopK: 1},
		Embedder: EmbedderConfig{Model: "embed", Dimensions: 3},
		Chunker:  ChunkerConfig{ChunkSize: 10},
		Reranker: RerankerConfig{AdaptiveMarginThreshold: 1.1},
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "adaptive_margin_threshold") {
		t.Fatalf("Validate() error = %v, want adaptive margin validation error", err)
	}
}

func TestLoadRejectsUnknownYAMLFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("unknown_field: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "unknown_field") {
		t.Fatalf("Load() error = %v, want unknown field error", err)
	}
}

func TestValidateCentralizesProductionDefaults(t *testing.T) {
	cfg := Config{
		Qdrant:   QdrantConfig{Addr: "qdrant:6334", Collection: "documents", TopK: 1},
		Embedder: EmbedderConfig{Provider: "ollama", OllamaAddr: "http://ollama:11434", Model: "embed", Dimensions: 3},
		Chunker:  ChunkerConfig{ChunkSize: 10},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.HTTP.StartupTimeout != 30*time.Second || cfg.Search.MaxTopK != 50 || cfg.Reranker.CandidateMul != 3 ||
		cfg.Inference.Ollama.MaxConcurrent != 1 || cfg.Inference.Reranker.Device != "cpu" {
		t.Fatalf("defaults not applied centrally: %+v", cfg)
	}
}

func TestValidateRejectsImplicitLocalRerankerDevice(t *testing.T) {
	cfg := Config{
		Qdrant:   QdrantConfig{Addr: "qdrant:6334", Collection: "documents", TopK: 1},
		Embedder: EmbedderConfig{Model: "embed", Dimensions: 3},
		Chunker:  ChunkerConfig{ChunkSize: 10},
		Inference: InferenceConfig{
			Profile:  "local",
			Reranker: RerankerResourceConfig{Device: "auto"},
		},
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "device must be explicit") {
		t.Fatalf("Validate() error = %v, want explicit local device error", err)
	}
}

func TestValidateRejectsInvalidSourceMode(t *testing.T) {
	cfg := Config{Source: SourceConfig{Mode: "delete-everything"}}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "source.mode") {
		t.Fatalf("Validate() error = %v, want source mode validation error", err)
	}
}

func TestValidateRequiresRootsForMirroredSources(t *testing.T) {
	cfg := Config{Source: SourceConfig{Mode: SourceModeMirror}}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "source.paths") {
		t.Fatalf("Validate() error = %v, want mirrored source root validation error", err)
	}
}

func TestProfilingIsDisabledByDefaultAndLoopbackOnlyWhenEnabled(t *testing.T) {
	cfg := Config{
		Qdrant:   QdrantConfig{Addr: "qdrant:6334", Collection: "documents", TopK: 1},
		Embedder: EmbedderConfig{Provider: "ollama", OllamaAddr: "http://ollama:11434", Model: "embed", Dimensions: 3},
		Chunker:  ChunkerConfig{ChunkSize: 10},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.Profiling.Enabled || cfg.Profiling.Addr != "127.0.0.1:6063" {
		t.Fatalf("profiling defaults = %+v, want disabled loopback listener", cfg.Profiling)
	}

	cfg.Profiling.Enabled = true
	cfg.Profiling.Addr = "0.0.0.0:6063"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "profiling.addr") {
		t.Fatalf("Validate() error = %v, want public profiling address rejection", err)
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
