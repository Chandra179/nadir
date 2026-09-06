package api

import (
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
	// Chat runs the chat use-case (start turn + subscribe to its event
	// stream) for the
	// chat UI.
	Chat chat.Chat
	// TopK is the configured default result count, used when a request
	// doesn't specify one.
	TopK int
	Log  *zap.Logger
}

type dependencies struct {
	ingest  ingest.Ingest
	store   store.Store
	history history.History
	chat    chat.Chat
	topK    int
	log     *zap.Logger
}

func NewDependencies(cfg DependenciesConfig) *dependencies {
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}
	return &dependencies{
		ingest:  cfg.Ingest,
		store:   cfg.Store,
		history: cfg.History,
		chat:    cfg.Chat,
		topK:    cfg.TopK,
		log:     log,
	}
}
