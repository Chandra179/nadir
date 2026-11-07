package runtime

import (
	"context"
	"log/slog"

	"nadir/internal/bootstrap/configuration"
	"nadir/internal/core/observability"
	"nadir/internal/core/retrieval/cache"
	cloudflarecache "nadir/internal/providers/cloudflare/cache"
	"nadir/internal/providers/cloudflare/kv"
	"nadir/internal/providers/cloudflare/vectorize"
	qdrantcache "nadir/internal/providers/qdrant/cache"
	"nadir/internal/providers/qdrant/shared"
)

// newSemanticCache builds the semantic cache for the configured provider. Any
// failure is logged and returns nil: the cache is an optimization, so search
// continues without it.
func newSemanticCache(ctx context.Context, cfg *config.Config, clients qdrantutil.Clients, dimensions int, telemetry *observability.Recorder, log *slog.Logger) cache.SemanticCache {
	if !cfg.SemanticCache.Enabled {
		return nil
	}
	policy := cache.DependenciesConfig{
		Threshold: cfg.SemanticCache.Threshold,
		TTL:       cfg.SemanticCache.TTL,
		Telemetry: telemetry,
		Version:   cachePolicyVersion(cfg),
	}
	var location string
	var built cache.SemanticCache
	var err error

	if cfg.SemanticCache.Provider == "cloudflare" {
		location = "vectorize:" + cfg.Cloudflare.VectorizeIndex
		built, err = cloudflareSemanticCache(ctx, cfg, dimensions, policy)
	} else {
		location = "qdrant:" + cfg.SemanticCache.Collection
		built, err = qdrantSemanticCache(ctx, cfg, clients, dimensions, policy)
	}
	if err != nil {
		log.Error("semantic cache unavailable; searches are not cached", slog.String("provider", cfg.SemanticCache.Provider), slog.Any("error", err))
		return nil
	}
	log.Info("semantic cache enabled",
		slog.String("provider", cfg.SemanticCache.Provider),
		slog.String("location", location),
		slog.Float64("threshold", float64(cfg.SemanticCache.Threshold)))
	return built
}

func qdrantSemanticCache(ctx context.Context, cfg *config.Config, clients qdrantutil.Clients, dimensions int, policy cache.DependenciesConfig) (cache.SemanticCache, error) {
	backend, err := qdrantcache.NewDependencies(qdrantcache.DependenciesConfig{Clients: clients, Collection: cfg.SemanticCache.Collection})
	if err != nil {
		return nil, err
	}
	if err := backend.EnsureCollection(ctx, dimensions); err != nil {
		return nil, err
	}
	policy.Backend = backend
	return cache.NewDependencies(policy)
}

func cloudflareSemanticCache(ctx context.Context, cfg *config.Config, dimensions int, policy cache.DependenciesConfig) (cache.SemanticCache, error) {
	index, err := vectorize.New(vectorize.Config{AccountID: cfg.Cloudflare.AccountID, APIToken: cfg.Cloudflare.APIToken, Index: cfg.Cloudflare.VectorizeIndex})
	if err != nil {
		return nil, err
	}
	store, err := kv.New(kv.Config{AccountID: cfg.Cloudflare.AccountID, APIToken: cfg.Cloudflare.APIToken, NamespaceID: cfg.Cloudflare.KVNamespaceID})
	if err != nil {
		return nil, err
	}
	backend, err := cloudflarecache.NewDependencies(cloudflarecache.DependenciesConfig{Index: index, Store: store, TTL: cfg.SemanticCache.TTL})
	if err != nil {
		return nil, err
	}
	if err := backend.EnsureIndex(ctx, dimensions); err != nil {
		return nil, err
	}
	policy.Backend = backend
	return cache.NewDependencies(policy)
}
