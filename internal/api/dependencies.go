package api

import (
	"nadir/config"
	"nadir/internal/chat"
	"nadir/internal/history"
	"nadir/internal/ingest"
	"nadir/internal/store"

	"go.uber.org/zap"
)

// DependenciesConfig groups everything needed to construct the API
// dependencies.
type DependenciesConfig struct {
	Ingest ingest.Ingest
	Store  store.Store
	// History is optional: when nil, chat sessions/turns are not persisted
	// and the sidebar's chat list is simply empty.
	History history.History
	// Chat runs the full chat use-case (search → generate → persist) for
	// the chat UI.
	Chat chat.Chat
	// TopK is the configured default result count, used when a request
	// doesn't specify one.
	TopK int
	// Config is the fully-loaded, env-overridden config used to boot this
	// server — surfaced read-only via the settings panel so users can see
	// what's actually running without shelling in to read config.yaml.
	Config *config.Config
	Log    *zap.Logger
}

type dependencies struct {
	ingest  ingest.Ingest
	store   store.Store
	history history.History
	chat    chat.Chat
	topK    int
	cfg     *config.Config
	log     *zap.Logger
}

func NewDependencies(cfg DependenciesConfig) *dependencies {
	return &dependencies{
		ingest:  cfg.Ingest,
		store:   cfg.Store,
		history: cfg.History,
		chat:    cfg.Chat,
		topK:    cfg.TopK,
		cfg:     cfg.Config,
		log:     cfg.Log,
	}
}
