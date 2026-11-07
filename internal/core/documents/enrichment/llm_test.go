package enrichment

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestContextualIntroCleansOutput(t *testing.T) {
	e := NewLLM(func(context.Context, string, string) (string, error) {
		return "  \"This chunk is from a calculus cheat sheet covering derivative rules.\"  ", nil
	})
	got, err := e.ContextualIntro(context.Background(), "excerpt", "chunk text")
	if err != nil || got != "This chunk is from a calculus cheat sheet covering derivative rules." {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestContextualIntroErrors(t *testing.T) {
	ctx := context.Background()
	empty := NewLLM(func(context.Context, string, string) (string, error) { return " ", nil })
	if _, err := empty.ContextualIntro(ctx, "e", "c"); err == nil || !strings.Contains(err.Error(), "empty contextual intro") {
		t.Fatalf("empty err = %v", err)
	}
	boom := errors.New("boom")
	failing := NewLLM(func(context.Context, string, string) (string, error) { return "", boom })
	if _, err := failing.ContextualIntro(ctx, "e", "c"); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

func TestContextualIntroSendsSystemPromptAndExcerpt(t *testing.T) {
	var system, user string
	e := NewLLM(func(_ context.Context, s, u string) (string, error) {
		system, user = s, u
		return "intro", nil
	})
	if _, err := e.ContextualIntro(context.Background(), "excerpt", "chunk"); err != nil {
		t.Fatal(err)
	}
	if system != contextualSystemPrompt || user != "Document excerpt:\nexcerpt\n\nChunk:\nchunk" {
		t.Fatalf("system=%q user=%q", system, user)
	}
}
