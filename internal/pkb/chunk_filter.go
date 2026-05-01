package pkb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ChunkFilter drops irrelevant chunks after retrieval, before generation.
// Complements cross-encoder reranking: reranker reorders, filter drops.
// Paper: arxiv 2410.19572 (+10pp PopQA accuracy).
type ChunkFilter interface {
	Filter(ctx context.Context, query string, chunks []ScoredChunk) ([]ScoredChunk, error)
}

// LLMChunkFilter calls an OpenAI-compatible chat endpoint and batches all
// chunks into one prompt.  Irrelevant chunks (score below threshold) are
// dropped; the order of surviving chunks is preserved.
type LLMChunkFilter struct {
	BaseURL    string
	Model      string
	APIKey     string
	Threshold  float64 // 0–1; chunks below this score are dropped (default 0.5)
	HTTPClient *http.Client
}

func NewLLMChunkFilter(baseURL, model, apiKey string, threshold float64) *LLMChunkFilter {
	if threshold <= 0 {
		threshold = 0.5
	}
	return &LLMChunkFilter{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		Model:      model,
		APIKey:     apiKey,
		Threshold:  threshold,
		HTTPClient: &http.Client{Timeout: 60 * time.Second},
	}
}

// chunkFilterPrompt asks for a JSON array of relevance scores (0–1) parallel to the passages.
const chunkFilterPrompt = `You are a retrieval filter for a knowledge base.

Query: %s

Rate each passage for relevance to the query.
Return ONLY a JSON array of numbers between 0 and 1, one per passage, in order.
Example for 3 passages: [0.9, 0.1, 0.7]

Passages:
%s`

func (f *LLMChunkFilter) Filter(ctx context.Context, query string, chunks []ScoredChunk) ([]ScoredChunk, error) {
	if len(chunks) == 0 {
		return chunks, nil
	}

	var sb strings.Builder
	for i, c := range chunks {
		text := c.WindowText
		if text == "" {
			text = c.Text
		}
		fmt.Fprintf(&sb, "[%d] %s\n\n", i+1, text)
	}

	prompt := fmt.Sprintf(chunkFilterPrompt, query, sb.String())

	reqBody := struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}{
		Model: f.Model,
		Messages: []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{
			{Role: "user", Content: prompt},
		},
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("chunk filter marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.BaseURL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("chunk filter new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if f.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+f.APIKey)
	}

	resp, err := f.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("chunk filter call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("chunk filter HTTP %d", resp.StatusCode)
	}

	var chatResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("chunk filter decode: %w", err)
	}
	if len(chatResp.Choices) == 0 {
		return nil, fmt.Errorf("chunk filter: empty choices")
	}

	raw := strings.TrimSpace(chatResp.Choices[0].Message.Content)
	// strip markdown code fences if present
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var scores []float64
	if err := json.NewDecoder(bytes.NewBufferString(raw)).Decode(&scores); err != nil {
		// LLM returned unexpected format; pass all chunks through rather than drop everything
		return chunks, nil
	}
	if len(scores) != len(chunks) {
		// mismatch; pass through
		return chunks, nil
	}

	kept := chunks[:0:len(chunks)]
	for i, c := range chunks {
		if scores[i] >= f.Threshold {
			kept = append(kept, c)
		}
	}
	return kept, nil
}
