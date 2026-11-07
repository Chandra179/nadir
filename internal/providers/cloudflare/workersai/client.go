// Package workersai is the shared HTTP client for Cloudflare Workers AI's
// OpenAI-compatible endpoints (/ai/v1/...) over the account-scoped cfapi
// transport. Callers own request and response bodies, so generation, chat
// completions and embeddings are thin adapters over one client.
package workersai

import (
	"context"
	"net/http"
	"time"

	"nadir/internal/providers/cloudflare/cfapi"
)

// DefaultBaseURL is Cloudflare's v4 REST API root.
const DefaultBaseURL = cfapi.DefaultBaseURL

// StatusError is a non-success HTTP response.
type StatusError = cfapi.StatusError

// Config groups the account, credential and request bounds. APIToken is a
// secret: it is sent only as a bearer header and never logged or included in
// errors.
type Config struct {
	AccountID string
	APIToken  string
	BaseURL   string // empty uses DefaultBaseURL; tests point it at httptest
	// RequestTimeout bounds one whole request including a streamed body.
	RequestTimeout time.Duration
	// MaxRetries is the number of retries after a 429 or 503. Zero uses the
	// default of 2; a negative value disables retries.
	MaxRetries int
	// RetryDelay is the first backoff when the response has no Retry-After.
	RetryDelay time.Duration
}

// Client sends Workers AI requests for one account.
type Client struct{ api *cfapi.Client }

// New validates cfg and constructs a Client.
func New(cfg Config) (*Client, error) {
	api, err := cfapi.New(cfapi.Config{
		AccountID: cfg.AccountID, APIToken: cfg.APIToken, BaseURL: cfg.BaseURL,
		RequestTimeout: cfg.RequestTimeout, MaxRetries: cfg.MaxRetries, RetryDelay: cfg.RetryDelay,
	})
	if err != nil {
		return nil, err
	}
	return &Client{api: api}, nil
}

// Post sends body as JSON to path under /ai/v1 (for example
// "/chat/completions") and returns the open 200 response; the caller must
// close it. 429 and 503 are retried first, honoring Retry-After.
func (c *Client) Post(ctx context.Context, path string, body any, extraHeaders map[string]string) (*http.Response, error) {
	return c.api.Post(ctx, "/ai/v1"+path, body, extraHeaders)
}

// PostJSON posts body under /ai/v1 and decodes the JSON response into out.
func (c *Client) PostJSON(ctx context.Context, path string, body, out any) error {
	return c.api.PostJSON(ctx, "/ai/v1"+path, body, out)
}
