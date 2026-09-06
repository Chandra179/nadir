package api

import (
	"io"
	"strings"
	"testing"
	"time"

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
			Turns:       []turnView{{Query: "q", Count: 1, HasAnswer: true, Answer: "a"}},
		}},
		{"turn", turnView{Query: "q", AttachedFiles: []string{"<script>.md"}, TopK: 5, Generate: true, HasAnswer: true, Answer: "a"}},
		{"turn", turnView{Query: "q", RewrittenQuery: "standalone?", TurnID: "t1", StreamURL: "/retrieval/turns/t1/events"}},
		{"history-sessions", historySessionsView{Enabled: true, Sessions: []history.Session{{ID: "s1", Title: "t", UpdatedAt: time.Now(), TurnCount: 2}}}},
		{"chips-ok", []string{"trig-functions.md"}},
		{"chips-error", struct{ Message string }{"no files provided"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if uiTemplates.Lookup(tc.name) == nil {
				t.Fatalf("template %q not defined in embedded UI templates", tc.name)
			}
			if err := uiTemplates.ExecuteTemplate(io.Discard, tc.name, tc.data); err != nil {
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
		if err := uiTemplates.ExecuteTemplate(&buf, tc.name, tc.data); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if strings.Contains(buf.String(), "<script>") || strings.Contains(buf.String(), "<img") {
			t.Errorf("%s: unescaped markup leaked into output:\n%s", tc.name, buf.String())
		}
	}
}
