package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"nadir/internal/evaluation"
	config "nadir/internal/platform/configuration"
)

func TestValidateOnlyAcceptsReleaseGateFixtureWithoutConfig(t *testing.T) {
	golden := evaluation.GoldenSet{
		SchemaVersion: 2,
		Metadata: evaluation.GoldenSetMetadata{
			Dataset:     "production-user-query-sample-2026-q3",
			Provenance:  "consent-safe export reviewed by privacy owner",
			Consent:     "opt-in users, collection purpose documented",
			Judgment:    "two independent domain experts",
			ReleaseGate: true,
		},
		Queries: make([]evaluation.GoldenQuery, 100),
	}
	for i := range golden.Queries {
		golden.Queries[i] = evaluation.GoldenQuery{
			ID:                "production-query-" + formatIndex(i),
			Query:             "What is the documented answer to query " + formatIndex(i) + "?",
			Type:              evaluation.QueryTypeFactoid,
			FaithfulnessLabel: evaluation.FaithfulnessFullySupported,
			ExpectedAnswer:    "The answer is documented in the source.",
			RequiredClaims:    []string{"the answer is documented"},
			Relevant:          []evaluation.RelevantChunk{{File: "doc.md", Contains: "documented"}},
		}
	}

	path := filepath.Join(t.TempDir(), "production-golden.json")
	data, err := json.Marshal(golden)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	// An invalid config path proves validate-only does not initialize Qdrant,
	// Ollama, or any other external runtime dependency.
	if err := run(filepath.Join(t.TempDir(), "missing.yaml"), path, 0, false, 1, "", false, true, true); err != nil {
		t.Fatalf("validate-only: %v", err)
	}
}

func formatIndex(index int) string {
	return fmt.Sprintf("%03d", index)
}

func TestValidateGenerationOptionsRequiresExplicitLargerJudge(t *testing.T) {
	cfg := &config.Config{Generator: config.GeneratorConfig{Enabled: true, Model: "gemma3:1b"}}
	if err := validateGenerationOptions(cfg, generationOptions{Enabled: true, JudgeAddr: "http://localhost:11434", JudgeModel: "gemma3:1b", JudgeLarger: true}); err == nil {
		t.Fatal("validateGenerationOptions accepted the answer model as judge")
	}
	if err := validateGenerationOptions(cfg, generationOptions{Enabled: true, JudgeAddr: "http://localhost:11434", JudgeModel: "phi4-mini:latest"}); err == nil {
		t.Fatal("validateGenerationOptions accepted an unconfirmed larger judge")
	}
	if err := validateGenerationOptions(cfg, generationOptions{Enabled: true, JudgeAddr: "http://localhost:11434", JudgeModel: "phi4-mini:latest", JudgeLarger: true}); err != nil {
		t.Fatalf("validateGenerationOptions(valid) = %v", err)
	}
}
