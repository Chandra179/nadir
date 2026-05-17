package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	HTTP          HTTPConfig          `yaml:"http"`
	Middleware    MiddlewareConfig    `yaml:"middleware"`
	KnowledgeBase KnowledgeBaseConfig `yaml:"knowledge_base"`
	PKB           PKBConfig           `yaml:"pkb"`
	Qdrant        QdrantConfig        `yaml:"qdrant"`
	Embedder      EmbedderConfig      `yaml:"embedder"`
	Chunker       ChunkerConfig       `yaml:"chunker"`
	Retry         RetryConfig         `yaml:"retry"`
	SparseScorer  SparseScorerConfig  `yaml:"sparse_scorer"`
	Reranker      RerankerConfig      `yaml:"reranker"`
	HyDE          HyDEConfig          `yaml:"hyde"`
	SemanticCache SemanticCacheConfig `yaml:"semantic_cache"`
	Generator     GeneratorConfig     `yaml:"generator"`
	Docling       DoclingConfig       `yaml:"docling"`
	ChunkFilter   ChunkFilterConfig   `yaml:"chunk_filter"`
}

type HTTPConfig struct {
	Addr         string        `yaml:"addr"`
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`
	IdleTimeout  time.Duration `yaml:"idle_timeout"`
}

type MiddlewareConfig struct {
	Timeout time.Duration `yaml:"timeout"`
	Logger  LoggerConfig  `yaml:"logger"`
}

type LoggerConfig struct {
	Level string `yaml:"level"`
}

// KnowledgeBaseConfig points to one or more local directories of markdown files.
// Paths is the primary list; Path is kept for backward-compat and merged in.
type KnowledgeBaseConfig struct {
	Path  string   `yaml:"path"`  // legacy single-dir; still works
	Paths []string `yaml:"paths"` // additional dirs (merged with Path at load time)
}

// AllPaths returns the deduplicated list of knowledge-base roots.
func (k KnowledgeBaseConfig) AllPaths() []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range append([]string{k.Path}, k.Paths...) {
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

type DoclingConfig struct {
	InputDir  string `yaml:"input_dir"`  // where raw PDFs live
	OutputDir string `yaml:"output_dir"` // where converted .md files are written
}

type QdrantConfig struct {
	Addr        string `yaml:"addr"`
	Collection  string `yaml:"collection"`
	TopK        int    `yaml:"top_k"`
	PrefetchMul int    `yaml:"prefetch_mul"` // store-level candidate multiplier for hybrid search legs (default 5)
}

type EmbedderConfig struct {
	Provider   string `yaml:"provider"`
	Model      string `yaml:"model"`
	APIKey     string `yaml:"api_key"`
	OllamaAddr string `yaml:"ollama_addr"`
	Dimensions int    `yaml:"dimensions"`
}

type ChunkerConfig struct {
	Provider     string `yaml:"provider"`
	ChunkSize    int    `yaml:"chunk_size"`
	ChunkOverlap int    `yaml:"chunk_overlap"`
	WindowSize   int    `yaml:"window_size"` // sentences before+after each sentence; used by sentence-window provider
}

type RetryConfig struct {
	MaxAttempts     uint64        `yaml:"max_attempts"`
	InitialInterval time.Duration `yaml:"initial_interval"`
	MaxInterval     time.Duration `yaml:"max_interval"`
	Multiplier      float64       `yaml:"multiplier"`
}

type PKBConfig struct {
	IgnorePatterns []string `yaml:"ignore_patterns"`
}

type SparseScorerConfig struct {
	Provider string `yaml:"provider"` // "tf" (default) | "splade"
	Addr     string `yaml:"addr"`     // sidecar addr for splade, e.g. http://localhost:5001
}

type RerankerConfig struct {
	Enabled      bool   `yaml:"enabled"`
	Addr         string `yaml:"addr"`          // sidecar addr, e.g. http://localhost:5002
	CandidateMul int    `yaml:"candidate_mul"` // fetch topK*candidate_mul before reranking (default 3)
}

type HyDEConfig struct {
	Enabled        bool    `yaml:"enabled"`
	Adaptive       bool    `yaml:"adaptive"`         // gate HyDE on top-1 score; skip LLM call when confident
	AdaptiveThresh float32 `yaml:"adaptive_thresh"`  // score threshold below which HyDE fires (default 0.5)
	MultiHyDE      bool    `yaml:"multi_hyde"`       // use diverse prompt templates per doc (arxiv 2509.16369); requires num_docs >= 3
	OllamaAddr     string  `yaml:"ollama_addr"`      // defaults to embedder.ollama_addr if empty
	Model          string  `yaml:"model"`            // LLM model for generation, e.g. llama3.1:8b-instruct-q4_K_M
	NumDocs        int     `yaml:"num_docs"`         // hypothetical docs to generate per query (default 1; paper uses 8)
}

type SemanticCacheConfig struct {
	Enabled    bool          `yaml:"enabled"`
	Collection string        `yaml:"collection"` // Qdrant collection name for cache (default: pkb_cache)
	Threshold  float32       `yaml:"threshold"`  // cosine similarity cutoff, e.g. 0.90
	TTL        time.Duration `yaml:"ttl"`        // zero = no expiry
}

type GeneratorConfig struct {
	Enabled          bool   `yaml:"enabled"`
	OllamaAddr       string `yaml:"ollama_addr"`        // defaults to embedder.ollama_addr if empty
	Model            string `yaml:"model"`              // LLM model, e.g. llama3.1:8b-instruct-q4_K_M
	MaxContextTokens int    `yaml:"max_context_tokens"` // token budget for retrieved chunks (default 2800)
}

type ChunkFilterConfig struct {
	Enabled    bool    `yaml:"enabled"`
	OllamaAddr string  `yaml:"ollama_addr"` // defaults to embedder.ollama_addr if empty
	Model      string  `yaml:"model"`
	Threshold  float64 `yaml:"threshold"` // 0–1 relevance cutoff; chunks below are dropped (default 0.5)
}

func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cfg Config
	if err := yaml.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, err
	}
	cfg.applyEnv()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// applyEnv overrides config fields from environment variables.
// Env vars take precedence over config.yaml values.
func (c *Config) applyEnv() {
	if v := os.Getenv("NOTES_PATH"); v != "" {
		c.KnowledgeBase.Path = v
	}
	if v := os.Getenv("QDRANT_ADDR"); v != "" {
		c.Qdrant.Addr = v
	}
	if v := os.Getenv("QDRANT_COLLECTION"); v != "" {
		c.Qdrant.Collection = v
	}
	if v := os.Getenv("OLLAMA_ADDR"); v != "" {
		c.Embedder.OllamaAddr = v
	}
	if v := os.Getenv("EMBEDDER_API_KEY"); v != "" {
		c.Embedder.APIKey = v
	}
	if v := os.Getenv("SPLADE_ADDR"); v != "" {
		c.SparseScorer.Addr = v
	}
	if v := os.Getenv("RERANKER_ADDR"); v != "" {
		c.Reranker.Addr = v
	}
	if v := os.Getenv("RERANKER_ENABLED"); v != "" {
		c.Reranker.Enabled = v == "true" || v == "1"
	}
	if v := os.Getenv("LOGGER_LEVEL"); v != "" {
		c.Middleware.Logger.Level = v
	}
	if v := os.Getenv("HYDE_ENABLED"); v != "" {
		c.HyDE.Enabled = v == "true" || v == "1"
	}
	if v := os.Getenv("HYDE_MODEL"); v != "" {
		c.HyDE.Model = v
	}
	if v := os.Getenv("SEMANTIC_CACHE_THRESHOLD"); v != "" {
		if f, err := strconv.ParseFloat(v, 32); err == nil {
			c.SemanticCache.Threshold = float32(f)
		}
	}
}

func (c *Config) Validate() error {
	if c.Qdrant.TopK <= 0 {
		return fmt.Errorf("config: qdrant.top_k must be > 0")
	}
	if c.Qdrant.PrefetchMul <= 0 {
		c.Qdrant.PrefetchMul = 5
	}
	if c.Embedder.Model == "" {
		return fmt.Errorf("config: embedder.model must not be empty")
	}
	if c.Embedder.Dimensions <= 0 {
		return fmt.Errorf("config: embedder.dimensions must be > 0")
	}
	if c.Qdrant.Addr == "" {
		return fmt.Errorf("config: qdrant.addr must not be empty")
	}
	if c.Qdrant.Collection == "" {
		return fmt.Errorf("config: qdrant.collection must not be empty")
	}
	return nil
}
