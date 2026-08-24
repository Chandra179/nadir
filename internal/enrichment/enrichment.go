// Package enrichment provides index-time LLM enrichment over Ollama:
// HyPE hypothetical-question generation and Anthropic-style contextual
// chunk intros. It is a domain package and must not import api/server/
// middleware packages.
package enrichment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// DependenciesConfig groups everything needed to construct the enrichment
// client.
type DependenciesConfig struct {
	Addr  string // Ollama base addr, e.g. http://localhost:11434
	Model string // instruct LLM used for generation
}

type dependencies struct {
	addr   string
	model  string
	client *http.Client
}

var _ Enricher = (*dependencies)(nil)

func NewDependencies(cfg DependenciesConfig) *dependencies {
	return &dependencies{
		addr:   cfg.Addr,
		model:  cfg.Model,
		client: &http.Client{Timeout: 120 * time.Second},
	}
}

// Enricher is consumed by the ingest pipeline; both methods are allowed to
// fail — callers degrade gracefully.
type Enricher interface {
	HypotheticalQuestions(ctx context.Context, header, text string, n int) ([]string, error)
	ContextualIntro(ctx context.Context, documentExcerpt, chunkText string) (string, error)
}

const hypeSystemPrompt = `You write search queries. Given a passage from a knowledge base, produce short standalone questions that a user might type and that this exact passage answers. Questions must be self-contained: never refer to "the passage" or "this section". Reply ONLY with a JSON array of strings.`

func (d *dependencies) HypotheticalQuestions(ctx context.Context, header, text string, n int) ([]string, error) {
	if n <= 0 {
		n = 3
	}
	prompt := fmt.Sprintf("Write exactly %d search queries for this passage.\n\nSection: %s\n\n%s", n, header, text)
	out, err := d.chat(ctx, hypeSystemPrompt, prompt)
	if err != nil {
		return nil, err
	}
	qs := parseStringList(out)
	if len(qs) == 0 {
		return nil, fmt.Errorf("enrichment: no questions parsed from model output")
	}
	cleaned := make([]string, 0, len(qs))
	for _, q := range qs {
		if q = strings.TrimSpace(q); q != "" {
			cleaned = append(cleaned, q)
		}
	}
	if len(cleaned) == 0 {
		return nil, fmt.Errorf("enrichment: all parsed questions were empty")
	}
	if len(cleaned) > n {
		cleaned = cleaned[:n]
	}
	return cleaned, nil
}

const contextualSystemPrompt = `You write document context lines. Given an excerpt of a document and one chunk from it, write ONE short sentence (<30 words) situating the chunk: which document/topic it belongs to and any key entities or terms needed to understand it out of order. Reply ONLY with the sentence itself — no preamble, no quotes.`

func (d *dependencies) ContextualIntro(ctx context.Context, documentExcerpt, chunkText string) (string, error) {
	prompt := fmt.Sprintf("Document excerpt:\n%s\n\nChunk:\n%s", documentExcerpt, chunkText)
	out, err := d.chat(ctx, contextualSystemPrompt, prompt)
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
func (d *dependencies) chat(ctx context.Context, system, user string) (string, error) {
	body, err := json.Marshal(map[string]any{
		"model": d.model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"stream": false,
		"options": map[string]any{
			"temperature": 0.2,
		},
	})
	if err != nil {
		return "", fmt.Errorf("enrichment: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.addr+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("enrichment: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("enrichment: ollama chat: %w", err)
	}
	defer resp.Body.Close()
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

var quotedString = regexp.MustCompile(`"((?:[^"\\]|\\.)*)"`)

// parseStringList extracts a list of strings from model output: it prefers
// a JSON array (tolerating code fences or prose around it) and falls back
// to scanning double-quoted strings.
func parseStringList(out string) []string {
	s := stripFences(out)
	if start := strings.Index(s, "["); start >= 0 {
		if end := strings.LastIndex(s, "]"); end > start {
			var arr []string
			if err := json.Unmarshal([]byte(s[start:end+1]), &arr); err == nil {
				return arr
			}
		}
	}
	matches := quotedString.FindAllStringSubmatch(s, -1)
	var out2 []string
	for _, m := range matches {
		var q string
		if err := json.Unmarshal([]byte(`"`+m[1]+`"`), &q); err == nil {
			out2 = append(out2, q)
		}
	}
	return out2
}

func stripFences(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}
