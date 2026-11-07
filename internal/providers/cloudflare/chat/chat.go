// Package chat is a small non-streaming Workers AI chat client for short,
// bounded completions (query rewriting, chunk enrichment). It owns the
// OpenAI-compatible request/response wire format only; prompts and output
// cleanup stay with the callers.
package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultBaseURL is Cloudflare's v4 REST API root.
const DefaultBaseURL = "https://api.cloudflare.com/client/v4"

// Config groups the Workers AI account, credential and model. APIToken is a
// secret: it is sent only as a bearer header and never logged or included in
// errors.
type Config struct {
	AccountID      string
	APIToken       string
	Model          string // Workers AI model id, e.g. "@cf/meta/llama-3.2-3b-instruct"
	BaseURL        string // empty uses DefaultBaseURL; tests point it at httptest
	RequestTimeout time.Duration
}

// Client sends non-streaming chat completions to one Workers AI model.
type Client struct {
	endpoint string
	apiToken string
	model    string
	client   *http.Client
}

// New validates cfg and constructs a Client.
func New(cfg Config) (*Client, error) {
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
		timeout = 30 * time.Second
	}
	return &Client{
		endpoint: base + "/accounts/" + url.PathEscape(cfg.AccountID) + "/ai/v1/chat/completions",
		apiToken: cfg.APIToken,
		model:    cfg.Model,
		client:   &http.Client{Timeout: timeout},
	}, nil
}

// Completer returns a function that completes one system+user exchange with
// the given output cap and temperature. Callers inject it as their own seam.
func (c *Client) Completer(maxTokens int, temperature float64) func(ctx context.Context, system, user string) (string, error) {
	return func(ctx context.Context, system, user string) (string, error) {
		return c.complete(ctx, system, user, maxTokens, temperature)
	}
}

type request struct {
	Model       string    `json:"model"`
	Messages    []message `json:"messages"`
	Stream      bool      `json:"stream"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type response struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func (c *Client) complete(ctx context.Context, system, user string, maxTokens int, temperature float64) (string, error) {
	body, err := json.Marshal(request{
		Model:       c.model,
		Messages:    []message{{Role: "system", Content: system}, {Role: "user", Content: user}},
		MaxTokens:   maxTokens,
		Temperature: temperature,
	})
	if err != nil {
		return "", fmt.Errorf("workers ai chat encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("workers ai chat build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiToken)

	resp, err := c.client.Do(req)
	if err != nil {
		// net/http errors carry the URL, never request headers.
		return "", fmt.Errorf("workers ai chat request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("workers ai chat: status %d%s", resp.StatusCode, errorDetail(resp.Body))
	}
	var out response
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return "", fmt.Errorf("workers ai chat decode response: %w", err)
	}
	if len(out.Choices) == 0 || strings.TrimSpace(out.Choices[0].Message.Content) == "" {
		return "", fmt.Errorf("workers ai chat: empty completion; increase the output cap or choose a non-reasoning model")
	}
	return out.Choices[0].Message.Content, nil
}

// errorDetail extracts a short provider message from a non-success body.
func errorDetail(body io.Reader) string {
	raw, _ := io.ReadAll(io.LimitReader(body, 512))
	var parsed struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if json.Unmarshal(raw, &parsed) == nil && len(parsed.Errors) > 0 && parsed.Errors[0].Message != "" {
		return ": " + parsed.Errors[0].Message
	}
	return ""
}
