package evaluation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"
	"nadir/internal/core/retrieval/search"
	"nadir/internal/testmocks"
)

func TestHarnessRetainsCompletedAndPartialQueriesOnFailure(t *testing.T) {
	searcher := &mocks.MockRetriever{}
	searcher.EXPECT().Query(mock.Anything, mock.Anything).
		Return(search.Result{Chunks: []search.Chunk{{FilePath: "doc.md", Text: "evidence"}}}, nil).Times(3)
	want := errors.New("retrieval offline")
	searcher.EXPECT().Query(mock.Anything, mock.Anything).Return(search.Result{}, want).Once()
	report, err := NewDependencies(DependenciesConfig{Searcher: searcher}).Run(context.Background(), &GoldenSet{
		Queries: []GoldenQuery{
			{ID: "complete", Query: "one", Relevant: []RelevantChunk{{File: "doc.md"}}},
			{ID: "partial", Query: "two", Relevant: []RelevantChunk{{File: "doc.md"}}},
		},
	}, 5, 2)
	if !errors.Is(err, want) || report == nil || len(report.PerQuery) != 2 {
		t.Fatalf("report=%+v, err=%v", report, err)
	}
	if len(report.PerQuery[0].Runs) != 2 || report.PerQuery[0].RepresentativeRun == 0 ||
		len(report.PerQuery[1].Runs) != 1 || report.PerQuery[1].RepresentativeRun != 0 {
		t.Fatalf("partial rankings not retained: %+v", report.PerQuery)
	}
	if report.Aggregate.Queries != 0 {
		t.Fatal("failed dataset presented as a complete aggregate")
	}
}

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

func TestHistoricalGoldenFixtureIsAnnotated(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(filepath.Dir(sourceFile), "../../test/evaluation/golden.json")
	golden, err := LoadGoldenSet(path)
	if err != nil {
		t.Fatalf("LoadGoldenSet(historical fixture): %v", err)
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

type sequenceRetriever struct {
	results  [][]search.Chunk
	requests []search.Request
}

func (s *sequenceRetriever) Query(_ context.Context, request search.Request) (search.Result, error) {
	s.requests = append(s.requests, request)
	return search.Result{Chunks: s.results[len(s.requests)-1]}, nil
}

func TestHarnessRetainsAllRunsAndUsesMedianQuality(t *testing.T) {
	retriever := &sequenceRetriever{results: [][]search.Chunk{
		{{FilePath: "doc.md", Text: "answer"}}, {{FilePath: "doc.md", Text: "answer"}}, {{FilePath: "other.md", Text: "wrong"}},
	}}
	report, err := NewDependencies(DependenciesConfig{Searcher: retriever}).Run(context.Background(), &GoldenSet{Queries: []GoldenQuery{{ID: "q", Query: "q", Relevant: []RelevantChunk{{File: "doc.md"}}}}}, 5, 3)
	if err != nil {
		t.Fatal(err)
	}
	if report.Aggregate.HitRateAtK != 1 || report.PerQuery[0].Metrics.HitRateAtK != 1 {
		t.Fatalf("quality used final miss: %+v", report.Aggregate)
	}
	if len(report.PerQuery[0].Runs) != 3 || len(report.RunAggregates) != 3 || report.RunAggregates[2].Aggregate.HitRateAtK != 0 {
		t.Fatalf("lost observations: %+v", report)
	}
	if report.Aggregate.Requests != 3 || report.RetrievalDepth != 10 {
		t.Fatalf("request provenance: %+v", report.Aggregate)
	}
	if got := report.Distributions["hit_rate_at_k"].Samples; len(got) != 3 || got[0] != 1 || got[2] != 0 {
		t.Fatalf("distribution=%v", got)
	}
	for _, r := range retriever.requests {
		if r.TopK != 10 {
			t.Fatalf("request depth=%d, want 10", r.TopK)
		}
	}
}

func TestMRRAt10IsIndependentOfTopK(t *testing.T) {
	chunks := make([]search.Chunk, 12)
	for i := range chunks {
		chunks[i] = search.Chunk{FilePath: "irrelevant.md"}
	}
	chunks[6].FilePath = "doc.md"
	result := scoreRun(chunks, canonicalEvidence([]RelevantChunk{{File: "doc.md"}}), nil, 5)
	if result.Metrics.HitRateAtK != 0 || result.Metrics.RecallAtK != 0 || result.Metrics.NDCGAtK != 0 || result.Metrics.MRRAt10 != 1.0/7 {
		t.Fatalf("metrics=%+v", result.Metrics)
	}
	chunks[6].FilePath = "irrelevant.md"
	chunks[10].FilePath = "doc.md"
	if got := scoreRun(chunks, canonicalEvidence([]RelevantChunk{{File: "doc.md"}}), nil, 12).Metrics.MRRAt10; got != 0 {
		t.Fatalf("rank 11 MRR@10=%v", got)
	}
}

func TestRepeatedChunkEvidenceCannotInflateNDCG(t *testing.T) {
	relevant := canonicalEvidence([]RelevantChunk{{File: "doc.md", Contains: "answer", Grade: 1}, {File: "doc.md", Contains: "answer", Grade: 3}})
	chunks := []search.Chunk{{FilePath: "doc.md", Text: "answer"}, {FilePath: "doc.md", Text: "answer"}, {FilePath: "doc.md", Text: "answer"}}
	result := scoreRun(chunks, relevant, nil, 5)
	if len(relevant) != 1 || relevant[0].Grade != 3 || result.Metrics.NDCGAtK != 1 || result.RelevantFound != 1 {
		t.Fatalf("duplicate evidence inflated metrics: %+v", result)
	}
	if result.Ranking[1].Gain != 0 || result.Ranking[2].Gain != 0 {
		t.Fatalf("duplicate gained credit: %+v", result.Ranking)
	}
}

func TestUnsupportedQueriesAreSeparateFromRetrievalHitRate(t *testing.T) {
	report, err := NewDependencies(DependenciesConfig{Searcher: &sequenceRetriever{results: [][]search.Chunk{{{FilePath: "doc.md"}}, nil}}}).Run(context.Background(), &GoldenSet{Queries: []GoldenQuery{{ID: "supported", Query: "q", Relevant: []RelevantChunk{{File: "doc.md"}}}, {ID: "unsupported", Query: "unknown", FaithfulnessLabel: FaithfulnessUnsupported}}}, 5, 1)
	if err != nil {
		t.Fatal(err)
	}
	if report.Aggregate.HitRateAtK != 1 || report.Aggregate.AnswerableQueries != 1 || report.Aggregate.AbstentionQueries != 1 || report.PerQuery[1].Metrics.HitRateAtK != 0 {
		t.Fatalf("abstention counted as hit/miss: %+v", report.Aggregate)
	}
}

func TestGoldenLoaderAcceptsOnlyExplicitUnsupportedEmptyEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "golden.json")
	good := `{"schema_version":2,"queries":[{"id":"unknown","query":"missing fact","type":"factoid","faithfulness_label":"unsupported","relevant":[],"expected_answer":"context cannot answer","required_claims":["abstain"]}]}`
	if err := os.WriteFile(path, []byte(good), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadGoldenSet(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(good, "unsupported", "fully_supported", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadGoldenSet(path); err == nil {
		t.Fatal("accepted unsupported missing evidence without explicit label")
	}
}
