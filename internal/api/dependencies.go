package api

import (
	"go.uber.org/zap"

	chatapi "nadir/internal/api/chat"
	historyapi "nadir/internal/api/history"
	"nadir/internal/api/internal/render"
	"nadir/internal/chat"
	"nadir/internal/history"
	"nadir/internal/ingest"
	"nadir/internal/store"
)

// defaultTopK backs the configured default when config leaves it unset.
const defaultTopK = 8

// DependenciesConfig groups everything needed to construct the API
// dependencies.
type DependenciesConfig struct {
	Ingest ingest.Ingest
	Store  store.Store
	// History is optional: when nil, session pages 404, sessions are not
	// minted and the sidebar's chat list is simply empty.
	History history.History
	// Chat runs the chat use-case for the chat UI: start turn, subscribe to
	// its event stream, cancel it.
	Chat chat.Chat
	// TopK is the configured default result count; requests and the
	// composer fall back to it (then to defaultTopK). Resolved once here so
	// handlers never re-apply the fallback.
	TopK int
	Log  *zap.Logger
}

type dependencies struct {
	ingest  ingest.Ingest
	store   store.Store
	history history.History
	topK    int
	render  *render.Engine
	turns   *chatapi.Handlers
	hist    *historyapi.Handlers
	log     *zap.Logger
}

// NewDependencies builds the transport: the page shell's own handlers plus
// the feature transports (turns, history) over a shared render engine.
func NewDependencies(cfg DependenciesConfig) *dependencies {
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}
	topK := cfg.TopK
	if topK <= 0 {
		topK = defaultTopK
	}
	engine := render.New(log)

	return &dependencies{
		ingest:  cfg.Ingest,
		store:   cfg.Store,
		history: cfg.History,
		topK:    topK,
		render:  engine,
		turns:   chatapi.New(chatapi.Config{Chat: cfg.Chat, TopK: topK, Render: engine}),
		hist:    historyapi.New(historyapi.Config{History: cfg.History, Render: engine, Log: log}),
		log:     log,
	}
}
