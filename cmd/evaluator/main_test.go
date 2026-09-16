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
		SchemaVersion: 3,
		Metadata: evaluation.GoldenSetMetadata{
			Dataset:                "production-user-query-sample-2026-q3",
			Provenance:             "consent-safe export reviewed by privacy owner",
			Consent:                "opt-in users, collection purpose documented",
			Judgment:               "two independent domain experts",
			ReleaseGate:            true,
			JudgmentArtifactPath:   "reviews/arqmath-judgments.jsonl",
			JudgmentArtifactSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			Source: &evaluation.SourceMetadata{
				Name:              "ARQMath public evaluation collection",
				Homepage:          "https://www.cs.rit.edu/~dprl/ARQMath/",
				License:           "Math Stack Exchange CC BY-SA with ARQMath non-commercial snapshot terms",
				Usage:             "non-commercial evaluation with attribution",
				Snapshot:          "ARQMath-1 through ARQMath-3 Task 1 snapshots",
				Attribution:       "Mansouri et al., ARQMath; Math Stack Exchange contributors",
				LicenseNoticePath: "LICENSE-NOTICE.md",
				ArtifactSHA256: map[string]string{
					"Posts.V1.3.zip": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
					"topics.xml":     "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
				},
			},
			Corpus: &evaluation.CorpusMetadata{
				ID:             "production-corpus-2026-q3",
				Documents:      []string{"docs/guide.md"},
				DocumentCount:  1,
				Representative: true,
				ManifestPath:   "manifests/corpus.jsonl",
				ManifestSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			},
			PrivacyReview: &evaluation.PrivacyReviewMetadata{
				Status:         "approved",
				Reviewer:       "privacy-owner@example.test",
				ReviewedAt:     "2026-09-16T00:00:00Z",
				EvidencePath:   "reviews/privacy-approval.md",
				EvidenceSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			},
			Annotators: []evaluation.AnnotatorMetadata{
				{ID: "expert-a", Role: "domain expert", Human: true, Independent: true, VerificationRef: "reviews/expert-a-attestation.md"},
				{ID: "expert-b", Role: "domain expert", Human: true, Independent: true, VerificationRef: "reviews/expert-b-attestation.md"},
			},
		},
		Queries: make([]evaluation.GoldenQuery, 100),
	}
	for i := range golden.Queries {
		golden.Queries[i] = evaluation.GoldenQuery{
			ID:                 "production-query-" + formatIndex(i),
			Query:              "What is the documented answer to query " + formatIndex(i) + "?",
			Type:               evaluation.QueryTypeFactoid,
			FaithfulnessLabel:  evaluation.FaithfulnessFullySupported,
			ExpectedAnswer:     "The answer is documented in the source.",
			RequiredClaims:     []string{"the answer is documented"},
			CandidateDocuments: []evaluation.CandidateDocument{{DocumentID: "doc-1", SourceGrade: 2}},
			Relevant:           []evaluation.RelevantChunk{{File: "doc.md", Contains: "documented"}},
			Judgments: []evaluation.RelevanceJudgment{
				{AnnotatorID: "expert-a", Relevant: []evaluation.RelevantChunk{{File: "doc.md", Contains: "documented"}}},
				{AnnotatorID: "expert-b", Relevant: []evaluation.RelevantChunk{{File: "doc.md", Contains: "documented"}}},
			},
			Adjudication: &evaluation.AdjudicationMetadata{
				Method:      "independent labels reviewed and adjudicated",
				ReviewerIDs: []string{"expert-a", "expert-b"},
			},
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
