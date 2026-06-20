package eval

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// GoldenQuery is a labeled query with known-relevant files and optional expected answer.
type GoldenQuery struct {
	Query           string         `yaml:"query"`
	ExpectedFiles   []string       `yaml:"expected_files"`
	Relevance       map[string]int `yaml:"relevance,omitempty"`       // graded: 0=irrelevant, 1-3=graded; overrides expected_files when set
	ExpectedHeaders []string       `yaml:"expected_headers,omitempty"`
	ExpectedAnswer  string         `yaml:"expected_answer,omitempty"` // for RAG mode (RAGAS context recall)
	Notes           string         `yaml:"notes,omitempty"`
}

// GoldenSet is the full labeled dataset.
type GoldenSet struct {
	Queries []GoldenQuery `yaml:"queries"`
}

// LoadGolden reads a YAML golden set from path.
func LoadGolden(path string) (*GoldenSet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var gs GoldenSet
	if err := yaml.Unmarshal(data, &gs); err != nil {
		return nil, err
	}
	return &gs, nil
}

// MatchFile reports whether retrievedPath corresponds to expectedFile.
// Matching is by path suffix so users can write "math/trigonometry.md"
// and still match a stored "gitbook/math/trigonometry.md".
func MatchFile(retrievedPath, expectedFile string) bool {
	r := filepath.ToSlash(filepath.Clean(retrievedPath))
	e := filepath.ToSlash(filepath.Clean(expectedFile))
	if r == e {
		return true
	}
	if strings.HasSuffix(r, "/"+e) {
		return true
	}
	if !strings.Contains(e, "/") && filepath.Base(r) == e {
		return true
	}
	return false
}

// GradedRelevance returns the graded relevance map for a query.
// If Relevance is set, it uses those grades directly.
// Otherwise, it builds a binary map (grade=1) from ExpectedFiles.
// Keys are cleaned (filepath.ToSlash + filepath.Clean) for consistent matching.
func (q GoldenQuery) GradedRelevance() map[string]int {
	if len(q.Relevance) > 0 {
		out := make(map[string]int, len(q.Relevance))
		for f, g := range q.Relevance {
			out[filepath.ToSlash(filepath.Clean(f))] = g
		}
		return out
	}
	out := make(map[string]int, len(q.ExpectedFiles))
	for _, f := range q.ExpectedFiles {
		out[filepath.ToSlash(filepath.Clean(f))] = 1
	}
	return out
}
