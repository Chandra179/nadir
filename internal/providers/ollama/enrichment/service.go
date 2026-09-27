// Package enrichment provides index-time LLM enrichment over Ollama:
// Anthropic-style contextual chunk intros. It is a domain package and must
// not import api/server/middleware packages.
package enrichment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const contextualSystemPrompt = `You write document context lines. Given an excerpt of a document and one chunk from it, write ONE short sentence (<30 words) situating the chunk: which document/topic it belongs to and any key entities or terms needed to understand it out of order. Reply ONLY with the sentence itself — no preamble, no quotes.`

func (d *dependencies) ContextualIntro(ctx context.Context, documentExcerpt, chunkText string) (string, error) {
	prompt := fmt.Sprintf("Document excerpt:\n%s\n\nChunk:\n%s", documentExcerpt, chunkText)
	out, err := d.chat(ctx, d.contextualAddr, d.contextualModel, contextualSystemPrompt, prompt)
	if err != nil {
		return "", err
	}
	intro := strings.TrimSpace(stripFences(out))
	intro = strings.Trim(intro, "\"“”'")
	intro = strings.Join(strings.Fields(intro), " ")
	if intro == "" {
		return "", fmt.Errorf("enrichment: empty contextual intro from model output")
	}
	return intro, nil
}

// chat posts a non-streaming chat request to Ollama and returns the
// assistant message content.
func (d *dependencies) chat(ctx context.Context, addr, model, system, user string) (string, error) {
	payload := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"stream": false,
		"options": map[string]any{
			"temperature": 0.2,
		},
	}
	if d.keepAlive != "" {
		payload["keep_alive"] = d.keepAlive
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("enrichment: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, addr+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("enrichment: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("enrichment: ollama chat: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("enrichment: ollama chat: status %d", resp.StatusCode)
	}

	var result struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("enrichment: decode chat response: %w", err)
	}
	return result.Message.Content, nil
}

func stripFences(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}
