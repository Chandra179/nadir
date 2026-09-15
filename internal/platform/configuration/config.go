// Package config loads, validates, and normalizes application configuration.
package config

import (
	"fmt"
	"math"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the complete application configuration loaded from YAML and
// environment overrides.
type Config struct {
	HTTP          HTTPConfig          `yaml:"http"`
	Profiling     ProfilingConfig     `yaml:"profiling"`
	Middleware    MiddlewareConfig    `yaml:"middleware"`
	Source        SourceConfig        `yaml:"source"`
	Ingest        IngestConfig        `yaml:"ingest"`
	Qdrant        QdrantConfig        `yaml:"qdrant"`
	Embedder      EmbedderConfig      `yaml:"embedder"`
	Chunker       ChunkerConfig       `yaml:"chunker"`
	Search        SearchConfig        `yaml:"search"`
	Reranker      RerankerConfig      `yaml:"reranker"`
	Inference     InferenceConfig     `yaml:"inference"`
	Admission     AdmissionConfig     `yaml:"admission"`
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

// HTTPConfig controls the API listener and its request/server timeouts.
type HTTPConfig struct {
	Addr             string        `yaml:"addr"`
	ReadTimeout      time.Duration `yaml:"read_timeout"`
	WriteTimeout     time.Duration `yaml:"write_timeout"`
	IdleTimeout      time.Duration `yaml:"idle_timeout"`
	StartupTimeout   time.Duration `yaml:"startup_timeout"`
	ShutdownTimeout  time.Duration `yaml:"shutdown_timeout"`
	ReadinessTimeout time.Duration `yaml:"readiness_timeout"`
}

// ProfilingConfig controls the optional local pprof listener. Profiling is
// disabled by default and, when enabled, must bind to loopback so diagnostic
// endpoints are not exposed as part of the API surface.
type ProfilingConfig struct {
	Enabled bool   `yaml:"enabled"`
	Addr    string `yaml:"addr"`
}

// MiddlewareConfig controls cross-cutting HTTP middleware.
type MiddlewareConfig struct {
	Timeout time.Duration `yaml:"timeout"`
	Logger  LoggerConfig  `yaml:"logger"`
}

// LoggerConfig controls structured log output.
type LoggerConfig struct {
	Level string `yaml:"level"`
}

// SourceConfig points to one or more local directories of text files.
type SourceConfig struct {
	Paths          []string `yaml:"paths"`
	IgnorePatterns []string `yaml:"ignore_patterns"`
	Mode           string   `yaml:"mode"` // upload-only | mirror
}

const (
	SourceModeUploadOnly = "upload-only"
	SourceModeMirror     = "mirror"
)

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

// QdrantConfig identifies the Qdrant collection and retrieval defaults.
type QdrantConfig struct {
	Addr        string `yaml:"addr"`
	Collection  string `yaml:"collection"`
	TopK        int    `yaml:"top_k"`
	PrefetchMul int    `yaml:"prefetch_mul"` // store-level candidate multiplier for hybrid search legs (default 5)
}

// EmbedderConfig selects the embedding provider and vector contract.
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

// ChunkerConfig selects the document chunking strategy and its bounds.
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
	MaxQueryChars          int          `yaml:"max_query_chars"`
	MaxFragments           int          `yaml:"max_fragments"`
	MaxConcurrentFragments int          `yaml:"max_concurrent_fragments"`
	MaxTopK                int          `yaml:"max_top_k"`
	MaxChunksPerFile       int          `yaml:"max_chunks_per_file"`
	Fusion                 FusionConfig `yaml:"fusion"`
}

// FusionConfig controls the opt-in Retrieval-side calibrated fusion path.
// Values are rank weights and small lexical boosts; raw dense/BM25 scores are
// never combined because their scales are provider-specific.
type FusionConfig struct {
	Enabled          bool                           `yaml:"enabled"`
	RRFK             int                            `yaml:"rrf_k"`
	DenseWeight      float32                        `yaml:"dense_weight"`
	BM25Weight       float32                        `yaml:"bm25_weight"`
	ExactMatchBoost  float32                        `yaml:"exact_match_boost"`
	HeaderMatchBoost float32                        `yaml:"header_match_boost"`
	MinExactTokens   int                            `yaml:"min_exact_tokens"`
	MinHeaderTokens  int                            `yaml:"min_header_tokens"`
	Profiles         map[string]FusionProfileConfig `yaml:"profiles"`
}

type FusionProfileConfig struct {
	DenseWeight      float32 `yaml:"dense_weight"`
	BM25Weight       float32 `yaml:"bm25_weight"`
	ExactMatchBoost  float32 `yaml:"exact_match_boost"`
	HeaderMatchBoost float32 `yaml:"header_match_boost"`
	MinExactTokens   int     `yaml:"min_exact_tokens"`
	MinHeaderTokens  int     `yaml:"min_header_tokens"`
}

// RerankerConfig controls the optional cross-encoder reranker Adapter.
type RerankerConfig struct {
	Enabled                 bool          `yaml:"enabled"`
	Addr                    string        `yaml:"addr"`                      // sidecar addr, e.g. http://localhost:5002
	Model                   string        `yaml:"model"`                     // cross-encoder the sidecar loads (RERANKER_MODEL; default BAAI/bge-reranker-v2-m3)
	CandidateMul            int           `yaml:"candidate_mul"`             // fetch topK*candidate_mul before reranking (default 3)
	RequestTimeout          time.Duration `yaml:"request_timeout"`           // timeout for one sidecar request
	AdaptiveEnabled         bool          `yaml:"adaptive_enabled"`          // invoke the reranker only for low-confidence hybrid results
	AdaptiveMarginThreshold float32       `yaml:"adaptive_margin_threshold"` // relative fused top-result margin below which reranking is required
}

// InferenceConfig defines the local model resource profile. It is process-local
// admission control; it does not coordinate multiple API instances.
type InferenceConfig struct {
	Profile  string                 `yaml:"profile"`
	Ollama   OllamaResourceConfig   `yaml:"ollama"`
	Reranker RerankerResourceConfig `yaml:"reranker"`
}

// AdmissionConfig defines process-wide backpressure budgets. These budgets
// are separate from the model-resource Gate because they describe operation
// ownership, not only hardware concurrency.
type AdmissionConfig struct {
	Retrieval   AdmissionOperationConfig `yaml:"retrieval"`
	Reranking   AdmissionOperationConfig `yaml:"reranking"`
	Generation  AdmissionOperationConfig `yaml:"generation"`
	Embedding   AdmissionOperationConfig `yaml:"embedding"`
	Indexing    AdmissionOperationConfig `yaml:"indexing"`
	Destructive AdmissionOperationConfig `yaml:"destructive"`
}

// AdmissionOperationConfig bounds one process-wide operation budget.
type AdmissionOperationConfig struct {
	MaxConcurrent int           `yaml:"max_concurrent"`
	QueueTimeout  time.Duration `yaml:"queue_timeout"`
}

// OllamaResourceConfig bounds all Ollama roles together. Holding the Gate for
// the full request, including a streaming response, prevents model overlap.
type OllamaResourceConfig struct {
	MaxConcurrent int           `yaml:"max_concurrent"`
	QueueTimeout  time.Duration `yaml:"queue_timeout"`
	KeepAlive     time.Duration `yaml:"keep_alive"`
}

// RerankerResourceConfig controls the separate reranker process and its
// explicit device/backend policy.
type RerankerResourceConfig struct {
	MaxConcurrent int           `yaml:"max_concurrent"`
	QueueTimeout  time.Duration `yaml:"queue_timeout"`
	Device        string        `yaml:"device"`
	Backend       string        `yaml:"backend"`
}

// SemanticCacheConfig controls persistence and matching for cached searches.
type SemanticCacheConfig struct {
	Enabled    bool          `yaml:"enabled"`
	Collection string        `yaml:"collection"` // Qdrant collection name for cache (default: search_cache)
	Threshold  float32       `yaml:"threshold"`  // cosine similarity cutoff, e.g. 0.90
	TTL        time.Duration `yaml:"ttl"`        // zero = no expiry
}

// GeneratorConfig controls the optional answer-generation LLM.
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
	Enabled         bool   `yaml:"enabled"`
	Collection      string `yaml:"collection"`
	SessionPageSize int    `yaml:"session_page_size"`
	TurnPageSize    int    `yaml:"turn_page_size"`
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
	if err := c.envBool(&c.Profiling.Enabled, "PROFILING_ENABLED"); err != nil {
		return err
	}
	c.envStr(&c.Profiling.Addr, "PROFILING_ADDR")
	c.envStr(&c.Qdrant.Collection, "QDRANT_COLLECTION")
	c.envStr(&c.Embedder.OllamaAddr, "OLLAMA_ADDR")
	c.envStr(&c.Embedder.Model, "EMBEDDER_MODEL")
	c.envStr(&c.Embedder.APIKey, "EMBEDDER_API_KEY")
	if err := c.envInt(&c.Embedder.Dimensions, "EMBEDDER_DIMENSIONS"); err != nil {
		return err
	}
	c.envStr(&c.Embedder.QueryPrefix, "EMBEDDER_QUERY_PREFIX")
	c.envStr(&c.Embedder.DocumentPrefix, "EMBEDDER_DOCUMENT_PREFIX")
	c.envStr(&c.Generator.OllamaAddr, "GENERATOR_ADDR")
	c.envStr(&c.Generator.Model, "GENERATOR_MODEL")
	c.envCSV(&c.Source.Paths, "SOURCE_PATHS")
	c.envCSV(&c.Source.IgnorePatterns, "SOURCE_IGNORE_PATTERNS")
	c.envStr(&c.Source.Mode, "SOURCE_MODE")
	if err := c.envBool(&c.Search.Fusion.Enabled, "FUSION_ENABLED"); err != nil {
		return err
	}
	if err := c.envInt(&c.Search.Fusion.RRFK, "FUSION_RRF_K"); err != nil {
		return err
	}
	if err := c.envFloat32(&c.Search.Fusion.DenseWeight, "FUSION_DENSE_WEIGHT"); err != nil {
		return err
	}
	if err := c.envFloat32(&c.Search.Fusion.BM25Weight, "FUSION_BM25_WEIGHT"); err != nil {
		return err
	}
	if err := c.envFloat32(&c.Search.Fusion.ExactMatchBoost, "FUSION_EXACT_MATCH_BOOST"); err != nil {
		return err
	}
	if err := c.envFloat32(&c.Search.Fusion.HeaderMatchBoost, "FUSION_HEADER_MATCH_BOOST"); err != nil {
		return err
	}
	if err := c.envInt(&c.Search.Fusion.MinExactTokens, "FUSION_MIN_EXACT_TOKENS"); err != nil {
		return err
	}
	if err := c.envInt(&c.Search.Fusion.MinHeaderTokens, "FUSION_MIN_HEADER_TOKENS"); err != nil {
		return err
	}
	c.envStr(&c.Reranker.Addr, "RERANKER_ADDR")
	if err := c.envBool(&c.Reranker.Enabled, "RERANKER_ENABLED"); err != nil {
		return err
	}
	c.envStr(&c.Reranker.Model, "RERANKER_MODEL")
	if err := c.envBool(&c.Reranker.AdaptiveEnabled, "RERANKER_ADAPTIVE_ENABLED"); err != nil {
		return err
	}
	if err := c.envFloat32(&c.Reranker.AdaptiveMarginThreshold, "RERANKER_ADAPTIVE_MARGIN_THRESHOLD"); err != nil {
		return err
	}
	c.envStr(&c.Inference.Reranker.Device, "RERANKER_DEVICE")
	c.envStr(&c.Inference.Reranker.Backend, "RERANKER_BACKEND")
	if err := c.envInt(&c.Inference.Reranker.MaxConcurrent, "RERANKER_MAX_CONCURRENT"); err != nil {
		return err
	}
	if err := c.envDuration(&c.Inference.Reranker.QueueTimeout, "RERANKER_QUEUE_TIMEOUT"); err != nil {
		return err
	}
	c.envStr(&c.Inference.Profile, "INFERENCE_PROFILE")
	if err := c.envInt(&c.Inference.Ollama.MaxConcurrent, "INFERENCE_OLLAMA_MAX_CONCURRENT"); err != nil {
		return err
	}
	if err := c.envDuration(&c.Inference.Ollama.QueueTimeout, "INFERENCE_OLLAMA_QUEUE_TIMEOUT"); err != nil {
		return err
	}
	if err := c.envDuration(&c.Inference.Ollama.KeepAlive, "INFERENCE_OLLAMA_KEEP_ALIVE"); err != nil {
		return err
	}
	if err := c.applyAdmissionEnv(); err != nil {
		return err
	}
	c.envStr(&c.Middleware.Logger.Level, "LOGGER_LEVEL")
	if err := c.envFloat32(&c.SemanticCache.Threshold, "SEMANTIC_CACHE_THRESHOLD"); err != nil {
		return err
	}
	if err := c.envBool(&c.History.Enabled, "HISTORY_ENABLED"); err != nil {
		return err
	}
	c.envStr(&c.History.Collection, "HISTORY_COLLECTION")
	if err := c.envInt(&c.History.SessionPageSize, "HISTORY_SESSION_PAGE_SIZE"); err != nil {
		return err
	}
	if err := c.envInt(&c.History.TurnPageSize, "HISTORY_TURN_PAGE_SIZE"); err != nil {
		return err
	}
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

func (c *Config) applyAdmissionEnv() error {
	for _, item := range []struct {
		cfg     *AdmissionOperationConfig
		maxEnv  string
		timeEnv string
	}{
		{&c.Admission.Retrieval, "ADMISSION_RETRIEVAL_MAX_CONCURRENT", "ADMISSION_RETRIEVAL_QUEUE_TIMEOUT"},
		{&c.Admission.Reranking, "ADMISSION_RERANKING_MAX_CONCURRENT", "ADMISSION_RERANKING_QUEUE_TIMEOUT"},
		{&c.Admission.Generation, "ADMISSION_GENERATION_MAX_CONCURRENT", "ADMISSION_GENERATION_QUEUE_TIMEOUT"},
		{&c.Admission.Embedding, "ADMISSION_EMBEDDING_MAX_CONCURRENT", "ADMISSION_EMBEDDING_QUEUE_TIMEOUT"},
		{&c.Admission.Indexing, "ADMISSION_INDEXING_MAX_CONCURRENT", "ADMISSION_INDEXING_QUEUE_TIMEOUT"},
		{&c.Admission.Destructive, "ADMISSION_DESTRUCTIVE_MAX_CONCURRENT", "ADMISSION_DESTRUCTIVE_QUEUE_TIMEOUT"},
	} {
		if err := c.envInt(&item.cfg.MaxConcurrent, item.maxEnv); err != nil {
			return err
		}
		if err := c.envDuration(&item.cfg.QueueTimeout, item.timeEnv); err != nil {
			return err
		}
	}
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

func (c *Config) envDuration(dst *time.Duration, env string) error {
	if v := os.Getenv(env); v != "" {
		d, err := time.ParseDuration(strings.TrimSpace(v))
		if err != nil {
			return fmt.Errorf("config: %s must be a duration: %w", env, err)
		}
		*dst = d
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
	if strings.TrimSpace(c.Profiling.Addr) == "" {
		c.Profiling.Addr = "127.0.0.1:6063"
	}
	if strings.TrimSpace(c.Source.Mode) == "" {
		c.Source.Mode = SourceModeUploadOnly
	}
	if c.HTTP.StartupTimeout <= 0 {
		c.HTTP.StartupTimeout = 30 * time.Second
	}
	if c.HTTP.ShutdownTimeout <= 0 {
		c.HTTP.ShutdownTimeout = 10 * time.Second
	}
	if c.HTTP.ReadinessTimeout <= 0 {
		c.HTTP.ReadinessTimeout = 15 * time.Second
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
	if c.Search.Fusion.RRFK <= 0 {
		c.Search.Fusion.RRFK = 60
	}
	if c.Search.Fusion.DenseWeight <= 0 {
		c.Search.Fusion.DenseWeight = 1
	}
	if c.Search.Fusion.BM25Weight <= 0 {
		c.Search.Fusion.BM25Weight = 1
	}
	if c.Search.Fusion.MinExactTokens <= 0 {
		c.Search.Fusion.MinExactTokens = 1
	}
	if c.Search.Fusion.MinHeaderTokens <= 0 {
		c.Search.Fusion.MinHeaderTokens = 1
	}
	if c.Reranker.CandidateMul <= 0 {
		c.Reranker.CandidateMul = 3
	}
	if c.Reranker.RequestTimeout <= 0 {
		c.Reranker.RequestTimeout = 30 * time.Second
	}
	if c.Reranker.AdaptiveMarginThreshold <= 0 {
		c.Reranker.AdaptiveMarginThreshold = 0.01
	}
	if strings.TrimSpace(c.Inference.Profile) == "" {
		c.Inference.Profile = "local"
	}
	if c.Inference.Ollama.MaxConcurrent == 0 {
		c.Inference.Ollama.MaxConcurrent = 1
	}
	if c.Inference.Ollama.QueueTimeout == 0 {
		c.Inference.Ollama.QueueTimeout = 30 * time.Second
	}
	if c.Inference.Ollama.KeepAlive == 0 {
		c.Inference.Ollama.KeepAlive = 5 * time.Minute
	}
	if c.Inference.Reranker.MaxConcurrent == 0 {
		c.Inference.Reranker.MaxConcurrent = 1
	}
	if c.Inference.Reranker.QueueTimeout == 0 {
		c.Inference.Reranker.QueueTimeout = 30 * time.Second
	}
	if strings.TrimSpace(c.Inference.Reranker.Device) == "" {
		c.Inference.Reranker.Device = "cpu"
	}
	if strings.TrimSpace(c.Inference.Reranker.Backend) == "" {
		c.Inference.Reranker.Backend = "torch"
	}
	if c.Admission.Retrieval.MaxConcurrent == 0 {
		c.Admission.Retrieval.MaxConcurrent = 16
	}
	if c.Admission.Retrieval.QueueTimeout == 0 {
		c.Admission.Retrieval.QueueTimeout = 5 * time.Second
	}
	if c.Admission.Reranking.MaxConcurrent == 0 {
		c.Admission.Reranking.MaxConcurrent = 1
	}
	if c.Admission.Reranking.QueueTimeout == 0 {
		c.Admission.Reranking.QueueTimeout = 30 * time.Second
	}
	if c.Admission.Generation.MaxConcurrent == 0 {
		c.Admission.Generation.MaxConcurrent = 1
	}
	if c.Admission.Generation.QueueTimeout == 0 {
		c.Admission.Generation.QueueTimeout = 30 * time.Second
	}
	if c.Admission.Embedding.MaxConcurrent == 0 {
		c.Admission.Embedding.MaxConcurrent = 1
	}
	if c.Admission.Embedding.QueueTimeout == 0 {
		c.Admission.Embedding.QueueTimeout = 30 * time.Second
	}
	if c.Admission.Indexing.MaxConcurrent == 0 {
		c.Admission.Indexing.MaxConcurrent = 1
	}
	if c.Admission.Indexing.QueueTimeout == 0 {
		c.Admission.Indexing.QueueTimeout = 5 * time.Second
	}
	if c.Admission.Destructive.MaxConcurrent == 0 {
		c.Admission.Destructive.MaxConcurrent = 1
	}
	if c.Admission.Destructive.QueueTimeout == 0 {
		c.Admission.Destructive.QueueTimeout = 10 * time.Second
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
	if c.History.SessionPageSize <= 0 {
		c.History.SessionPageSize = 50
	}
	if c.History.TurnPageSize <= 0 {
		c.History.TurnPageSize = 500
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
	if c.Profiling.Enabled && !isLoopbackAddr(c.Profiling.Addr) {
		return fmt.Errorf("config: profiling.addr must bind to loopback when profiling.enabled is true")
	}
	switch c.Source.Mode {
	case SourceModeUploadOnly:
	case SourceModeMirror:
		if len(c.Source.Paths) == 0 {
			return fmt.Errorf("config: source.paths must not be empty when source.mode is mirror")
		}
	default:
		return fmt.Errorf("config: source.mode must be upload-only or mirror")
	}
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
	if c.Search.Fusion.RRFK <= 0 || c.Search.Fusion.DenseWeight <= 0 || c.Search.Fusion.BM25Weight <= 0 {
		return fmt.Errorf("config: search.fusion.rrf_k and rank weights must be > 0")
	}
	if c.Search.Fusion.ExactMatchBoost < 0 || c.Search.Fusion.HeaderMatchBoost < 0 ||
		math.IsNaN(float64(c.Search.Fusion.ExactMatchBoost)) || math.IsInf(float64(c.Search.Fusion.ExactMatchBoost), 0) ||
		math.IsNaN(float64(c.Search.Fusion.HeaderMatchBoost)) || math.IsInf(float64(c.Search.Fusion.HeaderMatchBoost), 0) {
		return fmt.Errorf("config: search.fusion boosts must be finite and >= 0")
	}
	if c.Search.Fusion.MinExactTokens <= 0 || c.Search.Fusion.MinHeaderTokens <= 0 {
		return fmt.Errorf("config: search.fusion token thresholds must be > 0")
	}
	for name, profile := range c.Search.Fusion.Profiles {
		switch name {
		case "factoid", "formula", "procedure", "comparison", "multi_hop":
		default:
			return fmt.Errorf("config: search.fusion.profiles.%s is not a supported query type", name)
		}
		if profile.DenseWeight <= 0 || profile.BM25Weight <= 0 || profile.MinExactTokens <= 0 || profile.MinHeaderTokens <= 0 ||
			profile.ExactMatchBoost < 0 || profile.HeaderMatchBoost < 0 {
			return fmt.Errorf("config: search.fusion.profiles.%s has invalid weights, boosts, or thresholds", name)
		}
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
	if c.Reranker.AdaptiveMarginThreshold <= 0 || c.Reranker.AdaptiveMarginThreshold > 1 || math.IsNaN(float64(c.Reranker.AdaptiveMarginThreshold)) || math.IsInf(float64(c.Reranker.AdaptiveMarginThreshold), 0) {
		return fmt.Errorf("config: reranker.adaptive_margin_threshold must be > 0 and <= 1")
	}
	if c.Inference.Profile != "local" && c.Inference.Profile != "custom" {
		return fmt.Errorf("config: inference.profile must be local or custom")
	}
	if c.Inference.Ollama.MaxConcurrent <= 0 {
		return fmt.Errorf("config: inference.ollama.max_concurrent must be > 0")
	}
	if c.Inference.Ollama.QueueTimeout <= 0 {
		return fmt.Errorf("config: inference.ollama.queue_timeout must be > 0")
	}
	if c.Inference.Ollama.KeepAlive < 0 {
		return fmt.Errorf("config: inference.ollama.keep_alive must be >= 0")
	}
	if c.Inference.Reranker.MaxConcurrent <= 0 {
		return fmt.Errorf("config: inference.reranker.max_concurrent must be > 0")
	}
	if c.Inference.Reranker.QueueTimeout <= 0 {
		return fmt.Errorf("config: inference.reranker.queue_timeout must be > 0")
	}
	for _, item := range []struct {
		name string
		cfg  AdmissionOperationConfig
	}{
		{"retrieval", c.Admission.Retrieval},
		{"reranking", c.Admission.Reranking},
		{"generation", c.Admission.Generation},
		{"embedding", c.Admission.Embedding},
		{"indexing", c.Admission.Indexing},
		{"destructive", c.Admission.Destructive},
	} {
		if item.cfg.MaxConcurrent <= 0 {
			return fmt.Errorf("config: admission.%s.max_concurrent must be > 0", item.name)
		}
		if item.cfg.QueueTimeout <= 0 {
			return fmt.Errorf("config: admission.%s.queue_timeout must be > 0", item.name)
		}
		if item.name == "indexing" && item.cfg.MaxConcurrent != 1 {
			return fmt.Errorf("config: admission.indexing.max_concurrent must be 1 because an indexing pass is single-writer")
		}
	}
	switch c.Inference.Reranker.Device {
	case "cpu", "cuda", "auto":
	default:
		return fmt.Errorf("config: inference.reranker.device must be cpu, cuda, or auto")
	}
	switch c.Inference.Reranker.Backend {
	case "onnx", "torch-int8", "openvino", "torch":
	default:
		return fmt.Errorf("config: inference.reranker.backend must be onnx, torch-int8, openvino, or torch")
	}
	if c.Inference.Profile == "local" && c.Inference.Reranker.Device == "auto" {
		return fmt.Errorf("config: inference.reranker.device must be explicit for the local profile")
	}
	if c.Inference.Reranker.Device == "cuda" && c.Inference.Reranker.Backend != "torch" {
		return fmt.Errorf("config: inference.reranker.backend must be torch when inference.reranker.device is cuda")
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
	if c.History.SessionPageSize > 1000 {
		return fmt.Errorf("config: history.session_page_size must be <= 1000")
	}
	if c.History.TurnPageSize > 5000 {
		return fmt.Errorf("config: history.turn_page_size must be <= 5000")
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

func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
