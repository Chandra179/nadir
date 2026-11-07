// Package cache stores semantic-cache entries in Cloudflare Vectorize and KV.
// Vectorize holds only the query vector, found by nearest-neighbour search; the
// entry itself (results, version, timestamps) lives in KV under the same id,
// because cached result sets are far larger than Vectorize's per-vector
// metadata limit and KV expires them natively. Hit policy, TTL, versions and
// invalidation generations stay in core/retrieval/cache.
//
// Vectorize applies writes asynchronously, so a new entry can take a short time
// to become findable; that only delays hits and never returns a wrong result.
package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	semanticcache "nadir/internal/core/retrieval/cache"
	"nadir/internal/providers/cloudflare/kv"
	"nadir/internal/providers/cloudflare/vectorize"
)

// keyPrefix namespaces cache keys so a KV namespace can be shared and Clear
// lists only cache entries.
const keyPrefix = "semcache/"

// namespace derives deterministic ids from the query text, as the Qdrant
// Adapter does, so a repeated query replaces its earlier entry.
var namespace = uuid.MustParse("b1c2d3e4-f5a6-7b8c-9d00-1f2a3b4c5d6e")

// Index is the Vectorize capability this Adapter needs.
type Index interface {
	Describe(ctx context.Context) (vectorize.IndexConfig, error)
	Query(ctx context.Context, vector []float32, topK int) ([]vectorize.Match, error)
	Upsert(ctx context.Context, id string, vector []float32) error
	DeleteByIDs(ctx context.Context, ids []string) error
}

// Store is the KV capability this Adapter needs.
type Store interface {
	Put(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Get(ctx context.Context, key string) ([]byte, bool, error)
	ListKeys(ctx context.Context, prefix, cursor string) ([]string, string, error)
	DeleteMany(ctx context.Context, keys []string) error
}

var (
	_ Index = (*vectorize.Client)(nil)
	_ Store = (*kv.Client)(nil)
)

// DependenciesConfig groups the Vectorize index, the KV namespace and the
// entry lifetime.
type DependenciesConfig struct {
	Index Index
	Store Store
	// TTL is the KV expiration of each entry; zero keeps entries until Clear.
	// KV raises anything under 60 seconds to 60 seconds.
	TTL time.Duration
}

type dependencies struct {
	index Index
	store Store
	ttl   time.Duration
}

var _ interface {
	Find(ctx context.Context, vector []float32, threshold float32) (semanticcache.Entry, bool, error)
	Put(ctx context.Context, query string, vector []float32, entry semanticcache.Entry) error
	Clear(ctx context.Context) error
} = (*dependencies)(nil)

// NewDependencies constructs the Vectorize and KV semantic-cache Adapter.
func NewDependencies(cfg DependenciesConfig) (*dependencies, error) {
	if cfg.Index == nil || cfg.Store == nil {
		return nil, fmt.Errorf("semantic cache: vectorize index and kv store are required")
	}
	return &dependencies{index: cfg.Index, store: cfg.Store, ttl: cfg.TTL}, nil
}

// EnsureIndex checks that the existing Vectorize index matches the embedding
// dimensions and uses cosine similarity, which the cache threshold assumes. The
// index is not created here: provisioning an account resource is left to the
// operator.
func (d *dependencies) EnsureIndex(ctx context.Context, dimensions int) error {
	cfg, err := d.index.Describe(ctx)
	if err != nil {
		return fmt.Errorf("semantic cache index: %w", err)
	}
	if cfg.Dimensions != dimensions {
		return fmt.Errorf("semantic cache index has %d dimensions, want %d (embedder.dimensions); create an index with --dimensions=%d", cfg.Dimensions, dimensions, dimensions)
	}
	if !strings.EqualFold(cfg.Metric, "cosine") {
		return fmt.Errorf("semantic cache index metric is %q, want cosine", cfg.Metric)
	}
	return nil
}

// stored is the KV value: everything except the vector.
type stored struct {
	Version       string                    `json:"version"`
	CachedAt      time.Time                 `json:"cached_at"`
	Results       []semanticcache.Candidate `json:"results"`
	RequestedTopK int                       `json:"requested_top_k"`
}

func entryID(query string) string { return uuid.NewSHA1(namespace, []byte(query)).String() }

// Find returns the nearest cached entry at or above threshold. A nearest
// vector whose KV entry is gone (expired or cleared) is a miss and the orphaned
// vector is removed so it stops shadowing neighbours.
func (d *dependencies) Find(ctx context.Context, vector []float32, threshold float32) (semanticcache.Entry, bool, error) {
	matches, err := d.index.Query(ctx, vector, 1)
	if err != nil {
		return semanticcache.Entry{}, false, fmt.Errorf("semantic cache search: %w", err)
	}
	if len(matches) == 0 || matches[0].Score < threshold {
		return semanticcache.Entry{}, false, nil
	}
	id := matches[0].ID
	raw, found, err := d.store.Get(ctx, keyPrefix+id)
	if err != nil {
		return semanticcache.Entry{}, false, fmt.Errorf("semantic cache read: %w", err)
	}
	if !found {
		// Best effort: the miss stands whether or not the cleanup succeeds.
		_ = d.index.DeleteByIDs(ctx, []string{id})
		return semanticcache.Entry{}, false, nil
	}
	var value stored
	if err := json.Unmarshal(raw, &value); err != nil {
		return semanticcache.Entry{}, false, fmt.Errorf("semantic cache decode: %w", err)
	}
	return semanticcache.Entry{
		Version:       value.Version,
		CachedAt:      value.CachedAt,
		Results:       value.Results,
		RequestedTopK: value.RequestedTopK,
	}, true, nil
}

// Put stores the entry in KV first and the vector second, so a findable vector
// never points at a missing entry.
func (d *dependencies) Put(ctx context.Context, query string, vector []float32, entry semanticcache.Entry) error {
	cachedAt := entry.CachedAt
	if cachedAt.IsZero() {
		cachedAt = time.Now().UTC()
	}
	raw, err := json.Marshal(stored{Version: entry.Version, CachedAt: cachedAt.UTC(), Results: entry.Results, RequestedTopK: entry.RequestedTopK})
	if err != nil {
		return fmt.Errorf("semantic cache marshal: %w", err)
	}
	id := entryID(query)
	if err := d.store.Put(ctx, keyPrefix+id, raw, d.ttl); err != nil {
		return fmt.Errorf("semantic cache write: %w", err)
	}
	if err := d.index.Upsert(ctx, id, vector); err != nil {
		return fmt.Errorf("semantic cache index: %w", err)
	}
	return nil
}

// Clear deletes every cache entry: the vectors first, then their KV values,
// page by page. Vectorize cannot delete everything in one call, so the KV keys
// (one per entry, named by vector id) are the list of what to remove. Vectors
// whose KV value had already expired are removed lazily by Find. The cache
// policy rejects entries from an older generation even if this fails part-way.
func (d *dependencies) Clear(ctx context.Context) error {
	cursor := ""
	for {
		keys, next, err := d.store.ListKeys(ctx, keyPrefix, cursor)
		if err != nil {
			return fmt.Errorf("semantic cache clear: %w", err)
		}
		if len(keys) > 0 {
			ids := make([]string, len(keys))
			for i, key := range keys {
				ids[i] = strings.TrimPrefix(key, keyPrefix)
			}
			if err := d.index.DeleteByIDs(ctx, ids); err != nil {
				return fmt.Errorf("semantic cache clear: %w", err)
			}
			if err := d.store.DeleteMany(ctx, keys); err != nil {
				return fmt.Errorf("semantic cache clear: %w", err)
			}
		}
		if next == "" {
			return nil
		}
		cursor = next
	}
}
