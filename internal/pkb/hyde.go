package pkb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sync"
	"time"
)

// HyDEGenerator produces a hypothetical document for a query.
// The generated text is embedded and used as the search vector instead of the raw query.
// Based on: "Precise Zero-Shot Dense Retrieval without Relevance Labels" (Gao et al., ACL 2023).
type HyDEGenerator interface {
	Generate(ctx context.Context, query string) (string, error)
}

// OllamaHyDEGenerator calls a local Ollama LLM to generate hypothetical documents.
type OllamaHyDEGenerator struct {
	addr   string // e.g. http://localhost:11434
	model  string // e.g. llama3.1:8b-instruct-q4_K_M
	client *http.Client
}

func NewOllamaHyDEGenerator(addr, model string) *OllamaHyDEGenerator {
	return &OllamaHyDEGenerator{
		addr:  addr,
		model: model,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

type ollamaGenerateRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

type ollamaGenerateResponse struct {
	Response string `json:"response"`
}

// Generate produces a hypothetical document that would answer the query.
func (g *OllamaHyDEGenerator) Generate(ctx context.Context, query string) (string, error) {
	prompt := fmt.Sprintf(
		"Write a short, factual passage (2-4 sentences) that directly answers this question. Be specific and informative.\n\nQuestion: %s\n\nPassage:",
		query,
	)

	body, _ := json.Marshal(ollamaGenerateRequest{
		Model:  g.model,
		Prompt: prompt,
		Stream: false,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.addr+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("hyde generate build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("hyde generate: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("hyde generate: status %d", resp.StatusCode)
	}

	var result ollamaGenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("hyde generate decode: %w", err)
	}
	if result.Response == "" {
		return "", fmt.Errorf("hyde generate: empty response")
	}
	return result.Response, nil
}

// HyDESearcher implements the HyDE retrieval algorithm.
// For each query, generates N hypothetical documents in parallel,
// embeds them, averages the embeddings, then performs hybrid search.
type HyDESearcher struct {
	generator HyDEGenerator
	embedder  Embedder
	store     Store
	numDocs   int // number of hypothetical documents to generate (paper default: 8; latency-safe default: 1)
}

func NewHyDESearcher(generator HyDEGenerator, embedder Embedder, store Store, numDocs int) *HyDESearcher {
	if numDocs < 1 {
		numDocs = 1
	}
	return &HyDESearcher{
		generator: generator,
		embedder:  embedder,
		store:     store,
		numDocs:   numDocs,
	}
}

// Search generates hypothetical documents, averages their embeddings, and returns hybrid search results.
func (h *HyDESearcher) Search(ctx context.Context, query string, topK int) ([]ScoredChunk, error) {
	type result struct {
		vec []float32
		err error
	}

	results := make([]result, h.numDocs)
	var wg sync.WaitGroup
	wg.Add(h.numDocs)

	for i := 0; i < h.numDocs; i++ {
		go func(idx int) {
			defer wg.Done()
			doc, err := h.generator.Generate(ctx, query)
			if err != nil {
				results[idx] = result{err: err}
				return
			}
			vec, err := h.embedder.Embed(ctx, doc)
			results[idx] = result{vec: vec, err: err}
		}(i)
	}
	wg.Wait()

	// collect successful vectors; require at least one
	var vecs [][]float32
	for _, r := range results {
		if r.err == nil && len(r.vec) > 0 {
			vecs = append(vecs, r.vec)
		}
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("hyde: all %d generation attempts failed: %w", h.numDocs, results[0].err)
	}

	avgVec := averageVectors(vecs)
	return h.store.HybridSearch(ctx, avgVec, query, topK, nil)
}

// averageVectors returns the element-wise mean of the input vectors, L2-normalized.
func averageVectors(vecs [][]float32) []float32 {
	if len(vecs) == 0 {
		return nil
	}
	dim := len(vecs[0])
	avg := make([]float32, dim)
	for _, v := range vecs {
		for i, x := range v {
			avg[i] += x
		}
	}
	n := float32(len(vecs))
	for i := range avg {
		avg[i] /= n
	}
	return l2Normalize(avg)
}

func l2Normalize(v []float32) []float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	norm := float32(math.Sqrt(sum))
	if norm == 0 {
		return v
	}
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = x / norm
	}
	return out
}
