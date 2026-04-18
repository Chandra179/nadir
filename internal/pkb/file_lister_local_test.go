package pkb

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMatchPattern(t *testing.T) {
	tests := []struct {
		pattern string
		path    string
		want    bool
	}{
		// ** glob (recursive dir)
		{".gitbook/**", ".gitbook/assets/img.png", true},
		{".gitbook/**", ".gitbook/README.md", true},
		{".gitbook/**", "docs/.gitbook/file.md", false},
		{".git/**", ".git/config", true},
		{"node_modules/**", "node_modules/foo/bar.js", true},

		// exact file match
		{"CLAUDE.md", "CLAUDE.md", true},
		{"CLAUDE.md", "docs/CLAUDE.md", false},
		{"package.json", "package.json", true},

		// wildcard extension
		{"*.config.js", "vite.config.js", true},
		{"*.config.js", "tailwind.config.js", true},
		{"*.config.js", "config.js", false},
		{"*.jsonc", "tsconfig.jsonc", true},

		// nested dir not ignored
		{"scripts/**", "scripts/build.sh", true},
		{"scripts/**", "docs/scripts/build.sh", false},

		// dirs passed with trailing slash (directory walk)
		{".gitbook/**", ".gitbook/", true},
		{"node_modules/**", "node_modules/", true},
	}

	for _, tc := range tests {
		got := matchPattern(tc.pattern, tc.path)
		if got != tc.want {
			t.Errorf("matchPattern(%q, %q) = %v, want %v", tc.pattern, tc.path, got, tc.want)
		}
	}
}

func TestLocalFileLister_shouldIgnore(t *testing.T) {
	patterns := []string{
		".gitbook/**",
		".git/**",
		"node_modules/**",
		"scripts/**",
		"package.json",
		"*.config.js",
		"CLAUDE.md",
	}
	l := NewLocalFileLister(".", patterns)

	ignored := []string{
		".gitbook/assets/logo.png",
		".git/HEAD",
		"node_modules/react/index.js",
		"scripts/deploy.sh",
		"package.json",
		"vite.config.js",
		"CLAUDE.md",
	}
	for _, p := range ignored {
		if !l.shouldIgnore(p) {
			t.Errorf("shouldIgnore(%q) = false, want true", p)
		}
	}

	allowed := []string{
		"docs/intro.md",
		"README.md",
		"api/reference.md",
	}
	for _, p := range allowed {
		if l.shouldIgnore(p) {
			t.Errorf("shouldIgnore(%q) = true, want false", p)
		}
	}
}

func TestLocalFileLister_ListMarkdownFiles(t *testing.T) {
	// build temp dir tree
	root := t.TempDir()

	files := map[string]string{
		"README.md":                 "# root",
		"docs/intro.md":             "# intro",
		"docs/api.md":               "# api",
		".gitbook/assets/logo.md":   "hidden",
		"node_modules/pkg/notes.md": "hidden",
		"scripts/run.md":            "hidden",
		"docs/image.png":            "not md",
	}
	for rel, content := range files {
		abs := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	patterns := []string{".gitbook/**", "node_modules/**", "scripts/**"}
	lister := NewLocalFileLister(root, patterns)

	entries, err := lister.ListMarkdownFiles(context.TODO(), "")
	if err != nil {
		t.Fatalf("ListMarkdownFiles error: %v", err)
	}

	got := make(map[string]bool)
	for _, e := range entries {
		got[e.Path] = true
	}

	want := []string{"README.md", "docs/intro.md", "docs/api.md"}
	for _, w := range want {
		if !got[w] {
			t.Errorf("expected %q in results, not found", w)
		}
	}

	notWant := []string{
		".gitbook/assets/logo.md",
		"node_modules/pkg/notes.md",
		"scripts/run.md",
		"docs/image.png",
	}
	for _, nw := range notWant {
		if got[nw] {
			t.Errorf("expected %q excluded, but found in results", nw)
		}
	}

	if len(entries) != len(want) {
		t.Errorf("got %d entries, want %d", len(entries), len(want))
	}
}
