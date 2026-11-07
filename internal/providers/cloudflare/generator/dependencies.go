package generator

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	conversationgeneration "nadir/internal/core/conversation/generation"
	"nadir/internal/core/observability"
)

// DefaultBaseURL is Cloudflare's v4 REST API root.
const DefaultBaseURL = "https://api.cloudflare.com/client/v4"

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
	endpoint        string
	apiToken        string
	model           string
	maxOutputTokens int
	client          *http.Client
	telemetry       *observability.Recorder
}

var _ conversationgeneration.Generator = (*dependencies)(nil)

// NewDependencies constructs a Workers AI generation Adapter.
func NewDependencies(cfg DependenciesConfig) (*dependencies, error) {
	if strings.TrimSpace(cfg.AccountID) == "" {
		return nil, fmt.Errorf("workers ai account id is required")
	}
	if strings.TrimSpace(cfg.APIToken) == "" {
		return nil, fmt.Errorf("workers ai api token is required")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, fmt.Errorf("workers ai model is required")
	}
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		base = DefaultBaseURL
	}
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	return &dependencies{
		endpoint:        base + "/accounts/" + url.PathEscape(cfg.AccountID) + "/ai/v1/chat/completions",
		apiToken:        cfg.APIToken,
		model:           cfg.Model,
		maxOutputTokens: cfg.MaxOutputTokens,
		telemetry:       cfg.Telemetry,
		client:          &http.Client{Timeout: timeout},
	}, nil
}
