package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// ProbeResult describes the embedding runtime observed by Probe.
type ProbeResult struct {
	ConfiguredModel string
	LoadedModel     string
	Dimensions      int
}

func (e *dependencies) Dimensions() int { return e.dimensions }

func (e *dependencies) Embed(ctx context.Context, text string) ([]float32, error) {
	vecs, err := e.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return vecs[0], nil
}

func (e *dependencies) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	body, _ := json.Marshal(map[string]any{"model": e.model, "input": texts})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.addr+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama embed batch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama embed batch: status %d", resp.StatusCode)
	}

	var result struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ollama embed batch decode: %w", err)
	}
	if len(result.Embeddings) != len(texts) {
		return nil, fmt.Errorf("ollama embed batch: got %d embeddings for %d inputs", len(result.Embeddings), len(texts))
	}
	return result.Embeddings, nil
}

// Probe verifies that Ollama can load the configured embedding model and
// produce a vector with the configured dimensionality. Ollama's /api/ps
// response is checked afterwards so readiness reports the model that is
// actually resident in the runner, not only the model requested by config.
func (e *dependencies) Probe(ctx context.Context) (ProbeResult, error) {
	result := ProbeResult{ConfiguredModel: e.model}
	vector, err := e.Embed(ctx, "nadir readiness probe")
	if err != nil {
		return result, fmt.Errorf("embedding model %q unavailable: %w", e.model, err)
	}
	result.Dimensions = len(vector)
	if e.dimensions > 0 && len(vector) != e.dimensions {
		return result, fmt.Errorf("embedding model %q returned %d dimensions, want %d", e.model, len(vector), e.dimensions)
	}

	loaded, err := e.loadedModel(ctx)
	if err != nil {
		return result, fmt.Errorf("inspect loaded embedding model: %w", err)
	}
	result.LoadedModel = loaded
	if !sameModel(e.model, loaded) {
		return result, fmt.Errorf("configured embedding model %q is not loaded; runner reports %q", e.model, loaded)
	}
	return result, nil
}

func (e *dependencies) loadedModel(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.addr+"/api/ps", nil)
	if err != nil {
		return "", err
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama ps: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama ps: status %d", resp.StatusCode)
	}

	var result struct {
		Models []struct {
			Name  string `json:"name"`
			Model string `json:"model"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("ollama ps decode: %w", err)
	}
	for _, model := range result.Models {
		name := model.Name
		if name == "" {
			name = model.Model
		}
		if sameModel(e.model, name) {
			return name, nil
		}
	}
	if len(result.Models) == 0 {
		return "", nil
	}
	name := result.Models[0].Name
	if name == "" {
		name = result.Models[0].Model
	}
	return name, nil
}

func sameModel(configured, loaded string) bool {
	configured = strings.TrimSpace(configured)
	loaded = strings.TrimSpace(loaded)
	if configured == "" || loaded == "" {
		return false
	}
	if configured == loaded {
		return true
	}
	return strings.TrimSuffix(configured, ":latest") == strings.TrimSuffix(loaded, ":latest")
}
