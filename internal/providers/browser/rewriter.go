//go:build js && wasm

package browser

import (
	"context"
	"fmt"
	"strings"
	"syscall/js"

	conversationrewriting "nadir/internal/core/conversation/rewriting"
	ollamarewriter "nadir/internal/providers/ollama/rewriter"
)

// Rewriter resolves follow-up questions with a host-page LLM, using the same
// prompt wording and output cleanup as the Ollama adapter.
type Rewriter struct {
	complete js.Value
}

// NewRewriter wraps complete(system, user) => Promise<string>, a short
// deterministic chat completion.
func NewRewriter(complete js.Value) (*Rewriter, error) {
	if complete.Type() != js.TypeFunction {
		return nil, fmt.Errorf("complete must be a function")
	}
	return &Rewriter{complete: complete}, nil
}

func (r *Rewriter) Rewrite(ctx context.Context, turns []conversationrewriting.Turn, query string) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("rewriter: empty query")
	}
	system, user := ollamarewriter.Prompt(turns, query)
	out, err := Await(ctx, r.complete.Invoke(system, user))
	if err != nil {
		return "", fmt.Errorf("rewriter: %w", err)
	}
	rewritten := ollamarewriter.CleanQuery(out.String())
	if rewritten == "" {
		return "", fmt.Errorf("rewriter: empty rewrite from model output")
	}
	return rewritten, nil
}
