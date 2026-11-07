package rewriting

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func fake(out string, err error) Completer {
	return func(context.Context, string, string) (string, error) { return out, err }
}

func TestRewriteCleansOutput(t *testing.T) {
	cases := []struct{ name, out, want string }{
		{"plain", "What does the secant formula compute?", "What does the secant formula compute?"},
		{"quoted", `"How is the derivative of x^n defined?"`, "How is the derivative of x^n defined?"},
		{"fenced", "```\nwhat is the power rule?\n```", "what is the power rule?"},
		{"labeled", "Standalone search query: secant formula slope", "secant formula slope"},
		{"whitespace", "  what   about   limits? \n", "what about limits?"},
	}
	for _, c := range cases {
		got, err := NewLLM(fake(c.out, nil)).Rewrite(context.Background(), []Turn{{Query: "prior"}}, "follow-up?")
		if err != nil || got != c.want {
			t.Errorf("%s: got %q, %v; want %q", c.name, got, err, c.want)
		}
	}
}

func TestRewriteErrors(t *testing.T) {
	ctx := context.Background()
	if _, err := NewLLM(fake("   ", nil)).Rewrite(ctx, nil, "q?"); err == nil || !strings.Contains(err.Error(), "empty rewrite") {
		t.Fatalf("empty output err = %v", err)
	}
	if _, err := NewLLM(fake("x", nil)).Rewrite(ctx, nil, "   "); err == nil || !strings.Contains(err.Error(), "empty query") {
		t.Fatalf("empty query err = %v", err)
	}
	boom := errors.New("boom")
	if _, err := NewLLM(fake("", boom)).Rewrite(ctx, nil, "q?"); !errors.Is(err, boom) {
		t.Fatalf("completer error = %v", err)
	}
}

func TestRewriteSendsSystemPromptAndBuiltPrompt(t *testing.T) {
	turns := []Turn{{Query: "a", Answer: "b"}}
	var gotSystem, gotUser string
	r := NewLLM(func(_ context.Context, system, user string) (string, error) {
		gotSystem, gotUser = system, user
		return "q?", nil
	})
	if _, err := r.Rewrite(context.Background(), turns, "c?"); err != nil {
		t.Fatal(err)
	}
	if gotSystem != systemPrompt || gotUser != buildPrompt(turns, "c?") {
		t.Fatalf("system=%q user=%q", gotSystem, gotUser)
	}
}

func TestBuildPromptShape(t *testing.T) {
	prompt := buildPrompt([]Turn{
		{Query: "first question"},
		{Query: "", Answer: "orphan answer is skipped"},
		{Query: "second question", Answer: strings.Repeat("a", maxAnswerChars+50)},
	}, "what about the second one?")

	for _, want := range []string{
		"User: first question",
		"User: second question",
		"Follow-up question: what about the second one?",
		"Standalone search query:",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q:\n%s", want, prompt)
		}
	}
	if !strings.Contains(prompt, "…") {
		t.Error("long answer should be truncated")
	}
	if strings.Contains(prompt, "orphan") {
		t.Error("turns with empty queries must be skipped")
	}
	if got := strings.Count(prompt, "Assistant:"); got != 1 {
		t.Errorf("prompt shows %d assistant replies, want 1", got)
	}
}
