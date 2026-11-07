package embedding

import (
	"fmt"
	"strings"
	"time"

	coreembedding "nadir/internal/core/embedding"
	"nadir/internal/core/observability"
	"nadir/internal/providers/cloudflare/workersai"
)

// defaultBatchSize caps inputs per request. Workers AI embedding models accept
// array input; the exact per-request limit is not verified here, so the
// adapter splits conservatively and callers may batch freely.
const defaultBatchSize = 32

// DependenciesConfig groups the Workers AI account, model and vector shape.
// APIToken is a secret: it is sent only as a bearer header and never logged or
// included in errors.
type DependenciesConfig struct {
	AccountID      string
	APIToken       string
	Model          string // Workers AI embedding model id, e.g. "@cf/google/embeddinggemma-300m"
	Dimensions     int    // the vector size the Qdrant collections were created with
	BaseURL        string // empty uses the Workers AI default; tests point it at httptest
	RequestTimeout time.Duration
	BatchSize      int // inputs per request; zero uses the default
	// Telemetry receives the duration of each embedding request. Nil disables it.
	Telemetry *observability.Recorder
}

// dependencies embeds text through Workers AI's OpenAI-compatible endpoint.
type dependencies struct {
	client     *workersai.Client
	model      string
	dimensions int
	batchSize  int
	telemetry  *observability.Recorder
}

var _ coreembedding.Embedder = (*dependencies)(nil)

// NewDependencies constructs a Workers AI embedding Adapter.
func NewDependencies(cfg DependenciesConfig) (*dependencies, error) {
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, fmt.Errorf("workers ai embedding model is required")
	}
	if cfg.Dimensions <= 0 {
		return nil, fmt.Errorf("workers ai embedding dimensions must be > 0")
	}
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	client, err := workersai.New(workersai.Config{
		AccountID: cfg.AccountID, APIToken: cfg.APIToken, BaseURL: cfg.BaseURL, RequestTimeout: timeout,
	})
	if err != nil {
		return nil, err
	}
	batch := cfg.BatchSize
	if batch <= 0 {
		batch = defaultBatchSize
	}
	return &dependencies{
		client:     client,
		model:      cfg.Model,
		dimensions: cfg.Dimensions,
		batchSize:  batch,
		telemetry:  cfg.Telemetry,
	}, nil
}
