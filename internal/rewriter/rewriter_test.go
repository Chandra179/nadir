package rewriter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRewriteCleansOutput(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want string
	}{
		{"plain", "What does the secant formula compute?", "What does the secant formula compute?"},
		{"quoted", `"How is the derivative of x^n defined?"`, "How is the derivative of x^n defined?"},
		{"fenced", "```\nwhat is the power rule?\n```", "what is the power rule?"},
		{"labeled", "Standalone search query: secant formula slope", "secant formula slope"},
		{"whitespace", "  what   about   limits? \n", "what about limits?"},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{"content": c.out}})
		}))
		d := NewDependencies(DependenciesConfig{Addr: srv.URL, Model: "test"})
		got, err := d.Rewrite(context.Background(), []Turn{{Query: "prior"}}, "follow-up?")
		srv.Close()
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", c.name, err)
		}
		if got != c.want {
			t.Errorf("%s: Rewrite cleaned output = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestRewriteErrorOnEmptyOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{"content": "   "}})
	}))
	defer srv.Close()

	d := NewDependencies(DependenciesConfig{Addr: srv.URL, Model: "test"})
	if _, err := d.Rewrite(context.Background(), nil, "q?"); err == nil {
		t.Fatal("expected error for empty rewrite output")
	}
}

func TestRewriteRejectsEmptyQuery(t *testing.T) {
	d := NewDependencies(DependenciesConfig{Addr: "http://unused", Model: "test"})
	if _, err := d.Rewrite(context.Background(), nil, "   "); err == nil {
		t.Fatal("expected error for empty query")
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
