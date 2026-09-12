package config

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
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
	Search        SearchConfig        `yaml:"search"`
	Reranker      RerankerConfig      `yaml:"reranker"`
	SemanticCache SemanticCacheConfig `yaml:"semantic_cache"`
	Generator     GeneratorConfig     `yaml:"generator"`
	Chat          ChatConfig          `yaml:"chat"`
	Rewriter      RewriterConfig      `yaml:"rewriter"`
	History       HistoryConfig       `yaml:"history"`
	Enrichment    EnrichmentConfig    `yaml:"enrichment"`
	Docling       DoclingConfig       `yaml:"docling"`
}

// ChatConfig tunes the chat use-case (ADR 0006): prompt assembly and the
// streaming turn lifecycle.
type ChatConfig struct {
	MaxContextTokens int           `yaml:"max_context_tokens"` // token budget for retrieved chunks in the prompt (default 2800)
	EventBuffer      int           `yaml:"event_buffer"`
	MaxEventLogBytes int64         `yaml:"max_event_log_bytes"`
	MaxRetainedTurns int           `yaml:"max_retained_turns"`
	FinishedTurnTTL  time.Duration `yaml:"finished_turn_ttl"`
	PersistTimeout   time.Duration `yaml:"persist_timeout"`
}

type HTTPConfig struct {
	Addr            string        `yaml:"addr"`
	ReadTimeout     time.Duration `yaml:"read_timeout"`
	WriteTimeout    time.Duration `yaml:"write_timeout"`
	IdleTimeout     time.Duration `yaml:"idle_timeout"`
	StartupTimeout  time.Duration `yaml:"startup_timeout"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
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
	Paths          []string `yaml:"paths"`
	IgnorePatterns []string `yaml:"ignore_patterns"`
}

// DoclingConfig controls optional PDF document intake through the Docling
// sidecar. The sidecar is not needed for Markdown-only deployments.
type DoclingConfig struct {
	Enabled        bool          `yaml:"enabled"`
	Addr           string        `yaml:"addr"`
	RequestTimeout time.Duration `yaml:"request_timeout"`
}

// OllamaEndpoint is the configured address/model pair for one LLM role.
type OllamaEndpoint struct {
	Addr  string
	Model string
}

type QdrantConfig struct {
	Addr        string `yaml:"addr"`
	Collection  string `yaml:"collection"`
	TopK        int    `yaml:"top_k"`
	PrefetchMul int    `yaml:"prefetch_mul"` // store-level candidate multiplier for hybrid search legs (default 5)
}

type EmbedderConfig struct {
	Provider       string        `yaml:"provider"`
	Model          string        `yaml:"model"`
	APIKey         string        `yaml:"api_key"`
	OllamaAddr     string        `yaml:"ollama_addr"`
	Dimensions     int           `yaml:"dimensions"`
	RequestTimeout time.Duration `yaml:"request_timeout"`
	QueryPrefix    string        `yaml:"query_prefix"`    // prepended to search queries (e.g. "search_query: " for nomic-embed-text)
	DocumentPrefix string        `yaml:"document_prefix"` // prepended to chunks at ingest (e.g. "search_document: ")
}

type ChunkerConfig struct {
	Provider     string `yaml:"provider"`
	ChunkSize    int    `yaml:"chunk_size"`
	ChunkOverlap int    `yaml:"chunk_overlap"`
	WindowSize   int    `yaml:"window_size"` // sentences before+after each sentence; used by sentence-window provider
}

// IngestConfig also controls the backoff used for retrying embed calls during ingest.
type IngestConfig struct {
	MaxAttempts      uint64        `yaml:"max_attempts"`
	InitialInterval  time.Duration `yaml:"initial_interval"`
	MaxInterval      time.Duration `yaml:"max_interval"`
	Multiplier       float64       `yaml:"multiplier"`
	Workers          int           `yaml:"workers"`
	MaxFileBytes     int64         `yaml:"max_file_bytes"`
	MaxUploadBytes   int64         `yaml:"max_upload_bytes"`
	EmbedBatchSize   int           `yaml:"embed_batch_size"`
	MaxChunksPerFile int           `yaml:"max_chunks_per_file"`
}

// SearchConfig bounds work derived from user-controlled queries. These are
// operational limits, not retrieval-quality knobs: they prevent one request
// from creating an unbounded number of embeddings or Qdrant calls.
type SearchConfig struct {
	MaxQueryChars          int `yaml:"max_query_chars"`
	MaxFragments           int `yaml:"max_fragments"`
	MaxConcurrentFragments int `yaml:"max_concurrent_fragments"`
	MaxTopK                int `yaml:"max_top_k"`
	MaxChunksPerFile       int `yaml:"max_chunks_per_file"`
}

type RerankerConfig struct {
	Enabled        bool          `yaml:"enabled"`
	Addr           string        `yaml:"addr"`            // sidecar addr, e.g. http://localhost:5002
	Model          string        `yaml:"model"`           // cross-encoder the sidecar loads (RERANKER_MODEL; default BAAI/bge-reranker-v2-m3)
	CandidateMul   int           `yaml:"candidate_mul"`   // fetch topK*candidate_mul before reranking (default 3)
	MaxConcurrent  int           `yaml:"max_concurrent"`  // max concurrent reranker calls (default 10)
	RequestTimeout time.Duration `yaml:"request_timeout"` // timeout for one sidecar request
}

type SemanticCacheConfig struct {
	Enabled    bool          `yaml:"enabled"`
	Collection string        `yaml:"collection"` // Qdrant collection name for cache (default: search_cache)
	Threshold  float32       `yaml:"threshold"`  // cosine similarity cutoff, e.g. 0.90
	TTL        time.Duration `yaml:"ttl"`        // zero = no expiry
}

type GeneratorConfig struct {
	Enabled        bool          `yaml:"enabled"`
	OllamaAddr     string        `yaml:"ollama_addr"`
	Model          string        `yaml:"model"` // LLM model, e.g. llama3.1:8b-instruct-q4_K_M
	RequestTimeout time.Duration `yaml:"request_timeout"`
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
	Enabled        bool          `yaml:"enabled"`
	Turns          int           `yaml:"turns"` // prior turns fed to the rewriter (default 4)
	RequestTimeout time.Duration `yaml:"request_timeout"`
	OllamaAddr     string        `yaml:"ollama_addr"`
	Model          string        `yaml:"model"`
}

// EnrichmentConfig controls index-time LLM enrichment. Both features cost
// one-time LLM calls per chunk during ingest and add zero query-time
// latency; enabling either requires a reindex to take effect.
type EnrichmentConfig struct {
	RequestTimeout time.Duration    `yaml:"request_timeout"`
	Hype           HypeConfig       `yaml:"hype"`
	Contextual     ContextualConfig `yaml:"contextual"`
}

// HypeConfig enables HyPE (Hypothetical Prompt Embeddings): N hypothetical
// questions are generated per chunk at ingest and embedded as extra points,
// turning retrieval into question-to-question matching.
type HypeConfig struct {
	Enabled           bool   `yaml:"enabled"`
	QuestionsPerChunk int    `yaml:"questions_per_chunk"` // default 3 when enabled
	OllamaAddr        string `yaml:"ollama_addr"`
	Model             string `yaml:"model"`
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
	decoder := yaml.NewDecoder(f)
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return nil, err
	}
	if err := cfg.applyEnv(); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// applyEnv overrides config fields from environment variables.
// Env vars take precedence over config.yaml values.
func (c *Config) applyEnv() error {
	c.envStr(&c.Qdrant.Addr, "QDRANT_ADDR")
	c.envStr(&c.Qdrant.Collection, "QDRANT_COLLECTION")
	c.envStr(&c.Embedder.OllamaAddr, "OLLAMA_ADDR")
	c.envStr(&c.Embedder.APIKey, "EMBEDDER_API_KEY")
	c.envStr(&c.Generator.OllamaAddr, "GENERATOR_ADDR")
	c.envStr(&c.Generator.Model, "GENERATOR_MODEL")
	c.envCSV(&c.Source.Paths, "SOURCE_PATHS")
	c.envCSV(&c.Source.IgnorePatterns, "SOURCE_IGNORE_PATTERNS")
	c.envStr(&c.Reranker.Addr, "RERANKER_ADDR")
	if err := c.envBool(&c.Reranker.Enabled, "RERANKER_ENABLED"); err != nil {
		return err
	}
	c.envStr(&c.Reranker.Model, "RERANKER_MODEL")
	c.envStr(&c.Middleware.Logger.Level, "LOGGER_LEVEL")
	if err := c.envFloat32(&c.SemanticCache.Threshold, "SEMANTIC_CACHE_THRESHOLD"); err != nil {
		return err
	}
	if err := c.envBool(&c.History.Enabled, "HISTORY_ENABLED"); err != nil {
		return err
	}
	c.envStr(&c.History.Collection, "HISTORY_COLLECTION")
	if err := c.envBool(&c.Enrichment.Hype.Enabled, "HYPE_ENABLED"); err != nil {
		return err
	}
	c.envStr(&c.Enrichment.Hype.OllamaAddr, "HYPE_ADDR")
	c.envStr(&c.Enrichment.Hype.Model, "HYPE_MODEL")
	if err := c.envBool(&c.Enrichment.Contextual.Enabled, "CONTEXTUAL_ENABLED"); err != nil {
		return err
	}
	c.envStr(&c.Enrichment.Contextual.OllamaAddr, "CONTEXTUAL_ADDR")
	c.envStr(&c.Enrichment.Contextual.Model, "CONTEXTUAL_MODEL")
	if err := c.envBool(&c.Rewriter.Enabled, "REWRITE_ENABLED"); err != nil {
		return err
	}
	c.envStr(&c.Rewriter.OllamaAddr, "REWRITE_ADDR")
	c.envStr(&c.Rewriter.Model, "REWRITE_MODEL")
	if err := c.envInt(&c.Rewriter.Turns, "REWRITE_TURNS"); err != nil {
		return err
	}
	if err := c.envBool(&c.Docling.Enabled, "DOCLING_ENABLED"); err != nil {
		return err
	}
	c.envStr(&c.Docling.Addr, "DOCLING_ADDR")
	return nil
}

func (c Config) GeneratorEndpoint() OllamaEndpoint {
	return OllamaEndpoint{Addr: c.Generator.OllamaAddr, Model: c.Generator.Model}
}

func (c Config) RewriterEndpoint() OllamaEndpoint {
	return OllamaEndpoint{Addr: c.Rewriter.OllamaAddr, Model: c.Rewriter.Model}
}

func (c Config) HypeEndpoint() OllamaEndpoint {
	return OllamaEndpoint{Addr: c.Enrichment.Hype.OllamaAddr, Model: c.Enrichment.Hype.Model}
}

func (c Config) ContextualEndpoint() OllamaEndpoint {
	return OllamaEndpoint{Addr: c.Enrichment.Contextual.OllamaAddr, Model: c.Enrichment.Contextual.Model}
}

func (c *Config) envStr(dst *string, env string) {
	if v := os.Getenv(env); v != "" {
		*dst = v
	}
}

func (c *Config) envBool(dst *bool, env string) error {
	if v := os.Getenv(env); v != "" {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1":
			*dst = true
		case "false", "0":
			*dst = false
		default:
			return fmt.Errorf("config: %s must be one of true, false, 1, or 0", env)
		}
	}
	return nil
}

func (c *Config) envFloat32(dst *float32, env string) error {
	if v := os.Getenv(env); v != "" {
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 32)
		if err != nil {
			return fmt.Errorf("config: %s must be a number: %w", env, err)
		}
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return fmt.Errorf("config: %s must be a finite number", env)
		}
		*dst = float32(f)
	}
	return nil
}

func (c *Config) envInt(dst *int, env string) error {
	if v := os.Getenv(env); v != "" {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return fmt.Errorf("config: %s must be an integer: %w", env, err)
		}
		*dst = n
	}
	return nil
}

func (c *Config) envCSV(dst *[]string, env string) {
	if v := os.Getenv(env); v != "" {
		parts := strings.Split(v, ",")
		values := make([]string, 0, len(parts))
		for _, part := range parts {
			if part = strings.TrimSpace(part); part != "" {
				values = append(values, part)
			}
		}
		*dst = values
	}
}

// applyDefaults is the single production-defaults table. Package constructors
// still retain defensive defaults for direct unit tests, but a loaded Config
// always gets its runtime defaults here before validation and composition.
func (c *Config) applyDefaults() {
	if c.HTTP.StartupTimeout <= 0 {
		c.HTTP.StartupTimeout = 30 * time.Second
	}
	if c.HTTP.ShutdownTimeout <= 0 {
		c.HTTP.ShutdownTimeout = 10 * time.Second
	}
	if c.Qdrant.PrefetchMul <= 0 {
		c.Qdrant.PrefetchMul = 5
	}
	if c.Embedder.RequestTimeout <= 0 {
		c.Embedder.RequestTimeout = 60 * time.Second
	}
	if c.Ingest.MaxFileBytes <= 0 {
		c.Ingest.MaxFileBytes = 16 << 20
	}
	if c.Ingest.MaxUploadBytes <= 0 {
		c.Ingest.MaxUploadBytes = 64 << 20
	}
	if c.Ingest.Workers <= 0 {
		c.Ingest.Workers = 8
	}
	if c.Ingest.EmbedBatchSize <= 0 {
		c.Ingest.EmbedBatchSize = 64
	}
	if c.Ingest.MaxChunksPerFile <= 0 {
		c.Ingest.MaxChunksPerFile = 10000
	}
	if c.Search.MaxQueryChars <= 0 {
		c.Search.MaxQueryChars = 8192
	}
	if c.Search.MaxFragments <= 0 {
		c.Search.MaxFragments = 16
	}
	if c.Search.MaxConcurrentFragments <= 0 {
		c.Search.MaxConcurrentFragments = 8
	}
	if c.Search.MaxTopK <= 0 {
		c.Search.MaxTopK = 50
	}
	if c.Search.MaxChunksPerFile <= 0 {
		c.Search.MaxChunksPerFile = 3
	}
	if c.Reranker.CandidateMul <= 0 {
		c.Reranker.CandidateMul = 3
	}
	if c.Reranker.MaxConcurrent <= 0 {
		c.Reranker.MaxConcurrent = 10
	}
	if c.Reranker.RequestTimeout <= 0 {
		c.Reranker.RequestTimeout = 30 * time.Second
	}
	if c.Chat.MaxContextTokens <= 0 {
		c.Chat.MaxContextTokens = 2800
	}
	if c.Chat.EventBuffer <= 0 {
		c.Chat.EventBuffer = 4096
	}
	if c.Chat.MaxEventLogBytes <= 0 {
		c.Chat.MaxEventLogBytes = 1 << 20
	}
	if c.Chat.MaxRetainedTurns <= 0 {
		c.Chat.MaxRetainedTurns = 64
	}
	if c.Chat.FinishedTurnTTL <= 0 {
		c.Chat.FinishedTurnTTL = 10 * time.Minute
	}
	if c.Chat.PersistTimeout <= 0 {
		c.Chat.PersistTimeout = 5 * time.Second
	}
	if c.SemanticCache.Threshold == 0 {
		c.SemanticCache.Threshold = 0.90
	}
	if c.Enrichment.RequestTimeout <= 0 {
		c.Enrichment.RequestTimeout = 120 * time.Second
	}
	if c.Generator.RequestTimeout <= 0 {
		c.Generator.RequestTimeout = 120 * time.Second
	}
	if c.Enrichment.Hype.Enabled && c.Enrichment.Hype.QuestionsPerChunk <= 0 {
		c.Enrichment.Hype.QuestionsPerChunk = 3
	}
	if c.Rewriter.Enabled && c.Rewriter.Turns <= 0 {
		c.Rewriter.Turns = 4
	}
	if c.Rewriter.RequestTimeout <= 0 {
		c.Rewriter.RequestTimeout = 8 * time.Second
	}
	if c.Docling.RequestTimeout <= 0 {
		c.Docling.RequestTimeout = 120 * time.Second
	}
}

func (c *Config) Validate() error {
	c.applyDefaults()
	if c.Embedder.Model == "" {
		return fmt.Errorf("config: embedder.model must not be empty")
	}
	if strings.EqualFold(c.Embedder.Provider, "ollama") && strings.TrimSpace(c.Embedder.OllamaAddr) == "" {
		return fmt.Errorf("config: embedder.ollama_addr must not be empty for the ollama provider")
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
	if c.Qdrant.TopK <= 0 {
		return fmt.Errorf("config: qdrant.top_k must be > 0")
	}
	if c.Chunker.ChunkSize <= 0 {
		return fmt.Errorf("config: chunker.chunk_size must be > 0")
	}
	if c.Chunker.ChunkOverlap < 0 || c.Chunker.ChunkOverlap >= c.Chunker.ChunkSize {
		return fmt.Errorf("config: chunker.chunk_overlap must be >= 0 and < chunker.chunk_size")
	}
	if c.Reranker.Enabled && strings.TrimSpace(c.Reranker.Addr) == "" {
		return fmt.Errorf("config: reranker.addr must not be empty when reranker.enabled is true")
	}
	if c.Reranker.Enabled && strings.TrimSpace(c.Reranker.Model) == "" {
		return fmt.Errorf("config: reranker.model must not be empty when reranker.enabled is true")
	}
	if c.Chat.EventBuffer > 16384 {
		return fmt.Errorf("config: chat.event_buffer must be <= 16384")
	}
	if c.Chat.MaxEventLogBytes > 16<<20 {
		return fmt.Errorf("config: chat.max_event_log_bytes must be <= 16777216")
	}
	if c.Chat.MaxRetainedTurns > 1024 {
		return fmt.Errorf("config: chat.max_retained_turns must be <= 1024")
	}
	if c.SemanticCache.Threshold < 0 || c.SemanticCache.Threshold > 1 {
		return fmt.Errorf("config: semantic_cache.threshold must be > 0 and <= 1")
	}
	if c.SemanticCache.Enabled && strings.TrimSpace(c.SemanticCache.Collection) == "" {
		return fmt.Errorf("config: semantic_cache.collection must not be empty when semantic_cache.enabled is true")
	}
	if c.Generator.Enabled && strings.TrimSpace(c.Generator.OllamaAddr) == "" {
		return fmt.Errorf("config: generator.ollama_addr must not be empty when generator.enabled is true")
	}
	if c.Generator.Enabled && strings.TrimSpace(c.Generator.Model) == "" {
		return fmt.Errorf("config: generator.model must not be empty when generator.enabled is true")
	}
	if c.Rewriter.Enabled && strings.TrimSpace(c.Rewriter.OllamaAddr) == "" {
		return fmt.Errorf("config: rewriter.ollama_addr must not be empty when rewriter.enabled is true")
	}
	if c.Rewriter.Enabled && strings.TrimSpace(c.Rewriter.Model) == "" {
		return fmt.Errorf("config: rewriter.model must not be empty when rewriter.enabled is true")
	}
	if c.Enrichment.Hype.Enabled && strings.TrimSpace(c.Enrichment.Hype.OllamaAddr) == "" {
		return fmt.Errorf("config: enrichment.hype.ollama_addr must not be empty when enrichment.hype.enabled is true")
	}
	if c.Enrichment.Hype.Enabled && strings.TrimSpace(c.Enrichment.Hype.Model) == "" {
		return fmt.Errorf("config: enrichment.hype.model must not be empty when enrichment.hype.enabled is true")
	}
	if c.Enrichment.Contextual.Enabled && strings.TrimSpace(c.Enrichment.Contextual.OllamaAddr) == "" {
		return fmt.Errorf("config: enrichment.contextual.ollama_addr must not be empty when enrichment.contextual.enabled is true")
	}
	if c.Enrichment.Contextual.Enabled && strings.TrimSpace(c.Enrichment.Contextual.Model) == "" {
		return fmt.Errorf("config: enrichment.contextual.model must not be empty when enrichment.contextual.enabled is true")
	}
	if c.History.Enabled && strings.TrimSpace(c.History.Collection) == "" {
		return fmt.Errorf("config: history.collection must not be empty when history.enabled is true")
	}
	if c.Docling.Enabled && strings.TrimSpace(c.Docling.Addr) == "" {
		return fmt.Errorf("config: docling.addr must not be empty when docling.enabled is true")
	}
	return nil
}
