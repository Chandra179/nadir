package reranker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"
)

// DependenciesConfig groups everything needed to construct the HTTP
// cross-encoder reranker client.
type DependenciesConfig struct {
	Addr           string
	Model          string
	MaxConcurrent  int
	RequestTimeout time.Duration
	Log            *zap.Logger
}

type dependencies struct {
	addr   string
	model  string
	client *http.Client
	sem    chan struct{}
	log    *zap.Logger
}

var _ Reranker = (*dependencies)(nil)

func NewDependencies(cfg DependenciesConfig) *dependencies {
	maxConcurrent := cfg.MaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = 10
	}
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}
	return &dependencies{
		addr:   cfg.Addr,
		model:  cfg.Model,
		client: &http.Client{Timeout: timeout},
		sem:    make(chan struct{}, maxConcurrent),
		log:    log,
	}
}

// ProbeResult describes the model and runtime reported by the reranker
// sidecar's health endpoint.
type ProbeResult struct {
	ConfiguredModel string
	LoadedModel     string
	Backend         string
	Device          string
}

// Probe verifies that the reranker sidecar has a loaded model. The sidecar
// health response also exposes the concrete backend and device so operators
// can detect an unexpected CPU fallback or runner failure.
func (r *dependencies) Probe(ctx context.Context) (ProbeResult, error) {
	result := ProbeResult{ConfiguredModel: r.model}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.addr+"/health", nil)
	if err != nil {
		return result, err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return result, fmt.Errorf("reranker health: %w", err)
	}
	defer resp.Body.Close()

	var health struct {
		Status      string `json:"status"`
		Model       string `json:"model"`
		LoadedModel string `json:"loaded_model"`
		Backend     string `json:"backend"`
		Device      string `json:"device"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return result, fmt.Errorf("reranker health decode: %w", err)
	}
	result.LoadedModel = health.LoadedModel
	if result.LoadedModel == "" {
		result.LoadedModel = health.Model
	}
	result.Backend = health.Backend
	result.Device = health.Device
	if resp.StatusCode != http.StatusOK || health.Status != "ok" {
		if health.Error != "" {
			return result, fmt.Errorf("reranker runner: %s", health.Error)
		}
		return result, fmt.Errorf("reranker runner status %q (http %d)", health.Status, resp.StatusCode)
	}
	if result.LoadedModel == "" {
		return result, fmt.Errorf("reranker runner did not report a loaded model")
	}
	if r.model != "" && !sameModel(r.model, result.LoadedModel) {
		return result, fmt.Errorf("configured reranker model %q does not match loaded model %q", r.model, result.LoadedModel)
	}
	return result, nil
}

func sameModel(configured, loaded string) bool {
	configured = strings.TrimSpace(configured)
	loaded = strings.TrimSpace(loaded)
	if configured == loaded {
		return true
	}
	return strings.TrimSuffix(configured, ":latest") == strings.TrimSuffix(loaded, ":latest")
}
