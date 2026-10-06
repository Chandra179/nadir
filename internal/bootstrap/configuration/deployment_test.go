package config

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The supported deployment must not silently override the model profile
// validated in the local YAML. Addresses differ across host/container modes.
func TestDeploymentModelDefaultsMatchLocalConfiguration(t *testing.T) {
	data, err := os.ReadFile("config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var local Config
	if err := yaml.Unmarshal(data, &local); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile("../../../deploy/compose/compose.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var compose struct {
		Services map[string]struct {
			Environment map[string]string `yaml:"environment"`
			Profiles    []string          `yaml:"profiles"`
			DependsOn   map[string]any    `yaml:"depends_on"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &compose); err != nil {
		t.Fatal(err)
	}
	defaults := map[string]string{
		"EMBEDDER_MODEL":           local.Embedder.Model,
		"EMBEDDER_QUERY_PREFIX":    local.Embedder.QueryPrefix,
		"EMBEDDER_DOCUMENT_PREFIX": local.Embedder.DocumentPrefix,
		"GENERATOR_MODEL":          local.Generator.Model,
		"REWRITE_MODEL":            local.Rewriter.Model,
		"CONTEXTUAL_MODEL":         local.Enrichment.Contextual.Model,
		"GENERATOR_THINK":          "false",
		"REWRITE_THINK":            "false",
		"CONTEXTUAL_THINK":         "false",
		"RERANKER_ENABLED":         "false",
	}
	for name, want := range defaults {
		raw := compose.Services["app"].Environment[name]
		_, value, ok := strings.Cut(raw, ":-")
		if !ok || !strings.HasSuffix(value, "}") || strings.TrimSuffix(value, "}") != want {
			t.Errorf("Compose %s = %q, want default %q", name, raw, want)
		}
	}
	if local.Reranker.Enabled {
		t.Error("reranker must remain opt-in")
	}
	if _, required := compose.Services["app"].DependsOn["reranker"]; required {
		t.Error("disabled reranker must not block API startup")
	}
	if p := compose.Services["reranker"].Profiles; len(p) != 1 || p[0] != "rerank" {
		t.Errorf("reranker profile = %v", p)
	}
}
