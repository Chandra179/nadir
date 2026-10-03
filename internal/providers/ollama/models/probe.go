// Package models checks configured Ollama models without loading an inference
// runner. Readiness must not evict the active answer model just to check setup.
package models

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type DependenciesConfig struct {
	Addr           string
	Model          string
	RequestTimeout time.Duration
}

type Probe struct {
	addr   string
	model  string
	client *http.Client
}

func NewDependencies(cfg DependenciesConfig) *Probe {
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Probe{addr: strings.TrimRight(cfg.Addr, "/"), model: cfg.Model, client: &http.Client{Timeout: timeout}}
}

// Check verifies model availability at the role's own endpoint. It does not
// claim that a model is resident or that a generation request has succeeded.
func (p *Probe) Check(ctx context.Context) error {
	body, err := json.Marshal(map[string]string{"model": p.model})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.addr+"/api/show", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("Ollama model probe: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("check model %q: %w", p.model, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("model %q is not installed at its configured Ollama endpoint; install it with ollama pull %s", p.model, p.model)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("check model %q: Ollama returned HTTP %d", p.model, resp.StatusCode)
	}
	var result map[string]json.RawMessage
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&result); err != nil {
		return fmt.Errorf("check model %q: invalid Ollama metadata: %w", p.model, err)
	}
	if len(result) == 0 {
		return fmt.Errorf("check model %q: empty Ollama metadata", p.model)
	}
	return nil
}
