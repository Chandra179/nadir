package runtime

import (
	"context"
	"fmt"

	"nadir/internal/bootstrap/configuration"
	"nadir/internal/core/documents/indexing"
	"nadir/internal/core/retrieval/search"
	cloudflared1 "nadir/internal/providers/cloudflare/d1"
	cloudflaredocuments "nadir/internal/providers/cloudflare/documents"
	"nadir/internal/providers/cloudflare/vectorize"
	qdrantstore "nadir/internal/providers/qdrant/documents"
	"nadir/internal/providers/qdrant/shared"
)

// documentStore is the wiring-local union of what Retrieval and Indexing read
// from one document-store Adapter, plus reset. It is not a published contract:
// each consumer still declares its own narrow seam (ADR 0020), and both stores
// satisfy all of them.
type documentStore interface {
	HybridSearch(ctx context.Context, vector []float32, query string, topK int, filter *search.Filter) (search.HybridSearchResult, error)
	KeywordSearch(ctx context.Context, keyword string, topK int, filter *search.Filter) ([]search.SearchCandidate, error)
	GetAllFileSHAs(ctx context.Context) (map[string]string, error)
	ReplaceDocument(ctx context.Context, filePath, sourceSHA string, chunks []indexing.IndexedChunk) error
	DeleteDocument(ctx context.Context, filePath string) error
	DeleteAll(ctx context.Context) error
}

// newDocumentStore builds the configured document store and prepares its
// storage for vectors of the given size (a Qdrant collection, or D1 tables and
// a verified Vectorize index).
func newDocumentStore(ctx context.Context, cfg *config.Config, clients qdrantutil.Clients, dimensions int) (documentStore, error) {
	adaptive := cfg.Reranker.AdaptiveEnabled || cfg.Search.Fusion.Enabled
	if cfg.Documents.Store == "cloudflare" {
		db, err := cloudflared1.New(cloudflared1.Config{
			AccountID: cfg.Cloudflare.AccountID, APIToken: cfg.Cloudflare.APIToken, DatabaseID: cfg.Cloudflare.D1DatabaseID,
		})
		if err != nil {
			return nil, fmt.Errorf("cloudflare document store: %w", err)
		}
		index, err := vectorize.New(vectorize.Config{
			AccountID: cfg.Cloudflare.AccountID, APIToken: cfg.Cloudflare.APIToken, Index: cfg.Cloudflare.DocumentsIndex,
		})
		if err != nil {
			return nil, fmt.Errorf("cloudflare document store: %w", err)
		}
		store, err := cloudflaredocuments.NewDependencies(cloudflaredocuments.DependenciesConfig{
			DB: db, Index: index, IndexName: cfg.Cloudflare.DocumentsIndex,
			PrefetchMul: cfg.Qdrant.PrefetchMul, AdaptiveSignals: adaptive,
		})
		if err != nil {
			return nil, fmt.Errorf("cloudflare document store: %w", err)
		}
		if err := store.EnsureStore(ctx, dimensions); err != nil {
			return nil, fmt.Errorf("cloudflare document store: %w", err)
		}
		return store, nil
	}

	store, err := qdrantstore.NewDependencies(qdrantstore.DependenciesConfig{
		Clients:         clients,
		Collection:      cfg.Qdrant.Collection,
		PrefetchMul:     cfg.Qdrant.PrefetchMul,
		AdaptiveSignals: adaptive,
		HNSW: qdrantstore.HNSWConfig{
			M:                 cfg.Qdrant.HnswM,
			EfConstruct:       cfg.Qdrant.HnswEfConstruct,
			FullScanThreshold: cfg.Qdrant.HnswFullScanThreshold,
			IndexingThreshold: cfg.Qdrant.IndexingThreshold,
			Ef:                cfg.Qdrant.HnswEf,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("qdrant store init: %w", err)
	}
	if err := store.EnsureCollection(ctx, dimensions); err != nil {
		return nil, fmt.Errorf("qdrant ensure collection: %w", err)
	}
	return store, nil
}
