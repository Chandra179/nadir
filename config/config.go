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
	Source        SourceConfig        `yaml:"source"`
	Ingest        IngestConfig        `yaml:"ingest"`
	Qdrant        QdrantConfig        `yaml:"qdrant"`
	Embedder      EmbedderConfig      `yaml:"embedder"`
	Chunker       ChunkerConfig       `yaml:"chunker"`
	Reranker      RerankerConfig      `yaml:"reranker"`
	SemanticCache SemanticCacheConfig `yaml:"semantic_cache"`
	Generator     GeneratorConfig     `yaml:"generator"`
	History       HistoryConfig       `yaml:"history"`
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

// SourceConfig points to one or more local directories of text files.
type SourceConfig struct {
	Paths []string `yaml:"paths"`
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

// IngestConfig also controls the backoff used for retrying embed calls during ingest.
type IngestConfig struct {
	MaxAttempts     uint64        `yaml:"max_attempts"`
	InitialInterval time.Duration `yaml:"initial_interval"`
	MaxInterval     time.Duration `yaml:"max_interval"`
	Multiplier      float64       `yaml:"multiplier"`
}

type RerankerConfig struct {
	Enabled       bool   `yaml:"enabled"`
	Addr          string `yaml:"addr"`           // sidecar addr, e.g. http://localhost:5002
	CandidateMul  int    `yaml:"candidate_mul"`  // fetch topK*candidate_mul before reranking (default 3)
	MaxConcurrent int    `yaml:"max_concurrent"` // max concurrent reranker calls (default 10)
}

type SemanticCacheConfig struct {
	Enabled    bool          `yaml:"enabled"`
	Collection string        `yaml:"collection"` // Qdrant collection name for cache (default: search_cache)
	Threshold  float32       `yaml:"threshold"`  // cosine similarity cutoff, e.g. 0.90
	TTL        time.Duration `yaml:"ttl"`        // zero = no expiry
}

type GeneratorConfig struct {
	Enabled          bool   `yaml:"enabled"`
	OllamaAddr       string `yaml:"ollama_addr"`        // defaults to embedder.ollama_addr if empty
	Model            string `yaml:"model"`              // LLM model, e.g. llama3.1:8b-instruct-q4_K_M
	MaxContextTokens int    `yaml:"max_context_tokens"` // token budget for retrieved chunks (default 2800)
}

// HistoryConfig persists chat sessions/turns to a dedicated Qdrant
// collection (reuses the same Qdrant instance as document search/semantic
// cache, no extra infra required).
type HistoryConfig struct {
	Enabled    bool   `yaml:"enabled"`
	Collection string `yaml:"collection"`
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
	if v := os.Getenv("RERANKER_ADDR"); v != "" {
		c.Reranker.Addr = v
	}
	if v := os.Getenv("RERANKER_ENABLED"); v != "" {
		c.Reranker.Enabled = v == "true" || v == "1"
	}
	if v := os.Getenv("LOGGER_LEVEL"); v != "" {
		c.Middleware.Logger.Level = v
	}
	if v := os.Getenv("SEMANTIC_CACHE_THRESHOLD"); v != "" {
		if f, err := strconv.ParseFloat(v, 32); err == nil {
			c.SemanticCache.Threshold = float32(f)
		}
	}
	if v := os.Getenv("HISTORY_ENABLED"); v != "" {
		c.History.Enabled = v == "true" || v == "1"
	}
	if v := os.Getenv("HISTORY_COLLECTION"); v != "" {
		c.History.Collection = v
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
	if c.History.Enabled && c.History.Collection == "" {
		c.History.Collection = "chat_history"
	}
	return nil
}
