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
	body, _ := json.Marshal(map[string]string{"model": e.model, "prompt": text})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.addr+"/api/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama embed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama embed: status %d", resp.StatusCode)
	}

	var result struct {
		Embedding []float32 `json:"embedding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ollama embed decode: %w", err)
	}
	if len(result.Embedding) == 0 {
		return nil, fmt.Errorf("ollama embed: empty embedding")
	}
	return result.Embedding, nil
}
