// Package chat is the non-streaming Ollama /api/chat client used for short,
// bounded completions such as query rewriting and chunk enrichment. It owns
// the Ollama wire protocol only; prompts and output cleanup belong to the
// consuming use-case packages.
package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Config groups one role's Ollama endpoint and sampling options.
type Config struct {
	Addr           string        // Ollama base addr, e.g. http://localhost:11434
	Model          string        // instruct LLM for this role
	Think          *bool         // nil leaves the model default unchanged
	KeepAlive      string        // empty leaves Ollama's default
	RequestTimeout time.Duration // timeout for one request
	Temperature    float64
	// MaxTokens maps to num_predict; zero omits it.
	MaxTokens int
}

// Client posts non-streaming chat requests to one Ollama model.
type Client struct {
	cfg    Config
	client *http.Client
}

// New constructs a Client. A zero RequestTimeout defaults to 30 seconds.
func New(cfg Config) *Client {
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	cfg.Addr = strings.TrimRight(cfg.Addr, "/")
	return &Client{cfg: cfg, client: &http.Client{Timeout: timeout}}
}

// Complete sends one system+user exchange and returns the assistant content.
func (c *Client) Complete(ctx context.Context, system, user string) (string, error) {
	options := map[string]any{"temperature": c.cfg.Temperature}
	if c.cfg.MaxTokens > 0 {
		options["num_predict"] = c.cfg.MaxTokens
	}
	payload := map[string]any{
		"model": c.cfg.Model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"stream":  false,
		"options": options,
	}
	if c.cfg.Think != nil {
		payload["think"] = *c.cfg.Think
	}
	if c.cfg.KeepAlive != "" {
		payload["keep_alive"] = c.cfg.KeepAlive
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("ollama chat: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.Addr+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("ollama chat: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama chat: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama chat: status %d", resp.StatusCode)
	}

	var result struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("ollama chat: decode response: %w", err)
	}
	return result.Message.Content, nil
}
