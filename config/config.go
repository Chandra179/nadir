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
	Rewriter      RewriterConfig      `yaml:"rewriter"`
	History       HistoryConfig       `yaml:"history"`
	Enrichment    EnrichmentConfig    `yaml:"enrichment"`

	// Overridden records which config paths applyEnv replaced from the
	// environment at boot (config path → env var name). It is the single
	// source of truth for "where did this value come from" and is not part
	// of the yaml document.
	Overridden map[string]string `yaml:"-" json:"-"`
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
	Provider       string `yaml:"provider"`
	Model          string `yaml:"model"`
	APIKey         string `yaml:"api_key"`
	OllamaAddr     string `yaml:"ollama_addr"`
	Dimensions     int    `yaml:"dimensions"`
	QueryPrefix    string `yaml:"query_prefix"`    // prepended to search queries (e.g. "search_query: " for nomic-embed-text)
	DocumentPrefix string `yaml:"document_prefix"` // prepended to chunks at ingest (e.g. "search_document: ")
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
	Model         string `yaml:"model"`          // cross-encoder the sidecar loads (RERANKER_MODEL; default BAAI/bge-reranker-v2-m3)
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

// RewriterConfig enables conversational query rewriting (Rewrite-Retrieve-
// Read): follow-up turns are rewritten into standalone search queries
// against the session's last N turns. Skipped on the first turn; failures
// fall back to the raw query.
type RewriterConfig struct {
	Enabled    bool   `yaml:"enabled"`
	Turns      int    `yaml:"turns"`       // prior turns fed to the rewriter (default 4)
	OllamaAddr string `yaml:"ollama_addr"` // defaults to generator.ollama_addr, then embedder.ollama_addr
	Model      string `yaml:"model"`       // defaults to generator.model
}

// EnrichmentConfig controls index-time LLM enrichment. Both features cost
// one-time LLM calls per chunk during ingest and add zero query-time
// latency; enabling either requires a reindex to take effect.
type EnrichmentConfig struct {
	Hype       HypeConfig       `yaml:"hype"`
	Contextual ContextualConfig `yaml:"contextual"`
}

// HypeConfig enables HyPE (Hypothetical Prompt Embeddings): N hypothetical
// questions are generated per chunk at ingest and embedded as extra points,
// turning retrieval into question-to-question matching.
type HypeConfig struct {
	Enabled           bool   `yaml:"enabled"`
	QuestionsPerChunk int    `yaml:"questions_per_chunk"` // default 3 when enabled
	OllamaAddr        string `yaml:"ollama_addr"`         // defaults to generator, then embedder addr
	Model             string `yaml:"model"`               // defaults to generator.model
}

// ContextualConfig enables Anthropic-style contextual retrieval: a short
// LLM-written situational summary is prepended to each chunk before it is
// embedded/indexed.
type ContextualConfig struct {
	Enabled    bool   `yaml:"enabled"`
	OllamaAddr string `yaml:"ollama_addr"`
	Model      string `yaml:"model"`
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
// Env vars take precedence over config.yaml values; every applied override
// is recorded in Overridden (keyed by config path) so consumers can tell
// which values came from the environment rather than config.yaml.
func (c *Config) applyEnv() {
	c.envStr(&c.Qdrant.Addr, "qdrant.addr", "QDRANT_ADDR")
	c.envStr(&c.Qdrant.Collection, "qdrant.collection", "QDRANT_COLLECTION")
	c.envStr(&c.Embedder.OllamaAddr, "embedder.ollama_addr", "OLLAMA_ADDR")
	c.envStr(&c.Embedder.APIKey, "embedder.api_key", "EMBEDDER_API_KEY")
	c.envStr(&c.Reranker.Addr, "reranker.addr", "RERANKER_ADDR")
	c.envBool(&c.Reranker.Enabled, "reranker.enabled", "RERANKER_ENABLED")
	c.envStr(&c.Reranker.Model, "reranker.model", "RERANKER_MODEL")
	c.envStr(&c.Middleware.Logger.Level, "middleware.logger.level", "LOGGER_LEVEL")
	c.envFloat32(&c.SemanticCache.Threshold, "semantic_cache.threshold", "SEMANTIC_CACHE_THRESHOLD")
	c.envBool(&c.History.Enabled, "history.enabled", "HISTORY_ENABLED")
	c.envStr(&c.History.Collection, "history.collection", "HISTORY_COLLECTION")
	c.envBool(&c.Enrichment.Hype.Enabled, "enrichment.hype.enabled", "HYPE_ENABLED")
	c.envBool(&c.Enrichment.Contextual.Enabled, "enrichment.contextual.enabled", "CONTEXTUAL_ENABLED")
	c.envBool(&c.Rewriter.Enabled, "rewriter.enabled", "REWRITE_ENABLED")
	c.envStr(&c.Rewriter.OllamaAddr, "rewriter.ollama_addr", "REWRITE_ADDR")
	c.envStr(&c.Rewriter.Model, "rewriter.model", "REWRITE_MODEL")
	c.envInt(&c.Rewriter.Turns, "rewriter.turns", "REWRITE_TURNS")
}

func (c *Config) envStr(dst *string, key, env string) {
	if v := os.Getenv(env); v != "" {
		*dst = v
		c.record(key, env)
	}
}

func (c *Config) envBool(dst *bool, key, env string) {
	if v := os.Getenv(env); v != "" {
		*dst = v == "true" || v == "1"
		c.record(key, env)
	}
}

func (c *Config) envFloat32(dst *float32, key, env string) {
	if v := os.Getenv(env); v != "" {
		if f, err := strconv.ParseFloat(v, 32); err == nil {
			*dst = float32(f)
			c.record(key, env)
		}
	}
}

func (c *Config) envInt(dst *int, key, env string) {
	if v := os.Getenv(env); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			*dst = n
			c.record(key, env)
		}
	}
}

func (c *Config) record(key, env string) {
	if c.Overridden == nil {
		c.Overridden = make(map[string]string)
	}
	c.Overridden[key] = env
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
	if c.Reranker.Model == "" {
		c.Reranker.Model = "BAAI/bge-reranker-v2-m3"
	}
	if c.Enrichment.Hype.Enabled && c.Enrichment.Hype.QuestionsPerChunk <= 0 {
		c.Enrichment.Hype.QuestionsPerChunk = 3
	}
	if c.History.Enabled && c.History.Collection == "" {
		c.History.Collection = "chat_history"
	}
	if c.Rewriter.Enabled && c.Rewriter.Turns <= 0 {
		c.Rewriter.Turns = 4
	}
	return nil
}
