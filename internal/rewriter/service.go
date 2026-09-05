// Package rewriter rewrites conversational follow-up questions into
// standalone search queries over Ollama (Rewrite-Retrieve-Read, arXiv
// 2305.14283), so retrieval receives "what does the secant formula compute?"
// instead of "what about the second one?". It is a domain package and must
// not import api/server/middleware packages.
package rewriter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const systemPrompt = `You rewrite a follow-up question into a standalone search query for a search engine. Use the conversation to resolve pronouns, ellipsis, and references like "it", "that", or "the second one". Keep the original topic and wording; never answer the question. If the follow-up is already standalone, return it unchanged. Reply with ONLY the rewritten query — no quotes, no labels, no explanation.`

// maxAnswerChars bounds each assistant reply in the rewrite prompt: answers
// only serve as context for resolving references, not as documents to read.
const maxAnswerChars = 400

func (d *dependencies) Rewrite(ctx context.Context, turns []Turn, query string) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("rewriter: empty query")
	}
	out, err := d.chat(ctx, systemPrompt, buildPrompt(turns, query))
	if err != nil {
		return "", err
	}
	rewritten := cleanQuery(out)
	if rewritten == "" {
		return "", fmt.Errorf("rewriter: empty rewrite from model output")
	}
	return rewritten, nil
}

// buildPrompt renders the prior conversation turns plus the follow-up
// question into the rewrite prompt. Turns with empty queries are skipped so
// one malformed persisted turn can't derail the rewrite.
func buildPrompt(turns []Turn, query string) string {
	var sb strings.Builder
	sb.WriteString("Conversation:")
	for _, t := range turns {
		q := strings.TrimSpace(t.Query)
		if q == "" {
			continue
		}
		sb.WriteString("\nUser: ")
		sb.WriteString(q)
		if a := strings.TrimSpace(t.Answer); a != "" {
			sb.WriteString("\nAssistant: ")
			sb.WriteString(truncate(a, maxAnswerChars))
		}
	}
	sb.WriteString("\n\nFollow-up question: ")
	sb.WriteString(strings.TrimSpace(query))
	sb.WriteString("\nStandalone search query:")
	return sb.String()
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// cleanQuery strips code fences, surrounding quotes, and the labels the
// model sometimes echoes despite instructions, then collapses whitespace.
func cleanQuery(out string) string {
	s := strings.TrimSpace(stripFences(out))
	for _, prefix := range []string{
		"Standalone search query:", "Standalone query:", "Search query:",
		"Rewritten query:", "Rewritten:", "Query:",
	} {
		s = strings.TrimSpace(strings.TrimPrefix(s, prefix))
	}
	s = strings.Trim(s, "\"“”'")
	return strings.Join(strings.Fields(s), " ")
}

// chat posts a non-streaming chat request to Ollama and returns the
// assistant message content. Temperature 0 keeps rewrites deterministic —
// drift here silently changes what gets searched.
func (d *dependencies) chat(ctx context.Context, system, user string) (string, error) {
	body, err := json.Marshal(map[string]any{
		"model": d.model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"stream": false,
		"options": map[string]any{
			"temperature": 0,
		},
	})
	if err != nil {
		return "", fmt.Errorf("rewriter: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.addr+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("rewriter: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("rewriter: ollama chat: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("rewriter: ollama chat: status %d", resp.StatusCode)
	}

	var result struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("rewriter: decode chat response: %w", err)
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
