// Package documents is the document-store Adapter over Cloudflare Vectorize and
// D1. Vectorize holds only the dense vectors; D1 holds the version manifest, the
// chunk text and an FTS5 index that serves the keyword (BM25) leg. Search runs
// both legs and fuses them with reciprocal-rank fusion here, because neither
// service fuses for us.
//
// Visibility is decided by D1 alone: a chunk is searchable when its version row
// is active, and publishing a version is one atomic D1 batch. Vectorize writes
// are applied asynchronously, so a publication waits (best effort) for the
// vectors to become queryable before it makes the version active.
package documents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	indexing "nadir/internal/core/documents/indexing"
	"nadir/internal/providers/cloudflare/d1"
	"nadir/internal/providers/cloudflare/vectorize"
)

const (
	defaultPrefetchMul = 5
	// maxQueryTopK is Vectorize's cap for a query that returns ids only.
	maxQueryTopK = 100
	// rrfK is the reciprocal-rank-fusion constant, matching Qdrant's native RRF.
	rrfK = 2
	// Batch sizes keep every request under D1's 100-bound-parameter statement
	// limit and Vectorize's request limits.
	chunksPerBatch  = 10
	vectorsPerBatch = 200
	idsPerStatement = 90
	cleanupPage     = 1000
	shaPageSize     = 1000
	mutationWait    = 20 * time.Second
)

// Metadata properties used to pre-filter Vectorize queries. Values are short
// hashes because a string metadata index only covers the first 64 bytes.
const (
	metaFile   = "file_key"
	metaHeader = "header_key"
	metaSHA    = "sha_key"
)

// Database is the D1 capability this Adapter needs.
type Database interface {
	Exec(ctx context.Context, statements ...d1.Statement) ([][]d1.Row, error)
}

// Index is the Vectorize capability this Adapter needs.
type Index interface {
	Describe(ctx context.Context) (vectorize.IndexConfig, error)
	MetadataIndexes(ctx context.Context) ([]vectorize.MetadataIndex, error)
	QueryWith(ctx context.Context, vector []float32, opts vectorize.QueryOptions) ([]vectorize.Match, error)
	UpsertMany(ctx context.Context, vectors []vectorize.Vector) (string, error)
	DeleteByIDs(ctx context.Context, ids []string) error
	WaitForMutation(ctx context.Context, mutationID string, timeout time.Duration) (bool, error)
}

var (
	_ Database = (*d1.Client)(nil)
	_ Index    = (*vectorize.Client)(nil)
)

// DependenciesConfig groups the D1 database, the Vectorize index and the search
// shape.
type DependenciesConfig struct {
	DB    Database
	Index Index
	// IndexName is only used to make setup errors actionable.
	IndexName       string
	PrefetchMul     int
	AdaptiveSignals bool
}

type dependencies struct {
	db              Database
	index           Index
	indexName       string
	prefetchMul     int
	adaptiveSignals bool
	ready           bool
	// mu separates normal operations (read lock) from reset (write lock);
	// publicationMu serializes version publication within this process.
	mu            sync.RWMutex
	publicationMu sync.Mutex
}

// NewDependencies constructs the Cloudflare document-store Adapter.
func NewDependencies(cfg DependenciesConfig) (*dependencies, error) {
	if cfg.DB == nil || cfg.Index == nil {
		return nil, fmt.Errorf("document store: d1 database and vectorize index are required")
	}
	mul := cfg.PrefetchMul
	if mul <= 0 {
		mul = defaultPrefetchMul
	}
	return &dependencies{db: cfg.DB, index: cfg.Index, indexName: cfg.IndexName, prefetchMul: mul, adaptiveSignals: cfg.AdaptiveSignals}, nil
}

// EnsureStore creates the D1 tables and verifies the existing Vectorize index
// has the embedding dimensions, the cosine metric and the three string metadata
// indexes the filters need. Metadata indexes must exist before vectors are
// inserted, so a missing one is an error, not something to create silently.
func (s *dependencies) EnsureStore(ctx context.Context, dimensions int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cfg, err := s.index.Describe(ctx)
	if err != nil {
		return fmt.Errorf("document index: %w", err)
	}
	if cfg.Dimensions != dimensions {
		return fmt.Errorf("document index has %d dimensions, want %d (embedder.dimensions); create an index with --dimensions=%d", cfg.Dimensions, dimensions, dimensions)
	}
	if !strings.EqualFold(cfg.Metric, "cosine") {
		return fmt.Errorf("document index metric is %q, want cosine", cfg.Metric)
	}
	have, err := s.index.MetadataIndexes(ctx)
	if err != nil {
		return fmt.Errorf("document index metadata indexes: %w", err)
	}
	present := map[string]bool{}
	for _, m := range have {
		if strings.EqualFold(m.Type, "string") {
			present[m.Property] = true
		}
	}
	for _, property := range []string{metaFile, metaHeader, metaSHA} {
		if !present[property] {
			name := s.indexName
			if name == "" {
				name = "<index>"
			}
			return fmt.Errorf("document index lacks the string metadata index %q; create it before any vectors are stored: wrangler vectorize create-metadata-index %s --property-name=%s --type=string", property, name, property)
		}
	}
	if _, err := s.db.Exec(ctx, schema...); err != nil {
		return fmt.Errorf("document store schema: %w", err)
	}
	s.ready = true
	return nil
}

func (s *dependencies) generation(ctx context.Context) (int64, error) {
	rows, err := s.db.Exec(ctx, d1.Statement{SQL: `SELECT generation FROM doc_state WHERE id = 1`})
	if err != nil {
		return 0, fmt.Errorf("read corpus generation: %w", err)
	}
	if len(rows[0]) == 0 {
		return 0, fmt.Errorf("document store is not initialized")
	}
	return rows[0][0].Int("generation"), nil
}

var chunkIDNamespace = uuid.MustParse("a3b4c5d6-e7f8-4a5b-9c0d-1e2f3a4b5c6d")

// chunkID is a stable id for a chunk of one document version in one generation.
// The generation keeps ids from colliding with vectors of a retired corpus that
// has not been cleaned up yet.
func chunkID(c indexing.IndexedChunk, generation int64) string {
	key := c.FilePath + ":" + c.SourceSHA + ":" + strconv.Itoa(c.LineStart) + ":" + strconv.Itoa(c.ChunkIndex)
	return uuid.NewSHA1(chunkIDNamespace, []byte(key)).String() + "-g" + strconv.FormatInt(generation, 10)
}

// metaKey shortens an arbitrary string to a fixed 16-character filter key.
func metaKey(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}

func (s *dependencies) notReady() error {
	if !s.ready {
		return fmt.Errorf("document store is not initialized")
	}
	return nil
}
