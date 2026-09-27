package search

import "strconv"

// Filter limits document searches to indexed document fields.
type Filter struct {
	FilePath  string
	Header    string
	SourceSHA string
}

// SearchCandidate is the Retrieval-owned candidate exchanged with the
// document store, reranker, and semantic cache boundaries.
type SearchCandidate struct {
	Text       string
	WindowText string
	FilePath   string
	Header     string
	LineStart  int
	ChunkIndex int
	SourceSHA  string
	Score      float32
}

// HybridSearchResult contains the fused ranking and the two component legs
// used to decide whether cross-encoder reranking is necessary. The leg
// rankings are retrieval signals, not caller-facing scores.
type HybridSearchResult struct {
	Fused   []SearchCandidate
	Dense   []SearchCandidate
	Lexical []SearchCandidate
}

// QueryType is an optional retrieval hint. Production callers may leave it
// empty and let Search classify the query; evaluation callers should pass the
// golden-set annotation so calibration is measured against a stable label.
type QueryType string

const (
	QueryTypeUnknown    QueryType = ""
	QueryTypeFactoid    QueryType = "factoid"
	QueryTypeFormula    QueryType = "formula"
	QueryTypeProcedure  QueryType = "procedure"
	QueryTypeComparison QueryType = "comparison"
	QueryTypeMultiHop   QueryType = "multi_hop"
)

// FusionProfile contains deterministic rank-fusion weights and small lexical
// boosts. RRF is rank-based, so dense and BM25 score scales are never mixed.
// The minimum overlap fields are query-type thresholds: a boost is applied
// only when enough query terms are present in the candidate/header.
type FusionProfile struct {
	DenseWeight      float32
	BM25Weight       float32
	ExactMatchBoost  float32
	HeaderMatchBoost float32
	MinExactTokens   int
	MinHeaderTokens  int
}

// FusionConfig enables the opt-in calibrated fusion path. When disabled,
// Retrieval preserves the provider-native hybrid result.
type FusionConfig struct {
	Enabled          bool
	RRFK             int
	DenseWeight      float32
	BM25Weight       float32
	ExactMatchBoost  float32
	HeaderMatchBoost float32
	MinExactTokens   int
	MinHeaderTokens  int
	Profiles         map[QueryType]FusionProfile
}

// RerankTelemetry describes the decision and cost of the optional reranker
// for one Retrieval request. It is intentionally provider-neutral so the
// evaluator can measure coverage and dependency load without coupling to an
// adapter implementation.
type RerankTelemetry struct {
	Enabled       bool    `json:"enabled"`
	Attempted     bool    `json:"attempted"`
	Reason        string  `json:"reason"`
	Candidates    int     `json:"candidates"`
	LatencyMS     float64 `json:"latency_ms"`
	DependencyErr bool    `json:"dependency_error"`
}

// Key identifies a logical source chunk across dense, lexical, and HyPE
// results. HyPE siblings intentionally collapse onto their parent chunk.
func (c SearchCandidate) Key() string {
	return c.FilePath + ":" + strconv.Itoa(c.LineStart)
}

// Chunk is the stable Retrieval result shape used by chat and the HTTP
// transport. Storage-specific vectors and sparse-index fields stay behind the
// store Adapter.
type Chunk struct {
	Text       string
	WindowText string
	FilePath   string
	Header     string
	LineStart  int
	ChunkIndex int
	SourceSHA  string
	Score      float32
}

// Request is one bounded retrieval request.
type Request struct {
	Query     string
	Keyword   string
	TopK      int
	Filter    *Filter
	SkipCache bool
	QueryType QueryType
}

// Result is the provider-neutral retrieval response.
type Result struct {
	Chunks    []Chunk
	FromCache bool
	Rerank    RerankTelemetry
	// OperationID correlates this Retrieval result with structured domain
	// telemetry. It is intentionally opaque to callers.
	OperationID string
}
