package runtime

import (
	"context"
	"strings"
	"testing"

	"nadir/internal/bootstrap/configuration"
	"nadir/internal/providers/qdrant/shared"
)

func TestNewDocumentStoreFailsClosedWithoutCredentialsOrBackend(t *testing.T) {
	ctx := context.Background()
	cloudflare := &config.Config{
		Documents:  config.DocumentsConfig{Store: "cloudflare"},
		Cloudflare: config.CloudflareConfig{D1DatabaseID: "db", DocumentsIndex: "docs"},
	}
	// Credentials are checked before any request is attempted.
	if store, err := newDocumentStore(ctx, cloudflare, qdrantutil.Clients{}, 3); store != nil || err == nil || !strings.Contains(err.Error(), "cloudflare document store") {
		t.Fatalf("store = %v, err = %v", store, err)
	}
	cloudflare.Cloudflare = config.CloudflareConfig{AccountID: "a", APIToken: "t", DocumentsIndex: "docs"}
	if _, err := newDocumentStore(ctx, cloudflare, qdrantutil.Clients{}, 3); err == nil || !strings.Contains(err.Error(), "database") {
		t.Fatalf("missing database id err = %v", err)
	}

	// Unusable Qdrant clients must also fail, never return a typed-nil store.
	qdrant := &config.Config{Documents: config.DocumentsConfig{Store: "qdrant"}, Qdrant: config.QdrantConfig{Collection: "c"}}
	if store, err := newDocumentStore(ctx, qdrant, qdrantutil.Clients{}, 3); store != nil || err == nil {
		t.Fatalf("store = %v, err = %v", store, err)
	}
}
