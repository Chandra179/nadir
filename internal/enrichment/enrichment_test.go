package enrichment

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
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

	d := NewDependencies(DependenciesConfig{HypeAddr: srv.URL, HypeModel: "test"})
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

	d := NewDependencies(DependenciesConfig{HypeAddr: srv.URL, HypeModel: "test"})
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

	d := NewDependencies(DependenciesConfig{ContextualAddr: srv.URL, ContextualModel: "test"})
	intro, err := d.ContextualIntro(context.Background(), "excerpt", "chunk text")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if intro != "This chunk is from a calculus cheat sheet covering derivative rules." {
		t.Errorf("intro not cleaned: %q", intro)
	}
}

func TestEnrichmentHTTPContract(t *testing.T) {
	for _, tt := range []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{name: "status error", status: http.StatusBadGateway, body: "unavailable", wantErr: "status 502"},
		{name: "malformed json", status: http.StatusOK, body: `{`, wantErr: "decode"},
		{name: "response shape mismatch", status: http.StatusOK, body: `{}`, wantErr: "no questions parsed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			d := NewDependencies(DependenciesConfig{HypeAddr: srv.URL, HypeModel: "test", RequestTimeout: time.Second})
			_, err := d.HypotheticalQuestions(context.Background(), "header", "text", 2)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("HypotheticalQuestions() error = %v, want %q", err, tt.wantErr)
			}
		})
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{"content": ""}})
	}))
	defer srv.Close()
	d := NewDependencies(DependenciesConfig{ContextualAddr: srv.URL, ContextualModel: "test"})
	if _, err := d.ContextualIntro(context.Background(), "excerpt", "chunk"); err == nil || !strings.Contains(err.Error(), "empty contextual intro") {
		t.Fatalf("ContextualIntro() error = %v, want empty response-shape error", err)
	}
}

func TestEnrichmentHonorsTimeoutAndCancellation(t *testing.T) {
	started := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(started) })
		select {
		case <-r.Context().Done():
		case <-time.After(100 * time.Millisecond):
		}
	}))
	defer srv.Close()

	d := NewDependencies(DependenciesConfig{HypeAddr: srv.URL, HypeModel: "test", RequestTimeout: 10 * time.Millisecond})
	if _, err := d.HypotheticalQuestions(context.Background(), "header", "text", 2); err == nil {
		t.Fatal("HypotheticalQuestions() succeeded after client timeout")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := d.HypotheticalQuestions(ctx, "header", "text", 2)
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("enrichment request did not start")
	}
	cancel()
	if err := <-result; err == nil {
		t.Fatal("HypotheticalQuestions() succeeded with canceled request")
	}
}
