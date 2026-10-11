package assembly

import (
	"context"
	"log/slog"
	"net/http"

	config "nadir/internal/bootstrap/configuration"
	"nadir/internal/bootstrap/gates"
	"nadir/internal/bootstrap/readiness"
	"nadir/internal/core/conversation/chat"
	"nadir/internal/core/conversation/generation"
	"nadir/internal/core/conversation/history"
	"nadir/internal/core/conversation/rewriting"
	api "nadir/internal/edge/http"
)

// HistoryStore is the full chat-history capability: the Chat lifecycle's
// mutations plus the read side the HTTP transport serves.
type HistoryStore interface {
	history.Reader
	CreateSession(ctx context.Context, title string) (history.Session, error)
	TruncateSession(ctx context.Context, sessionID string, beforeSequence int) error
	AppendTurn(ctx context.Context, sessionID string, turn history.Turn, firstTurnTitle string) error
	DeleteSession(ctx context.Context, sessionID string) error
	DeleteAllSessions(ctx context.Context) error
}

// APIProviders are the conversation adapters for BuildAPI. Every field is
// optional: without a Generator turns return retrieval only, without History
// sessions are not persisted, and without a Rewriter follow-ups are searched
// as asked. Leave absent fields as nil interfaces.
type APIProviders struct {
	Generator generation.Generator
	Rewriter  rewriting.Rewriter
	History   HistoryStore
	// Readiness lists the dependency probes behind GET /api/v1/ready.
	Readiness []readiness.Dependency
}

// API is the assembled versioned JSON/SSE surface.
type API struct {
	// Mux serves /api/v1. The caller may mount more routes and middleware.
	Mux *http.ServeMux
	// Drain stops active generation and waits for it to finish.
	Drain func(context.Context) error
}

// BuildAPI wires the Chat lifecycle and the HTTP transport over a Runtime and
// the supplied conversation providers.
func BuildAPI(cfg *config.Config, graph *Runtime, prov APIProviders, log *slog.Logger) *API {
	chatConfig := chat.DependenciesConfig{
		Searcher:             graph.Searcher,
		RewriteTurns:         cfg.Rewriter.Turns,
		MaxContextTokens:     cfg.Chat.MaxContextTokens,
		ContextWindowTokens:  cfg.Generator.NumCtx,
		ReservedOutputTokens: cfg.Generator.MaxOutputTokens,
		EventBuffer:          cfg.Chat.EventBuffer,
		MaxEventLogBytes:     cfg.Chat.MaxEventLogBytes,
		MaxRetainedTurns:     cfg.Chat.MaxRetainedTurns,
		FinishedTurnTTL:      cfg.Chat.FinishedTurnTTL,
		PersistTimeout:       cfg.Chat.PersistTimeout,
		DestructiveGate:      graph.Gates.AcquireFunc(gates.Destructive),
		Model:                cfg.Generator.Model,
		Telemetry:            graph.Telemetry,
		Log:                  log,
	}
	apiConfig := api.DependenciesConfig{
		Ingest:                  graph.Ingest,
		DocumentVersions:        graph.DocumentVersions,
		Reset:                   graph.Reset,
		TopK:                    cfg.Qdrant.TopK,
		MaxTopK:                 cfg.Search.MaxTopK,
		DocumentsPaths:          cfg.Documents.Paths,
		DocumentsIgnorePatterns: cfg.Documents.IgnorePatterns,
		DocumentsMode:           cfg.Documents.Mode,
		HistorySessionPageSize:  cfg.History.SessionPageSize,
		MaxDocumentFileBytes:    cfg.Ingest.MaxFileBytes,
		MaxUploadBytes:          cfg.Ingest.MaxUploadBytes,
		ReadinessTimeout:        cfg.HTTP.ReadinessTimeout,
		Log:                     log,
	}
	if prov.Generator != nil {
		chatConfig.Generator = prov.Generator
	}
	if prov.History != nil {
		// The concrete Adapter satisfies each consumer's narrow, private
		// Interface. No aggregate history Interface is needed downstream.
		chatConfig.History = prov.History
		apiConfig.History = prov.History
	}
	if prov.Rewriter != nil {
		chatConfig.Rewriter = prov.Rewriter
	}
	chatService := chat.NewDependencies(chatConfig)
	apiConfig.Chat = chatService

	checker := readiness.NewDependencies(readiness.DependenciesConfig{Dependencies: prov.Readiness})
	apiConfig.Readiness = func(ctx context.Context) api.ReadinessReport {
		report := checker.Check(ctx)
		checks := make(map[string]api.ReadinessCheck, len(report.Checks))
		for name, check := range report.Checks {
			checks[name] = api.ReadinessCheck{
				Ready:       check.Ready,
				Model:       check.Model,
				LoadedModel: check.LoadedModel,
				Backend:     check.Backend,
				Device:      check.Device,
				Error:       check.Error,
				Details:     check.Details,
			}
		}
		return api.ReadinessReport{Ready: report.Ready, Checks: checks}
	}
	mux := api.NewRouter(http.NewServeMux(), api.NewDependencies(apiConfig))
	return &API{Mux: mux, Drain: chatService.Drain}
}
