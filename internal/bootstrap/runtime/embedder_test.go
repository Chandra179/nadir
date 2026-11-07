package runtime

import (
	"context"
	"testing"

	"nadir/internal/bootstrap/configuration"
)

func TestNewEmbedderHonorsProvider(t *testing.T) {
	cfg := &config.Config{
		Cloudflare: config.CloudflareConfig{AccountID: "acct", APIToken: "token"},
		Embedder:   config.EmbedderConfig{Provider: "cloudflare", Model: "@cf/m", Dimensions: 768},
	}
	emb, probe, err := newEmbedder(cfg, nil)
	if err != nil || emb.Dimensions() != 768 {
		t.Fatalf("cloudflare embedder = %v, %v", emb, err)
	}
	result, err := probe(context.Background())
	if err != nil || result.ConfiguredModel != "@cf/m" || result.Dimensions != 768 {
		t.Fatalf("probe = %+v, %v", result, err)
	}

	cfg.Cloudflare.APIToken = ""
	if _, _, err := newEmbedder(cfg, nil); err == nil {
		t.Fatal("cloudflare embedder without a token must fail")
	}

	local := &config.Config{Embedder: config.EmbedderConfig{Provider: "ollama", OllamaAddr: "http://o", Model: "m", Dimensions: 3}}
	emb, probe, err = newEmbedder(local, nil)
	if err != nil || emb.Dimensions() != 3 || probe == nil {
		t.Fatalf("ollama embedder = %v, %v", emb, err)
	}
}
