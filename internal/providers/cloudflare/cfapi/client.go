// Package cfapi is the shared HTTP transport for Cloudflare's account-scoped
// v4 REST API. It owns the account URL, bearer authentication, error decoding
// and retry of rate-limited requests. Product packages (Workers AI, D1) own
// their path prefix and request and response bodies.
//
// The API token is a secret: it is sent only as a bearer header and never
// appears in errors or logs.
package cfapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is Cloudflare's v4 REST API root.
const DefaultBaseURL = "https://api.cloudflare.com/client/v4"

const (
	defaultMaxRetries = 2
	defaultRetryDelay = 500 * time.Millisecond
	maxRetryAfter     = 10 * time.Second
)

// Config groups the account, credential and request bounds.
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
	// RetryStatuses lists the statuses that are retried. Empty means 429 and
	// 503. Use only 429 for requests that must not run twice (writes): a 429
	// was rejected before execution, while a 503 may not have been.
	RetryStatuses []int
}

// Client sends account-scoped requests to Workers AI.
type Client struct {
	base       string
	apiToken   string
	maxRetries int
	retryDelay time.Duration
	retryOn    map[int]bool
	http       *http.Client
}

// StatusError is a non-success HTTP response. Detail is Cloudflare's short
// error message when the body carried one.
type StatusError struct {
	Code   int
	Detail string
}

func (e *StatusError) Error() string {
	if e.Detail == "" {
		return fmt.Sprintf("status %d", e.Code)
	}
	return fmt.Sprintf("status %d: %s", e.Code, e.Detail)
}

// New validates cfg and constructs a Client.
func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.AccountID) == "" {
		return nil, fmt.Errorf("workers ai account id is required")
	}
	if strings.TrimSpace(cfg.APIToken) == "" {
		return nil, fmt.Errorf("workers ai api token is required")
	}
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		base = DefaultBaseURL
	}
	retries := cfg.MaxRetries
	switch {
	case retries == 0:
		retries = defaultMaxRetries
	case retries < 0:
		retries = 0
	}
	delay := cfg.RetryDelay
	if delay <= 0 {
		delay = defaultRetryDelay
	}
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	statuses := cfg.RetryStatuses
	if len(statuses) == 0 {
		statuses = []int{http.StatusTooManyRequests, http.StatusServiceUnavailable}
	}
	retryOn := make(map[int]bool, len(statuses))
	for _, status := range statuses {
		retryOn[status] = true
	}
	return &Client{
		base:       base + "/accounts/" + url.PathEscape(cfg.AccountID),
		retryOn:    retryOn,
		apiToken:   cfg.APIToken,
		maxRetries: retries,
		retryDelay: delay,
		http:       &http.Client{Timeout: timeout},
	}, nil
}

// Post sends body as JSON to path, relative to the account, (for example "/ai/v1/chat/completions") and
// returns the open 200 response; the caller must close it. extraHeaders are
// added to the request. Non-200 responses return a *StatusError; 429 and 503
// are retried first, honoring Retry-After.
func (c *Client) Post(ctx context.Context, path string, body any, extraHeaders map[string]string) (*http.Response, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(payload))
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+c.apiToken)
		for k, v := range extraHeaders {
			req.Header.Set(k, v)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			// net/http errors carry the URL, never request headers.
			return nil, fmt.Errorf("request: %w", err)
		}
		if resp.StatusCode == http.StatusOK {
			return resp, nil
		}
		statusErr := &StatusError{Code: resp.StatusCode, Detail: errorDetail(resp.Body)}
		retryAfter := resp.Header.Get("Retry-After")
		_ = resp.Body.Close()
		retryable := c.retryOn[resp.StatusCode]
		if !retryable || attempt >= c.maxRetries {
			return nil, statusErr
		}
		timer := time.NewTimer(c.backoff(attempt, retryAfter))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, fmt.Errorf("request: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

// PostJSON posts body and decodes the JSON response into out (at most 8 MiB).
func (c *Client) PostJSON(ctx context.Context, path string, body, out any) error {
	resp, err := c.Post(ctx, path, body, nil)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (c *Client) backoff(attempt int, retryAfter string) time.Duration {
	if secs, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && secs >= 0 {
		return min(time.Duration(secs)*time.Second, maxRetryAfter)
	}
	return c.retryDelay << attempt
}

// errorDetail extracts a short provider message from a non-success body.
func errorDetail(body io.Reader) string {
	raw, _ := io.ReadAll(io.LimitReader(body, 512))
	var parsed struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if json.Unmarshal(raw, &parsed) == nil && len(parsed.Errors) > 0 {
		return parsed.Errors[0].Message
	}
	return ""
}
