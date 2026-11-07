// Package chat is a small non-streaming Workers AI chat client for short,
// bounded completions (query rewriting, chunk enrichment). It owns the
// OpenAI-compatible chat request/response shape only; prompts and output
// cleanup stay with the callers and transport lives in workersai.
package chat

import (
	"context"
	"fmt"
	"strings"
	"time"

	"nadir/internal/providers/cloudflare/workersai"
)

// Config groups the Workers AI account, credential and model. APIToken is a
// secret: it is sent only as a bearer header and never logged or included in
// errors.
type Config struct {
	AccountID      string
	APIToken       string
	Model          string // Workers AI model id, e.g. "@cf/meta/llama-3.2-3b-instruct"
	BaseURL        string // empty uses the Workers AI default; tests point it at httptest
	RequestTimeout time.Duration
}

// Client sends non-streaming chat completions to one Workers AI model.
type Client struct {
	api   *workersai.Client
	model string
}

// New validates cfg and constructs a Client.
func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, fmt.Errorf("workers ai model is required")
	}
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	api, err := workersai.New(workersai.Config{
		AccountID: cfg.AccountID, APIToken: cfg.APIToken, BaseURL: cfg.BaseURL, RequestTimeout: timeout,
	})
	if err != nil {
		return nil, err
	}
	return &Client{api: api, model: cfg.Model}, nil
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
	var out response
	err := c.api.PostJSON(ctx, "/chat/completions", request{
		Model:       c.model,
		Messages:    []message{{Role: "system", Content: system}, {Role: "user", Content: user}},
		MaxTokens:   maxTokens,
		Temperature: temperature,
	}, &out)
	if err != nil {
		return "", fmt.Errorf("workers ai chat: %w", err)
	}
	if len(out.Choices) == 0 || strings.TrimSpace(out.Choices[0].Message.Content) == "" {
		return "", fmt.Errorf("workers ai chat: empty completion; increase the output cap or choose a non-reasoning model")
	}
	return out.Choices[0].Message.Content, nil
}
