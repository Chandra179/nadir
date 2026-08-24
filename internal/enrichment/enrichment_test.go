package enrichment

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseStringList(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"plain array", `["a","b"]`, 2},
		{"fenced array", "```json\n[\"q1\",\"q2\",\"q3\"]\n```", 3},
		{"prose around array", `Sure! Here you go: ["what is x?"] hope that helps.`, 1},
		{"fallback quoted strings", `1. "first question here" 2. "second question"`, 2},
		{"nothing", `I cannot answer that.`, 0},
	}
	for _, c := range cases {
		got := parseStringList(c.in)
		if len(got) != c.want {
			t.Errorf("%s: parseStringList(%q) = %v, want %d items", c.name, c.in, got, c.want)
		}
	}
}

func TestHypotheticalQuestionsCapsAndCleans(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]string{
				"content": `["  what is the power rule? ", "", "how to differentiate x^n?", "extra question?", "another extra?"]`,
			},
		})
	}))
	defer srv.Close()

	d := NewDependencies(DependenciesConfig{Addr: srv.URL, Model: "test"})
	qs, err := d.HypotheticalQuestions(context.Background(), "Power Rule", "If f(x)=x^n...", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(qs) != 3 {
		t.Fatalf("got %d questions, want 3", len(qs))
	}
	if qs[0] != "what is the power rule?" {
		t.Errorf("question not trimmed: %q", qs[0])
	}
}

func TestHypotheticalQuestionsErrorWhenEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{"content": "no lists here"}})
	}))
	defer srv.Close()

	d := NewDependencies(DependenciesConfig{Addr: srv.URL, Model: "test"})
	if _, err := d.HypotheticalQuestions(context.Background(), "h", "t", 3); err == nil {
		t.Fatal("expected error for unparsable output")
	}
}

func TestContextualIntroCleansOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]string{"content": "  \"This chunk is from a calculus cheat sheet covering derivative rules.\"  "},
		})
	}))
	defer srv.Close()

	d := NewDependencies(DependenciesConfig{Addr: srv.URL, Model: "test"})
	intro, err := d.ContextualIntro(context.Background(), "excerpt", "chunk text")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if intro != "This chunk is from a calculus cheat sheet covering derivative rules." {
		t.Errorf("intro not cleaned: %q", intro)
	}
}
