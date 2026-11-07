package enrichment

import (
	"context"
	"fmt"
	"strings"
)

// Completer runs one system+user chat exchange against a language model and
// returns the reply text. Transport, model choice and sampling options belong
// to the provider Adapter that implements it.
type Completer func(ctx context.Context, system, user string) (string, error)

const contextualSystemPrompt = `You write document context lines. Given an excerpt of a document and one chunk from it, write ONE short sentence (<30 words) situating the chunk: which document/topic it belongs to and any key entities or terms needed to understand it out of order. Reply ONLY with the sentence itself — no preamble, no quotes.`

type llmEnricher struct{ complete Completer }

// NewLLM returns an Enricher that builds the contextual-intro prompt and
// cleans the model's reply; the model call itself is delegated to complete.
func NewLLM(complete Completer) Enricher {
	return &llmEnricher{complete: complete}
}

func (e *llmEnricher) ContextualIntro(ctx context.Context, documentExcerpt, chunkText string) (string, error) {
	prompt := fmt.Sprintf("Document excerpt:\n%s\n\nChunk:\n%s", documentExcerpt, chunkText)
	out, err := e.complete(ctx, contextualSystemPrompt, prompt)
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

func stripFences(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}
