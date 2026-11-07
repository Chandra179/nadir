package server

import (
	"context"
	"strings"
	"testing"

	"nadir/internal/bootstrap/configuration"
)

func TestNewHistoryD1RequiresDatabaseAndCredentials(t *testing.T) {
	cfg := &config.Config{
		Cloudflare: config.CloudflareConfig{AccountID: "acct", APIToken: "token"},
		History:    config.HistoryConfig{Enabled: true, Provider: "d1"},
	}
	if _, _, err := newHistory(context.Background(), cfg, nil); err == nil || !strings.Contains(err.Error(), "database") {
		t.Fatalf("missing database id err = %v", err)
	}
	cfg.Cloudflare = config.CloudflareConfig{D1DatabaseID: "db"}
	if _, _, err := newHistory(context.Background(), cfg, nil); err == nil {
		t.Fatal("missing credentials must fail before any request")
	}
}
