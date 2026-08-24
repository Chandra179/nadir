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
	cases := map[string]any{
		"page": pageView{
			DefaultTopK: 8,
			SessionID:   "s1",
			Turns:       []turnView{{Query: "q", Count: 1, HasAnswer: true, Answer: "a"}},
		},
		"turn":             turnView{Query: "q", AttachedFiles: []string{"<script>.md"}, TopK: 5, Generate: true},
		"history-sessions": historySessionsView{Enabled: true, Sessions: []history.Session{{ID: "s1", Title: "t", UpdatedAt: time.Now(), TurnCount: 2}}},
		"settings":         struct{ Groups []settingsGroup }{Groups: []settingsGroup{{Name: "HTTP", Items: []settingsItem{{Key: "k", Value: "<v>"}}}}},
		"chips-ok":         []string{"trig-functions.md"},
		"chips-error":      struct{ Message string }{"no files provided"},
	}

	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if uiTemplates.Lookup(name) == nil {
				t.Fatalf("template %q not defined in embedded UI templates", name)
			}
			if err := uiTemplates.ExecuteTemplate(io.Discard, name, data); err != nil {
				t.Fatalf("execute %q: %v", name, err)
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
