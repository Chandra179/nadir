package ingest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverFilesIsStableAndHonorsIgnorePatterns(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, contents := range map[string]string{
		"z.md":           "z",
		"nested/a.md":    "a",
		"nested/skip.md": "skip",
		"notes.txt":      "not markdown",
		".hidden.md":     "hidden",
	} {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	files, err := DiscoverFiles([]string{root}, []string{"nested/skip.md", ".hidden.md"}, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("discovered %d files, want 2", len(files))
	}
	if filepath.Base(files[0].Name) != "a.md" || filepath.Base(files[1].Name) != "z.md" {
		t.Fatalf("files are not stable and sorted: %+v", files)
	}
}

func TestDiscoverFilesRejectsOversizedFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "large.md")
	if err := os.WriteFile(path, []byte("12345"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := DiscoverFiles([]string{root}, nil, 4); err == nil {
		t.Fatal("expected oversized source error")
	}
}
