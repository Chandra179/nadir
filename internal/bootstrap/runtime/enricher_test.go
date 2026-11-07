package runtime

import (
	"testing"

	"nadir/internal/bootstrap/configuration"
)

func TestNewEnricherHonorsProviderAndEnabled(t *testing.T) {
	cfg := &config.Config{
		Cloudflare: config.CloudflareConfig{AccountID: "acct", APIToken: "token"},
		Enrichment: config.EnrichmentConfig{Contextual: config.ContextualConfig{Enabled: true, Provider: "cloudflare", Model: "@cf/m"}},
	}
	if e, err := newEnricher(cfg); err != nil || e == nil {
		t.Fatalf("cloudflare enricher = %v, %v", e, err)
	}
	cfg.Cloudflare.AccountID = ""
	if _, err := newEnricher(cfg); err == nil {
		t.Fatal("cloudflare enricher without an account must fail")
	}
	cfg.Enrichment.Contextual.Enabled = false
	if e, err := newEnricher(cfg); e != nil || err != nil {
		t.Fatalf("disabled enricher = %v, %v", e, err)
	}
	cfg.Enrichment.Contextual = config.ContextualConfig{Enabled: true, Provider: "ollama", OllamaAddr: "http://o", Model: "m"}
	if e, err := newEnricher(cfg); err != nil || e == nil {
		t.Fatalf("ollama enricher = %v, %v", e, err)
	}
}
