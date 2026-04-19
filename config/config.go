package config

import (
	"os"
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
	Eval          EvalConfig          `yaml:"eval"`
	SparseScorer  SparseScorerConfig  `yaml:"sparse_scorer"`
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

// KnowledgeBaseConfig points to a local directory of markdown files.
// Set path to any directory — git submodule, a plain folder, or a symlink.
type KnowledgeBaseConfig struct {
	Path string `yaml:"path"`
}

type QdrantConfig struct {
	Addr       string `yaml:"addr"`
	Collection string `yaml:"collection"`
	TopK       int    `yaml:"top_k"`
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

type EvalConfig struct {
	LLMBaseURL  string `yaml:"llm_base_url"`
	LLMModel    string `yaml:"llm_model"`
	HistoryPath string `yaml:"history_path"`
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

	return &cfg, nil
}
