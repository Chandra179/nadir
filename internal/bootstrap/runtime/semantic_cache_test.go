package runtime

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"nadir/internal/bootstrap/configuration"
	"nadir/internal/providers/qdrant/shared"
)

func TestNewSemanticCacheDegradesToNoCache(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()

	disabled := &config.Config{}
	if c := newSemanticCache(ctx, disabled, qdrantutil.Clients{}, 3, nil, log); c != nil {
		t.Fatalf("disabled cache = %v, want nil", c)
	}

	// Missing credentials must fail closed to "no cache", not a typed-nil
	// interface that search would then call.
	cloudflare := &config.Config{
		SemanticCache: config.SemanticCacheConfig{Enabled: true, Provider: "cloudflare"},
		Cloudflare:    config.CloudflareConfig{VectorizeIndex: "i", KVNamespaceID: "n"},
	}
	if c := newSemanticCache(ctx, cloudflare, qdrantutil.Clients{}, 3, nil, log); c != nil {
		t.Fatalf("cloudflare cache without credentials = %v, want nil", c)
	}

	// Unusable Qdrant clients do the same.
	qdrant := &config.Config{SemanticCache: config.SemanticCacheConfig{Enabled: true, Provider: "qdrant", Collection: "c"}}
	if c := newSemanticCache(ctx, qdrant, qdrantutil.Clients{}, 3, nil, log); c != nil {
		t.Fatalf("qdrant cache without clients = %v, want nil", c)
	}
}
