package server

import (
	"context"
	"fmt"

	"nadir/internal/bootstrap/configuration"
	"nadir/internal/bootstrap/runtime"
	"nadir/internal/core/conversation/history"
	cloudflared1 "nadir/internal/providers/cloudflare/d1"
	cloudflarehistory "nadir/internal/providers/cloudflare/history"
	qdranthistory "nadir/internal/providers/qdrant/history"
)

// historyBackend is the wiring-local union of what Chat's lifecycle and the
// history API read from one persistence Adapter. It is not a published
// contract: each consumer still declares its own narrow seam (ADR 0020), and
// both history Adapters satisfy all of them.
type historyBackend interface {
	history.Reader
	CreateSession(ctx context.Context, title string) (history.Session, error)
	TruncateSession(ctx context.Context, sessionID string, beforeSequence int) error
	AppendTurn(ctx context.Context, sessionID string, turn history.Turn, firstTurnTitle string) error
	DeleteSession(ctx context.Context, sessionID string) error
	DeleteAllSessions(ctx context.Context) error
}

// newHistory builds the configured history Adapter and prepares its storage
// (Qdrant collection or D1 tables). It returns a human-readable location for
// logs.
func newHistory(ctx context.Context, cfg *config.Config, graph *runtime.Runtime) (historyBackend, string, error) {
	if cfg.History.Provider == "d1" {
		db, err := cloudflared1.New(cloudflared1.Config{
			AccountID:  cfg.Cloudflare.AccountID,
			APIToken:   cfg.Cloudflare.APIToken,
			DatabaseID: cfg.Cloudflare.D1DatabaseID,
		})
		if err != nil {
			return nil, "", fmt.Errorf("history init: %w", err)
		}
		h, err := cloudflarehistory.NewDependencies(cloudflarehistory.DependenciesConfig{DB: db, TurnPageSize: cfg.History.TurnPageSize})
		if err != nil {
			return nil, "", fmt.Errorf("history init: %w", err)
		}
		if err := h.EnsureSchema(ctx); err != nil {
			return nil, "", err
		}
		return h, "d1:" + cfg.Cloudflare.D1DatabaseID, nil
	}
	h, err := qdranthistory.NewDependencies(qdranthistory.DependenciesConfig{
		Clients:      graph.Clients,
		Collection:   cfg.History.Collection,
		Dimensions:   graph.Embedder.Dimensions(),
		TurnPageSize: cfg.History.TurnPageSize,
	})
	if err != nil {
		return nil, "", fmt.Errorf("history init: %w", err)
	}
	if err := h.EnsureCollection(ctx); err != nil {
		return nil, "", fmt.Errorf("history ensure collection: %w", err)
	}
	return h, "qdrant:" + cfg.History.Collection, nil
}
