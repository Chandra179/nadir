package pkb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// OllamaEmbedder implements Embedder using a local Ollama instance.
type OllamaEmbedder struct {
	addr       string // e.g. http://localhost:11434
	model      string // e.g. nomic-embed-text
	dimensions int
	client     *http.Client
}

func NewOllamaEmbedder(addr, model string, dimensions int) *OllamaEmbedder {
	return &OllamaEmbedder{
		addr:       addr,
		model:      model,
		dimensions: dimensions,
		client:     &http.Client{},
	}
}

func (e *OllamaEmbedder) Dimensions() int { return e.dimensions }

func (e *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	vecs, err := e.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return vecs[0], nil
}

// EmbedBatch calls /api/embed which accepts an array of inputs in one round-trip.
func (e *OllamaEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
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
