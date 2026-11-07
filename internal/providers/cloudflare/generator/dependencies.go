package generator

import (
	"fmt"
	"strings"
	"time"

	conversationgeneration "nadir/internal/core/conversation/generation"
	"nadir/internal/core/observability"
	"nadir/internal/providers/cloudflare/workersai"
)

// DefaultBaseURL is Cloudflare's v4 REST API root.
const DefaultBaseURL = workersai.DefaultBaseURL

// DependenciesConfig groups the Workers AI account, credential and request
// bounds. APIToken is a secret: it is sent only as a bearer header and never
// logged or included in errors.
type DependenciesConfig struct {
	AccountID       string
	APIToken        string
	Model           string // Workers AI model id, e.g. "@cf/meta/llama-3.2-3b-instruct"
	BaseURL         string // empty uses DefaultBaseURL; tests point it at httptest
	RequestTimeout  time.Duration
	MaxOutputTokens int
	// Telemetry receives the total duration of each generation. Nil disables it.
	Telemetry *observability.Recorder
}

// dependencies streams RAG answers from a Workers AI chat model through its
// OpenAI-compatible chat completions endpoint.
type dependencies struct {
	client          *workersai.Client
	model           string
	maxOutputTokens int
	telemetry       *observability.Recorder
}

var _ conversationgeneration.Generator = (*dependencies)(nil)

// NewDependencies constructs a Workers AI generation Adapter.
func NewDependencies(cfg DependenciesConfig) (*dependencies, error) {
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, fmt.Errorf("workers ai model is required")
	}
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	client, err := workersai.New(workersai.Config{
		AccountID:      cfg.AccountID,
		APIToken:       cfg.APIToken,
		BaseURL:        cfg.BaseURL,
		RequestTimeout: timeout,
	})
	if err != nil {
		return nil, err
	}
	return &dependencies{
		client:          client,
		model:           cfg.Model,
		maxOutputTokens: cfg.MaxOutputTokens,
		telemetry:       cfg.Telemetry,
	}, nil
}
