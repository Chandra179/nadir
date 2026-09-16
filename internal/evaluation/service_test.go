package evaluation

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/mock"
	"nadir/internal/retrieval/search"
	"nadir/mocks"
)

func TestHarnessBypassesCacheAndAggregatesResults(t *testing.T) {
	searcher := &mocks.MockRetriever{}
	called := 0
	var request search.Request
	searcher.EXPECT().Query(mock.Anything, mock.Anything).
		Run(func(_ context.Context, got search.Request) {
			called++
			request = got
		}).
		Return(search.Result{Chunks: []search.Chunk{
			{FilePath: "samples/math.md", Text: "The answer is 42."},
			{FilePath: "samples/other.md", Text: "unrelated"},
		}}, nil).
		Twice()
	harness := NewDependencies(DependenciesConfig{Searcher: searcher})
	report, err := harness.Run(context.Background(), &GoldenSet{Queries: []GoldenQuery{{
		ID:    "answer",
		Query: "what is the answer",
		Relevant: []RelevantChunk{{
			File:     "math.md",
			Contains: "42",
		}},
	}}}, 2, 2)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if called != 2 {
		t.Fatalf("Query calls = %d, want 2", called)
	}
	if !request.SkipCache {
		t.Fatal("evaluation must bypass semantic cache")
	}
	if report.Aggregate.HitRateAtK != 1 || report.Aggregate.MRRAt10 != 1 {
		t.Fatalf("aggregate = %+v, want perfect first-rank result", report.Aggregate)
	}
	if report.PerQuery[0].DistractorHits != 0 {
		t.Fatalf("distractor hits = %d, want 0", report.PerQuery[0].DistractorHits)
	}
}

func TestLoadGoldenSetRejectsDuplicateIDs(t *testing.T) {
	path := t.TempDir() + "/golden.json"
	if err := os.WriteFile(path, []byte(`{"queries":[{"id":"q","query":"one","relevant":[{"contains":"one"}]},{"id":"q","query":"two","relevant":[{"contains":"two"}]}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadGoldenSet(path); err == nil {
		t.Fatal("LoadGoldenSet accepted duplicate IDs")
	}
}

func TestMatchedRelevantSupportsHostAndContainerPaths(t *testing.T) {
	chunk := search.Chunk{FilePath: "/app/source/trig-functions.md", Text: "reciprocal of cosine"}
	matched := MatchedRelevant(chunk, []RelevantChunk{{File: "samples/trig-functions.md", Contains: "RECIPROCAL"}})
	if len(matched) != 1 || matched[0] != 0 {
		t.Fatalf("MatchedRelevant() = %v, want [0]", matched)
	}
}

func TestMatchedRelevantIncludesSectionHeaders(t *testing.T) {
	chunk := search.Chunk{FilePath: "linear-algebra.md", Header: "Special Matrices", Text: "Identity matrices have ones on the diagonal."}
	matched := MatchedRelevant(chunk, []RelevantChunk{{File: "linear-algebra.md", Contains: "Special Matrices"}})
	if len(matched) != 1 || matched[0] != 0 {
		t.Fatalf("MatchedRelevant() = %v, want header match", matched)
	}
}

func TestLoadGoldenSetRequiresAnnotationsForSchemaV2(t *testing.T) {
	path := t.TempDir() + "/golden.json"
	data := []byte(`{"schema_version":2,"queries":[{"id":"q","query":"one","relevant":[{"contains":"one"}]}]}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadGoldenSet(path); err == nil {
		t.Fatal("LoadGoldenSet accepted schema v2 query without annotations")
	}
}

func TestLoadGoldenSetRequiresJudgmentsForSchemaV3(t *testing.T) {
	path := t.TempDir() + "/golden.json"
	data := []byte(`{"schema_version":3,"queries":[{"id":"q","query":"one","relevant":[{"contains":"one"}],"expected_answer":"one","required_claims":["one"],"faithfulness_label":"fully_supported"}]}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadGoldenSet(path); err == nil {
		t.Fatal("LoadGoldenSet accepted schema v3 query without independent judgment records")
	}
}

func TestMatchedDistractors(t *testing.T) {
	chunk := search.Chunk{FilePath: "/app/source/calculus.md", Text: "The chain rule composes derivatives."}
	matched := MatchedDistractors(chunk, []RelevantChunk{{File: "calculus.md", Contains: "chain rule"}})
	if len(matched) != 1 || matched[0] != 0 {
		t.Fatalf("MatchedDistractors() = %v, want [0]", matched)
	}
}

func TestActiveGoldenFixtureIsAnnotated(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(filepath.Dir(sourceFile), "../../test/evaluation/golden.json")
	golden, err := LoadGoldenSet(path)
	if err != nil {
		t.Fatalf("LoadGoldenSet(active fixture): %v", err)
	}
	if golden.SchemaVersion != 3 {
		t.Fatalf("schema version = %d, want 3", golden.SchemaVersion)
	}
	if len(golden.Queries) < 100 {
		t.Fatalf("query count = %d, want at least 100", len(golden.Queries))
	}
	if golden.Metadata.Dataset != "expert-authored-synthetic-user-intent-candidate" {
		t.Fatalf("dataset = %q, want expert-authored synthetic fixture", golden.Metadata.Dataset)
	}
	if golden.Metadata.ReleaseGate {
		t.Fatal("synthetic fixture must not be marked as a production release gate")
	}
	if golden.Metadata.Corpus == nil || golden.Metadata.Corpus.DocumentCount != 4 {
		t.Fatalf("corpus metadata = %+v, want four-document manifest", golden.Metadata.Corpus)
	}
	if len(golden.Metadata.Annotators) != 2 {
		t.Fatalf("annotators = %d, want two synthetic passes", len(golden.Metadata.Annotators))
	}
	ambiguous, negative := 0, 0
	for _, query := range golden.Queries {
		if len(query.Distractors) == 0 {
			t.Errorf("query %q has no distractor annotation", query.ID)
		}
		if len(query.Judgments) != 2 || query.Adjudication == nil {
			t.Errorf("query %q does not have two judgments and adjudication", query.ID)
		}
		for _, tag := range query.Tags {
			switch tag {
			case "ambiguous":
				ambiguous++
			case "negative":
				negative++
			}
		}
	}
	if ambiguous == 0 || negative == 0 {
		t.Fatalf("tag coverage = ambiguous:%d negative:%d, want both categories", ambiguous, negative)
	}
}

func TestSyntheticGoldenFixtureCannotPassReleaseGate(t *testing.T) {
	golden := &GoldenSet{
		SchemaVersion: 3,
		Metadata: GoldenSetMetadata{
			Dataset:     "expert-authored-synthetic-user-intent",
			Consent:     "not-applicable-no-production-user-data",
			Judgment:    "single-expert",
			ReleaseGate: true,
		},
		Queries: make([]GoldenQuery, 100),
	}
	for i := range golden.Queries {
		golden.Queries[i] = GoldenQuery{
			ID: "q-" + string(rune('a'+i%26)), Query: "query", Type: QueryTypeFactoid,
			FaithfulnessLabel: FaithfulnessFullySupported, ExpectedAnswer: "answer",
			RequiredClaims: []string{"claim"}, Relevant: []RelevantChunk{{File: "doc.md"}},
		}
	}
	if err := golden.ValidateReleaseGate(); err == nil {
		t.Fatal("synthetic golden fixture passed release gate validation")
	}
}

func TestReleaseGateRequiresProvenance(t *testing.T) {
	golden := &GoldenSet{
		SchemaVersion: 3,
		Metadata: GoldenSetMetadata{
			Dataset:     "production-user-queries",
			Consent:     "consented opt-in sample",
			Judgment:    "two independent experts",
			ReleaseGate: true,
		},
		Queries: make([]GoldenQuery, 100),
	}
	for i := range golden.Queries {
		golden.Queries[i] = GoldenQuery{
			ID: "production-" + string(rune('a'+i%26)) + string(rune('0'+i/26)), Query: "query", Type: QueryTypeFactoid,
			FaithfulnessLabel: FaithfulnessFullySupported, ExpectedAnswer: "answer",
			RequiredClaims: []string{"claim"}, Relevant: []RelevantChunk{{File: "doc.md"}},
		}
	}
	if err := golden.ValidateReleaseGate(); err == nil {
		t.Fatal("release gate accepted metadata without provenance")
	}
}

func TestReleaseGateRequiresHumanIndependentAnnotators(t *testing.T) {
	golden := completeReleaseGateFixture()
	golden.Metadata.Annotators[0].Human = false
	if err := golden.ValidateReleaseGate(); err == nil {
		t.Fatal("release gate accepted a non-human annotator")
	}
}

func TestReleaseGateRequiresEveryAnnotatorOnEveryQuery(t *testing.T) {
	golden := completeReleaseGateFixture()
	golden.Metadata.Annotators = append(golden.Metadata.Annotators, AnnotatorMetadata{
		ID: "expert-c", Role: "domain expert", Human: true, Independent: true,
		VerificationRef: "reviews/expert-c-attestation.md",
	})
	if err := golden.ValidateReleaseGate(); err == nil {
		t.Fatal("release gate accepted a query without both independent judgments")
	}
}

func TestReleaseGateRequiresRepresentativeCorpusAndPrivacyReview(t *testing.T) {
	golden := completeReleaseGateFixture()
	golden.Metadata.Corpus.Representative = false
	if err := golden.ValidateReleaseGate(); err == nil {
		t.Fatal("release gate accepted a non-representative corpus")
	}

	golden = completeReleaseGateFixture()
	golden.Metadata.PrivacyReview.Status = "pending"
	if err := golden.ValidateReleaseGate(); err == nil {
		t.Fatal("release gate accepted a pending privacy review")
	}
}

func TestReleaseGateRequiresVerifiedARQMathCollectionHash(t *testing.T) {
	golden := completeReleaseGateFixture()
	delete(golden.Metadata.Source.ArtifactSHA256, "Posts.V1.3.zip")
	if err := golden.ValidateReleaseGate(); err == nil {
		t.Fatal("release gate accepted source metadata without the collection artifact hash")
	}
}

func TestLoadGoldenSetRejectsDuplicateCandidateDocuments(t *testing.T) {
	path := t.TempDir() + "/golden.json"
	data := []byte(`{"schema_version":3,"queries":[{"id":"q","query":"one","candidate_documents":[{"document_id":"42","source_grade":2},{"document_id":"42","source_grade":1}],"relevant":[{"contains":"one"}],"expected_answer":"one","required_claims":["one"],"faithfulness_label":"fully_supported","judgments":[{"annotator_id":"a","relevant":[{"contains":"one"}]},{"annotator_id":"b","relevant":[{"contains":"one"}]}],"adjudication":{"method":"review","reviewer_ids":["a","b"]}}]}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadGoldenSet(path); err == nil {
		t.Fatal("LoadGoldenSet accepted duplicate candidate document IDs")
	}
}

func TestReleaseGateAcceptsCompleteEvidenceContract(t *testing.T) {
	if err := completeReleaseGateFixture().ValidateReleaseGate(); err != nil {
		t.Fatalf("complete release-gate fixture rejected: %v", err)
	}
}

func completeReleaseGateFixture() *GoldenSet {
	golden := &GoldenSet{
		SchemaVersion: 3,
		Metadata: GoldenSetMetadata{
			Dataset:                "production-user-queries-2026-q3",
			Provenance:             "consent-safe export with documented sampling window",
			Consent:                "opt-in users, collection purpose documented",
			Judgment:               "two independent human experts with per-query adjudication",
			ReleaseGate:            true,
			JudgmentArtifactPath:   "reviews/arqmath-judgments.jsonl",
			JudgmentArtifactSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			Source: &SourceMetadata{
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
			Corpus: &CorpusMetadata{
				ID:             "production-corpus-2026-q3",
				Documents:      []string{"docs/guide.md"},
				DocumentCount:  1,
				Representative: true,
				ManifestPath:   "manifests/corpus.jsonl",
				ManifestSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			},
			PrivacyReview: &PrivacyReviewMetadata{
				Status:         "approved",
				Reviewer:       "privacy-owner@example.test",
				ReviewedAt:     "2026-09-16T00:00:00Z",
				EvidencePath:   "reviews/privacy-approval.md",
				EvidenceSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			},
			Annotators: []AnnotatorMetadata{
				{ID: "expert-a", Role: "domain expert", Human: true, Independent: true, VerificationRef: "reviews/expert-a-attestation.md"},
				{ID: "expert-b", Role: "domain expert", Human: true, Independent: true, VerificationRef: "reviews/expert-b-attestation.md"},
			},
		},
		Queries: make([]GoldenQuery, 100),
	}
	for i := range golden.Queries {
		relevant := []RelevantChunk{{File: "docs/guide.md", Contains: "documented"}}
		golden.Queries[i] = GoldenQuery{
			ID:                 "production-" + string(rune('a'+i%26)) + string(rune('0'+i/26)),
			Query:              "What is the documented answer to query " + string(rune('a'+i%26)) + "?",
			Type:               QueryTypeFactoid,
			FaithfulnessLabel:  FaithfulnessFullySupported,
			ExpectedAnswer:     "The answer is documented in the source.",
			RequiredClaims:     []string{"the answer is documented"},
			CandidateDocuments: []CandidateDocument{{DocumentID: "doc-1", SourceGrade: 2}},
			Relevant:           relevant,
			Judgments: []RelevanceJudgment{
				{AnnotatorID: "expert-a", Relevant: relevant},
				{AnnotatorID: "expert-b", Relevant: relevant},
			},
			Adjudication: &AdjudicationMetadata{
				Method:      "independent labels reviewed and adjudicated",
				ReviewerIDs: []string{"expert-a", "expert-b"},
			},
		}
	}
	return golden
}
