package api

import (
	"io"
	"strings"
	"testing"
	"time"

	chatapi "nadir/internal/api/chat"
	historyapi "nadir/internal/api/history"
	"nadir/internal/api/internal/render"
	"nadir/internal/history"
)

// TestUITemplatesExecute guards the embedded template set: every fragment
// a handler can render must parse (enforced at package init via
// template.Must) and execute against representative data.
func TestUITemplatesExecute(t *testing.T) {
	// A template may appear multiple times with different representative
	// data (e.g. "turn" renders both replayed and streaming turns).
	cases := []struct {
		name string
		data any
	}{
		{"page", pageView{
			DefaultTopK: 8,
			SessionID:   "s1",
			Turns:       []chatapi.TurnView{{Query: "q", Count: 1, HasAnswer: true, Answer: "a"}},
		}},
		{"turn", chatapi.TurnView{Query: "q", AttachedFiles: []string{"<script>.md"}, TopK: 5, Generate: true, HasAnswer: true, Answer: "a"}},
		{"turn", chatapi.TurnView{Query: "q", RewrittenQuery: "standalone?", TurnID: "t1", StreamURL: "/retrieval/turns/t1/events"}},
		{"history-sessions", historyapi.SessionsView{Enabled: true, Sessions: []history.Session{{ID: "s1", Title: "t", UpdatedAt: time.Now(), TurnCount: 2}}}},
		{"chips-ok", []string{"trig-functions.md"}},
		{"chips-error", struct{ Message string }{"no files provided"}},
		{"feedback", feedbackView{}},
		{"feedback", feedbackView{Error: "delete failed: boom"}},
	}

	engine := render.New(nil)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := engine.Execute(io.Discard, tc.name, tc.data); err != nil {
				t.Fatalf("execute %q: %v", tc.name, err)
			}
		})
	}
}

// TestChipsEscape verifies user-controlled upload names and error messages
// are HTML-escaped by the engine rather than injected raw.
func TestChipsEscape(t *testing.T) {
	for _, tc := range []struct {
		name string
		data any
	}{
		{"chips-ok", []string{`x"><script>alert(1)</script>`}},
		{"chips-error", struct{ Message string }{`<img src=x onerror=alert(1)>`}},
	} {
		var buf strings.Builder
		engine := render.New(nil)
		if err := engine.Execute(&buf, tc.name, tc.data); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if strings.Contains(buf.String(), "<script>") || strings.Contains(buf.String(), "<img") {
			t.Errorf("%s: unescaped markup leaked into output:\n%s", tc.name, buf.String())
		}
	}
}
