package rewriting

import (
	"context"
	"fmt"
	"strings"
)

// Completer runs one system+user chat exchange against a language model and
// returns the reply text. Transport, model choice and sampling options belong
// to the provider Adapter that implements it.
type Completer func(ctx context.Context, system, user string) (string, error)

const systemPrompt = `Copy the follow-up question, replacing only unresolved references with their specific named subject from the conversation. Preserve the intent, wording and conditions. If the subject is already named in the follow-up, return that question unchanged. Never answer it. Output only the question.`

// maxAnswerChars bounds each assistant reply in the rewrite prompt: answers
// only serve as context for resolving references, not as documents to read.
const maxAnswerChars = 400

type llmRewriter struct{ complete Completer }

// NewLLM returns a Rewriter that builds the rewrite prompt and cleans the
// model's reply; the model call itself is delegated to complete.
func NewLLM(complete Completer) Rewriter {
	return &llmRewriter{complete: complete}
}

func (r *llmRewriter) Rewrite(ctx context.Context, turns []Turn, query string) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("rewriter: empty query")
	}
	out, err := r.complete(ctx, systemPrompt, buildPrompt(turns, query))
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
	sb.WriteString("\n\nCopy the follow-up, replacing only unresolved references with the named subject. For an ordered comparison, resolve first and second by their order. Keep why/how/what happens and every condition. If the subject is already named, leave the question unchanged. Return a complete question, not keywords.")
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

func stripFences(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}
