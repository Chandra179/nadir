package evaluation

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"nadir/internal/retrieval/search"
)

const currentGoldenSchemaVersion = 3

// RelevantChunk identifies one golden-relevant Retrieval result. File is a
// case-insensitive path-suffix match, with a base-name fallback so the same
// golden set works for host paths and container mounts; Contains is a
// case-insensitive substring match over the chunk's section header, text, and
// contextual window.
type RelevantChunk struct {
	File     string `json:"file"`
	Contains string `json:"contains"`
	Grade    int    `json:"grade,omitempty"`
}

// CandidateDocument preserves the fixed source-document pool presented to
// reviewers. It is audit metadata; retrieval matching still uses the
// canonical Relevant and Distractors fields.
type CandidateDocument struct {
	DocumentID  string `json:"document_id"`
	SourceGrade int    `json:"source_grade"`
}

// QueryType describes the evidence shape of a golden query.
type QueryType string

const (
	QueryTypeFactoid    QueryType = "factoid"
	QueryTypeFormula    QueryType = "formula"
	QueryTypeProcedure  QueryType = "procedure"
	QueryTypeComparison QueryType = "comparison"
	QueryTypeMultiHop   QueryType = "multi_hop"
)

// FaithfulnessLabel records the expected support relationship between an
// answer and the indexed evidence. It is an annotation for the generation
// evaluator; retrieval scoring still uses Relevant and Distractors.
type FaithfulnessLabel string

const (
	FaithfulnessFullySupported FaithfulnessLabel = "fully_supported"
	FaithfulnessPartially      FaithfulnessLabel = "partially_supported"
	FaithfulnessUnsupported    FaithfulnessLabel = "unsupported"
)

// GoldenQuery defines one evaluation query, its evidence, and its answer
// annotation. ExpectedAnswer and RequiredClaims make the set useful for a
// later generation-faithfulness evaluator without coupling this package to a
// judge model.
type GoldenQuery struct {
	ID                 string              `json:"id"`
	Query              string              `json:"query"`
	Type               QueryType           `json:"type"`
	FaithfulnessLabel  FaithfulnessLabel   `json:"faithfulness_label"`
	ExpectedAnswer     string              `json:"expected_answer"`
	RequiredClaims     []string            `json:"required_claims"`
	Tags               []string            `json:"tags,omitempty"`
	SourceQueryID      string              `json:"source_query_id,omitempty"`
	SourceEdition      string              `json:"source_edition,omitempty"`
	SourceYear         string              `json:"source_year,omitempty"`
	CandidateDocuments []CandidateDocument `json:"candidate_documents,omitempty"`
	// Relevant and Distractors are the adjudicated canonical labels used by
	// the evaluator. Judgments retains the separate annotation passes that
	// produced those canonical labels.
	Relevant     []RelevantChunk       `json:"relevant"`
	Distractors  []RelevantChunk       `json:"distractors,omitempty"`
	Judgments    []RelevanceJudgment   `json:"judgments,omitempty"`
	Adjudication *AdjudicationMetadata `json:"adjudication,omitempty"`
}

// RelevanceJudgment records one annotator's relevance and distractor labels
// for a query. It is intentionally separate from the canonical labels on
// GoldenQuery so release-gate validation can verify independent annotation.
type RelevanceJudgment struct {
	AnnotatorID string          `json:"annotator_id"`
	Relevant    []RelevantChunk `json:"relevant"`
	Distractors []RelevantChunk `json:"distractors,omitempty"`
}

// AdjudicationMetadata identifies how the canonical labels were resolved from
// the recorded judgments.
type AdjudicationMetadata struct {
	Method      string   `json:"method"`
	ReviewerIDs []string `json:"reviewer_ids"`
}

// SourceMetadata identifies the public dataset source and the exact artifacts
// used to build an evaluation pack. ArtifactSHA256 is keyed by a stable
// artifact name such as "topics-arqmath-3.xml" or "qrels-arqmath-3.tsv".
type SourceMetadata struct {
	Name              string            `json:"name"`
	Homepage          string            `json:"homepage"`
	License           string            `json:"license"`
	Usage             string            `json:"usage"`
	Snapshot          string            `json:"snapshot"`
	Attribution       string            `json:"attribution"`
	LicenseNoticePath string            `json:"license_notice_path,omitempty"`
	ArtifactSHA256    map[string]string `json:"artifact_sha256"`
}

// CorpusMetadata identifies the exact corpus used by a golden set.
type CorpusMetadata struct {
	ID             string   `json:"id"`
	Documents      []string `json:"documents"`
	DocumentCount  int      `json:"document_count"`
	Representative bool     `json:"representative"`
	ManifestPath   string   `json:"manifest_path"`
	ManifestSHA256 string   `json:"manifest_sha256"`
}

// PrivacyReviewMetadata records the review status for corpus and query
// provenance. A release gate requires an approved review with an accountable
// reviewer and timestamp.
type PrivacyReviewMetadata struct {
	Status         string `json:"status"`
	Reviewer       string `json:"reviewer"`
	ReviewedAt     string `json:"reviewed_at"`
	EvidencePath   string `json:"evidence_path"`
	EvidenceSHA256 string `json:"evidence_sha256"`
}

// AnnotatorMetadata identifies a relevance annotator. Human and Independent
// are explicit because a label such as "two experts" in a free-form string is
// not sufficient evidence for a release decision.
type AnnotatorMetadata struct {
	ID              string `json:"id"`
	Role            string `json:"role"`
	Human           bool   `json:"human"`
	Independent     bool   `json:"independent"`
	VerificationRef string `json:"verification_ref"`
}

// GoldenSetMetadata records how a fixture was obtained. Keeping provenance in
// the fixture prevents synthetic expert-authored cases from being mistaken for
// consented production telemetry.
type GoldenSetMetadata struct {
	Dataset                string                 `json:"dataset,omitempty"`
	Provenance             string                 `json:"provenance,omitempty"`
	Consent                string                 `json:"consent,omitempty"`
	Judgment               string                 `json:"judgment,omitempty"`
	ReleaseGate            bool                   `json:"release_gate,omitempty"`
	JudgmentArtifactPath   string                 `json:"judgment_artifact_path,omitempty"`
	JudgmentArtifactSHA256 string                 `json:"judgment_artifact_sha256,omitempty"`
	Source                 *SourceMetadata        `json:"source,omitempty"`
	Corpus                 *CorpusMetadata        `json:"corpus,omitempty"`
	PrivacyReview          *PrivacyReviewMetadata `json:"privacy_review,omitempty"`
	Annotators             []AnnotatorMetadata    `json:"annotators,omitempty"`
}

// GoldenSet is the complete collection of evaluation queries.
type GoldenSet struct {
	SchemaVersion int               `json:"schema_version"`
	Metadata      GoldenSetMetadata `json:"metadata"`
	Queries       []GoldenQuery     `json:"queries"`
}

// ValidateReleaseGate verifies the additional provenance contract required
// before a golden set can drive a production release decision. The ordinary
// loader intentionally accepts synthetic development fixtures; this stricter
// check is opt-in from the evaluator command because production evidence
// requires data and consent that cannot be generated by the repository.
func (gs *GoldenSet) ValidateReleaseGate() error {
	if gs == nil {
		return fmt.Errorf("golden set is required")
	}
	if gs.SchemaVersion < 3 {
		return fmt.Errorf("release gate requires golden set schema version 3")
	}
	if len(gs.Queries) < 100 {
		return fmt.Errorf("release gate requires at least 100 annotated queries, got %d", len(gs.Queries))
	}
	if !gs.Metadata.ReleaseGate {
		return fmt.Errorf("golden set metadata.release_gate is false")
	}
	if strings.TrimSpace(gs.Metadata.Dataset) == "" || strings.Contains(strings.ToLower(gs.Metadata.Dataset), "synthetic") {
		return fmt.Errorf("release gate requires a consented production dataset")
	}
	if strings.TrimSpace(gs.Metadata.Provenance) == "" {
		return fmt.Errorf("release gate requires provenance metadata")
	}
	consent := strings.ToLower(strings.TrimSpace(gs.Metadata.Consent))
	if consent == "" || consent == "not-applicable-no-production-user-data" || strings.Contains(consent, "without consent") {
		return fmt.Errorf("release gate requires consent metadata for production queries")
	}
	if strings.TrimSpace(gs.Metadata.Judgment) == "" {
		return fmt.Errorf("release gate requires expert judgment metadata")
	}
	if err := validateReleaseSource(gs.Metadata.Source); err != nil {
		return err
	}
	if err := validateEvidenceArtifact("judgment artifact", gs.Metadata.JudgmentArtifactPath, gs.Metadata.JudgmentArtifactSHA256); err != nil {
		return err
	}
	if err := validateReleaseCorpus(gs.Metadata.Corpus); err != nil {
		return err
	}
	if err := validateReleasePrivacyReview(gs.Metadata.PrivacyReview); err != nil {
		return err
	}
	annotators, err := validateReleaseAnnotators(gs.Metadata.Annotators)
	if err != nil {
		return err
	}
	for _, query := range gs.Queries {
		if strings.TrimSpace(query.ExpectedAnswer) == "" || len(query.RequiredClaims) == 0 || query.FaithfulnessLabel == "" {
			return fmt.Errorf("release gate query %q is missing generation annotations", query.ID)
		}
		if err := validateCandidateDocuments(query.ID, query.CandidateDocuments, true); err != nil {
			return err
		}
		if err := validateQueryJudgments(query, annotators, true); err != nil {
			return err
		}
	}
	return nil
}

func validateReleaseCorpus(corpus *CorpusMetadata) error {
	if corpus == nil {
		return fmt.Errorf("release gate requires corpus metadata")
	}
	if strings.TrimSpace(corpus.ID) == "" {
		return fmt.Errorf("release gate requires corpus.id")
	}
	if !corpus.Representative {
		return fmt.Errorf("release gate requires a representative corpus")
	}
	if corpus.DocumentCount <= 0 {
		return fmt.Errorf("release gate requires a positive corpus document count")
	}
	if len(corpus.Documents) > 0 && len(corpus.Documents) != corpus.DocumentCount {
		return fmt.Errorf("release gate requires a complete corpus document manifest")
	}
	if strings.TrimSpace(corpus.ManifestPath) == "" {
		return fmt.Errorf("release gate requires corpus.manifest_path")
	}
	seen := make(map[string]struct{}, len(corpus.Documents))
	for _, document := range corpus.Documents {
		document = strings.TrimSpace(document)
		if document == "" {
			return fmt.Errorf("release gate corpus manifest contains an empty document")
		}
		if _, exists := seen[document]; exists {
			return fmt.Errorf("release gate corpus manifest contains duplicate document %q", document)
		}
		seen[document] = struct{}{}
	}
	manifest := strings.TrimSpace(corpus.ManifestSHA256)
	if len(manifest) != 64 {
		return fmt.Errorf("release gate requires corpus.manifest_sha256")
	}
	if _, err := hex.DecodeString(manifest); err != nil {
		return fmt.Errorf("release gate corpus.manifest_sha256 must be hexadecimal: %w", err)
	}
	return nil
}

func validateReleasePrivacyReview(review *PrivacyReviewMetadata) error {
	if review == nil {
		return fmt.Errorf("release gate requires privacy_review metadata")
	}
	if !strings.EqualFold(strings.TrimSpace(review.Status), "approved") {
		return fmt.Errorf("release gate requires approved privacy review")
	}
	if strings.TrimSpace(review.Reviewer) == "" {
		return fmt.Errorf("release gate requires privacy_review.reviewer")
	}
	if err := validateEvidenceArtifact("privacy review", review.EvidencePath, review.EvidenceSHA256); err != nil {
		return err
	}
	if timestamp := strings.TrimSpace(review.ReviewedAt); timestamp == "" {
		return fmt.Errorf("release gate requires privacy_review.reviewed_at")
	} else if _, err := time.Parse(time.RFC3339, timestamp); err != nil {
		return fmt.Errorf("release gate privacy_review.reviewed_at must be RFC3339: %w", err)
	}
	return nil
}

func validateReleaseAnnotators(annotators []AnnotatorMetadata) (map[string]AnnotatorMetadata, error) {
	if len(annotators) < 2 {
		return nil, fmt.Errorf("release gate requires at least two annotators")
	}
	byID := make(map[string]AnnotatorMetadata, len(annotators))
	for _, annotator := range annotators {
		id := strings.TrimSpace(annotator.ID)
		if id == "" || strings.TrimSpace(annotator.Role) == "" {
			return nil, fmt.Errorf("release gate annotators require id and role")
		}
		if _, exists := byID[id]; exists {
			return nil, fmt.Errorf("release gate annotators contain duplicate id %q", id)
		}
		if !annotator.Human || !annotator.Independent {
			return nil, fmt.Errorf("release gate annotator %q must be an independent human", id)
		}
		if strings.TrimSpace(annotator.VerificationRef) == "" {
			return nil, fmt.Errorf("release gate annotator %q requires verification_ref", id)
		}
		byID[id] = annotator
	}
	return byID, nil
}

func validateQueryJudgments(query GoldenQuery, annotators map[string]AnnotatorMetadata, releaseGate bool) error {
	if len(query.Judgments) < 2 {
		return fmt.Errorf("query %q requires at least two relevance judgments", query.ID)
	}
	seen := make(map[string]struct{}, len(query.Judgments))
	for _, judgment := range query.Judgments {
		annotatorID := strings.TrimSpace(judgment.AnnotatorID)
		if annotatorID == "" {
			return fmt.Errorf("query %q has a judgment without annotator_id", query.ID)
		}
		if _, exists := seen[annotatorID]; exists {
			return fmt.Errorf("query %q has duplicate judgment annotator %q", query.ID, annotatorID)
		}
		seen[annotatorID] = struct{}{}
		if releaseGate {
			annotator, exists := annotators[annotatorID]
			if !exists || !annotator.Human || !annotator.Independent {
				return fmt.Errorf("query %q judgment %q is not from a verified independent human annotator", query.ID, annotatorID)
			}
		}
		if len(judgment.Relevant) == 0 {
			return fmt.Errorf("query %q judgment %q has no relevant labels", query.ID, annotatorID)
		}
		for index, relevant := range judgment.Relevant {
			if err := validateMatch(query.ID, "judgment relevant", index, relevant); err != nil {
				return err
			}
		}
		for index, distractor := range judgment.Distractors {
			if err := validateMatch(query.ID, "judgment distractor", index, distractor); err != nil {
				return err
			}
		}
	}
	if query.Adjudication == nil || strings.TrimSpace(query.Adjudication.Method) == "" {
		return fmt.Errorf("query %q requires adjudication metadata", query.ID)
	}
	if len(query.Adjudication.ReviewerIDs) < 2 {
		return fmt.Errorf("query %q adjudication requires at least two reviewers", query.ID)
	}
	seenReviewers := make(map[string]struct{}, len(query.Adjudication.ReviewerIDs))
	for _, reviewerID := range query.Adjudication.ReviewerIDs {
		reviewerID = strings.TrimSpace(reviewerID)
		if _, exists := seen[reviewerID]; !exists {
			return fmt.Errorf("query %q adjudication reviewer %q has no recorded judgment", query.ID, reviewerID)
		}
		if _, exists := seenReviewers[reviewerID]; exists {
			return fmt.Errorf("query %q adjudication has duplicate reviewer %q", query.ID, reviewerID)
		}
		if releaseGate {
			annotator, exists := annotators[reviewerID]
			if !exists || !annotator.Human || !annotator.Independent {
				return fmt.Errorf("query %q adjudication reviewer %q is not a verified independent human annotator", query.ID, reviewerID)
			}
		}
		seenReviewers[reviewerID] = struct{}{}
	}
	if releaseGate {
		for annotatorID := range annotators {
			if _, exists := seen[annotatorID]; !exists {
				return fmt.Errorf("query %q is missing judgment from annotator %q", query.ID, annotatorID)
			}
		}
	}
	return nil
}

func validateCandidateDocuments(queryID string, candidates []CandidateDocument, required bool) error {
	if required && len(candidates) == 0 {
		return fmt.Errorf("release gate query %q requires a fixed candidate document pool", queryID)
	}
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		documentID := strings.TrimSpace(candidate.DocumentID)
		if documentID == "" {
			return fmt.Errorf("query %q candidate pool contains an empty document id", queryID)
		}
		if _, exists := seen[documentID]; exists {
			return fmt.Errorf("query %q candidate pool contains duplicate document %q", queryID, documentID)
		}
		seen[documentID] = struct{}{}
	}
	return nil
}

func validateReleaseSource(source *SourceMetadata) error {
	if source == nil {
		return fmt.Errorf("release gate requires source metadata")
	}
	if !strings.Contains(strings.ToLower(strings.TrimSpace(source.Name)), "arqmath") {
		return fmt.Errorf("release gate requires ARQMath source metadata")
	}
	for label, value := range map[string]string{
		"homepage":            source.Homepage,
		"license":             source.License,
		"usage":               source.Usage,
		"snapshot":            source.Snapshot,
		"attribution":         source.Attribution,
		"license_notice_path": source.LicenseNoticePath,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("release gate source.%s is required", label)
		}
	}
	if len(source.ArtifactSHA256) == 0 {
		return fmt.Errorf("release gate requires source artifact hashes")
	}
	if _, exists := source.ArtifactSHA256["Posts.V1.3.zip"]; !exists {
		return fmt.Errorf("release gate requires the verified ARQMath Posts.V1.3.zip artifact hash")
	}
	for artifact, hash := range source.ArtifactSHA256 {
		if strings.TrimSpace(artifact) == "" {
			return fmt.Errorf("release gate source artifact hash has an empty name")
		}
		if err := validateSHA256("source artifact "+artifact, hash); err != nil {
			return err
		}
	}
	return nil
}

func validateEvidenceArtifact(label, path, hash string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("release gate requires %s evidence_path", label)
	}
	return validateSHA256(label, hash)
}

func validateSHA256(label, value string) error {
	value = strings.TrimSpace(value)
	if len(value) != 64 {
		return fmt.Errorf("release gate requires %s SHA-256", label)
	}
	if _, err := hex.DecodeString(value); err != nil {
		return fmt.Errorf("release gate %s SHA-256 must be hexadecimal: %w", label, err)
	}
	return nil
}

// LoadGoldenSet reads and validates a golden query set from JSON.
func LoadGoldenSet(path string) (*GoldenSet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read golden set: %w", err)
	}
	var gs GoldenSet
	if err := json.Unmarshal(data, &gs); err != nil {
		return nil, fmt.Errorf("parse golden set %s: %w", path, err)
	}
	if len(gs.Queries) == 0 {
		return nil, fmt.Errorf("golden set %s has no queries", path)
	}
	if gs.SchemaVersion > currentGoldenSchemaVersion {
		return nil, fmt.Errorf("golden set %s uses unsupported schema version %d", path, gs.SchemaVersion)
	}
	seenIDs := make(map[string]struct{}, len(gs.Queries))
	for i, q := range gs.Queries {
		if strings.TrimSpace(q.ID) == "" {
			return nil, fmt.Errorf("golden set query #%d has no id", i+1)
		}
		if _, exists := seenIDs[q.ID]; exists {
			return nil, fmt.Errorf("golden set query #%d (%q) duplicates an id", i+1, q.ID)
		}
		seenIDs[q.ID] = struct{}{}
		if strings.TrimSpace(q.Query) == "" || len(q.Relevant) == 0 {
			return nil, fmt.Errorf("golden set query #%d (%q) needs a query and at least one relevant entry", i+1, q.ID)
		}
		if gs.SchemaVersion >= 2 {
			if !validQueryType(q.Type) {
				return nil, fmt.Errorf("golden set query %q has invalid type %q", q.ID, q.Type)
			}
			if !validFaithfulnessLabel(q.FaithfulnessLabel) {
				return nil, fmt.Errorf("golden set query %q has invalid faithfulness label %q", q.ID, q.FaithfulnessLabel)
			}
			if strings.TrimSpace(q.ExpectedAnswer) == "" || len(q.RequiredClaims) == 0 {
				return nil, fmt.Errorf("golden set query %q needs an expected answer and at least one required claim", q.ID)
			}
			for claimIndex, claim := range q.RequiredClaims {
				if strings.TrimSpace(claim) == "" {
					return nil, fmt.Errorf("golden set query %q required claim #%d is empty", q.ID, claimIndex+1)
				}
			}
		}
		if gs.SchemaVersion >= 3 {
			if err := validateQueryJudgments(q, nil, false); err != nil {
				return nil, err
			}
		}
		if err := validateCandidateDocuments(q.ID, q.CandidateDocuments, false); err != nil {
			return nil, err
		}
		for j, relevant := range q.Relevant {
			if err := validateMatch(q.ID, "relevant", j, relevant); err != nil {
				return nil, err
			}
		}
		for j, distractor := range q.Distractors {
			if err := validateMatch(q.ID, "distractor", j, distractor); err != nil {
				return nil, err
			}
		}
	}
	return &gs, nil
}

func validateMatch(queryID, kind string, index int, match RelevantChunk) error {
	if strings.TrimSpace(match.File) == "" && strings.TrimSpace(match.Contains) == "" {
		return fmt.Errorf("golden set query %q %s entry #%d needs file or contains", queryID, kind, index+1)
	}
	return nil
}

func validQueryType(value QueryType) bool {
	switch value {
	case QueryTypeFactoid, QueryTypeFormula, QueryTypeProcedure, QueryTypeComparison, QueryTypeMultiHop:
		return true
	default:
		return false
	}
}

func validFaithfulnessLabel(value FaithfulnessLabel) bool {
	switch value {
	case FaithfulnessFullySupported, FaithfulnessPartially, FaithfulnessUnsupported:
		return true
	default:
		return false
	}
}

// MatchedRelevant returns the indices of golden entries matched by chunk.
func MatchedRelevant(chunk search.Chunk, relevant []RelevantChunk) []int {
	return matchedEntries(chunk, relevant)
}

// MatchedDistractors returns the indices of distractor annotations matched by
// a chunk. It uses the same host/container path matching rules as relevance.
func MatchedDistractors(chunk search.Chunk, distractors []RelevantChunk) []int {
	return matchedEntries(chunk, distractors)
}

func matchedEntries(chunk search.Chunk, entries []RelevantChunk) []int {
	haystack := strings.ToLower(chunk.Header + "\n" + chunk.Text + "\n" + chunk.WindowText)
	filePath := strings.ToLower(chunk.FilePath)
	matched := make([]int, 0, len(entries))
	for i, want := range entries {
		if want.File != "" && !matchesFile(filePath, want.File) {
			continue
		}
		if want.Contains != "" && !strings.Contains(haystack, strings.ToLower(want.Contains)) {
			continue
		}
		matched = append(matched, i)
	}
	return matched
}

func matchesFile(actual, expected string) bool {
	actual = strings.ToLower(filepath.ToSlash(actual))
	expected = strings.ToLower(filepath.ToSlash(expected))
	return strings.HasSuffix(actual, expected) || filepath.Base(actual) == filepath.Base(expected)
}

// QueryResult records retrieval quality and latency for one golden query.
type QueryResult struct {
	ID                string                 `json:"id"`
	Query             string                 `json:"query"`
	Type              QueryType              `json:"type,omitempty"`
	FaithfulnessLabel FaithfulnessLabel      `json:"faithfulness_label,omitempty"`
	NumRelevant       int                    `json:"num_relevant"`
	Hits              []bool                 `json:"hits"`
	FirstHitRank      int                    `json:"first_hit_rank"`
	RelevantFound     int                    `json:"relevant_found"`
	DistractorHits    int                    `json:"distractor_hits,omitempty"`
	LatencyMS         float64                `json:"latency_ms"`
	Latencies         []float64              `json:"latencies"`
	Rerank            search.RerankTelemetry `json:"rerank"`
}

// Aggregate contains quality and latency metrics across a golden set.
type Aggregate struct {
	Queries    int     `json:"queries"`
	TopK       int     `json:"top_k"`
	HitRateAtK float64 `json:"hit_rate_at_k"`
	RecallAtK  float64 `json:"recall_at_k"`
	MRRAt10    float64 `json:"mrr_at_10"`
	NDCGAtK    float64 `json:"ndcg_at_k"`
	// DistractorHitRateAtK is the fraction of queries with at least one
	// annotated distractor in the top-k results.
	DistractorHitRateAtK  float64        `json:"distractor_hit_rate_at_k,omitempty"`
	P50LatMS              float64        `json:"p50_latency_ms"`
	P95LatMS              float64        `json:"p95_latency_ms"`
	RerankCoverage        float64        `json:"rerank_coverage"`
	RerankDependencyCalls int            `json:"rerank_dependency_calls"`
	RerankCandidateTotal  int            `json:"rerank_candidate_total"`
	RerankP50LatMS        float64        `json:"rerank_p50_latency_ms"`
	RerankP95LatMS        float64        `json:"rerank_p95_latency_ms"`
	RerankErrors          int            `json:"rerank_dependency_errors"`
	RerankReasons         map[string]int `json:"rerank_reasons,omitempty"`
}

// Report is the persisted evaluation result for one evaluator run.
type Report struct {
	Timestamp            string            `json:"timestamp"`
	Dataset              string            `json:"dataset,omitempty"`
	DatasetSchemaVersion int               `json:"dataset_schema_version,omitempty"`
	DatasetReleaseGate   bool              `json:"dataset_release_gate"`
	TopK                 int               `json:"top_k"`
	Rerank               bool              `json:"reranker_enabled"`
	AdaptiveRerank       bool              `json:"adaptive_reranker_enabled"`
	PerQuery             []QueryResult     `json:"per_query"`
	Aggregate            Aggregate         `json:"aggregate"`
	Generation           *GenerationReport `json:"generation,omitempty"`
}
