package api

import (
	"nadir/config"
	"nadir/internal/chat"
	"nadir/internal/generator"
	"nadir/internal/history"
	"nadir/internal/ingest"
	"nadir/internal/search"
	"nadir/internal/store"

	"go.uber.org/zap"
)

// DependenciesConfig groups everything needed to construct the API
// dependencies.
type DependenciesConfig struct {
	Search search.Search
	Ingest ingest.Ingest
	Store  store.Store
	// Generator backs POST /search's streamed answers; the chat use-case's
	// generation goes through Chat instead.
	Generator generator.Generator
	// History is optional: when nil, chat sessions/turns are not persisted
	// and the sidebar's chat list is simply empty.
	History history.History
	// Chat runs the full chat use-case (search → generate → persist) for
	// the chat UI.
	Chat chat.Chat
	TopK int
	// Config is the fully-loaded, env-overridden config used to boot this
	// server — surfaced read-only via the settings panel so users can see
	// what's actually running without shelling in to read config.yaml.
	Config *config.Config
	Log    *zap.Logger
}

type dependencies struct {
	search    search.Search
	ingest    ingest.Ingest
	store     store.Store
	generator generator.Generator
	history   history.History
	chat      chat.Chat
	topK      int
	cfg       *config.Config
	log       *zap.Logger
}

func NewDependencies(cfg DependenciesConfig) *dependencies {
	return &dependencies{
		search:    cfg.Search,
		ingest:    cfg.Ingest,
		store:     cfg.Store,
		generator: cfg.Generator,
		history:   cfg.History,
		chat:      cfg.Chat,
		topK:      cfg.TopK,
		cfg:       cfg.Config,
		log:       cfg.Log,
	}
}
