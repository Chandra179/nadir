package server

import (
	"io"
	"log/slog"
	"testing"

	"nadir/internal/bootstrap/configuration"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestRoleFactoriesHonorProviderAndEnabled(t *testing.T) {
	cf := config.CloudflareConfig{AccountID: "acct", APIToken: "token"}
	cfg := &config.Config{
		Cloudflare: cf,
		Generator:  config.GeneratorConfig{Enabled: true, Provider: "cloudflare", Model: "@cf/g"},
		Rewriter:   config.RewriterConfig{Enabled: true, Provider: "cloudflare", Model: "@cf/r"},
	}
	if g, err := newGenerator(cfg, nil, discard()); err != nil || g == nil {
		t.Fatalf("cloudflare generator = %v, %v", g, err)
	}
	if r, err := newRewriter(cfg, discard()); err != nil || r == nil {
		t.Fatalf("cloudflare rewriter = %v, %v", r, err)
	}

	cfg.Cloudflare.APIToken = ""
	if _, err := newGenerator(cfg, nil, discard()); err == nil {
		t.Fatal("cloudflare generator without a token must fail")
	}
	if _, err := newRewriter(cfg, discard()); err == nil {
		t.Fatal("cloudflare rewriter without a token must fail")
	}

	cfg.Generator.Enabled, cfg.Rewriter.Enabled = false, false
	if g, err := newGenerator(cfg, nil, discard()); g != nil || err != nil {
		t.Fatalf("disabled generator = %v, %v", g, err)
	}
	if r, err := newRewriter(cfg, discard()); r != nil || err != nil {
		t.Fatalf("disabled rewriter = %v, %v", r, err)
	}

	local := &config.Config{
		Generator: config.GeneratorConfig{Enabled: true, Provider: "ollama", OllamaAddr: "http://o", Model: "m"},
		Rewriter:  config.RewriterConfig{Enabled: true, Provider: "ollama", OllamaAddr: "http://o", Model: "m"},
	}
	if g, err := newGenerator(local, nil, discard()); err != nil || g == nil {
		t.Fatalf("ollama generator = %v, %v", g, err)
	}
	if r, err := newRewriter(local, discard()); err != nil || r == nil {
		t.Fatalf("ollama rewriter = %v, %v", r, err)
	}
	local.Rewriter.OllamaAddr = ""
	if r, err := newRewriter(local, discard()); err != nil || r != nil {
		t.Fatalf("unresolved ollama rewriter = %v, %v, want nil rewriter", r, err)
	}
}
