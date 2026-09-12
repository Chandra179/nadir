package api

import (
	"go.uber.org/zap"

	"nadir/internal/conversation/chat"
	"nadir/internal/knowledge/indexing"
	chatapi "nadir/internal/transport/http/chat"
	historyapi "nadir/internal/transport/http/history"
)

// defaultTopK backs the configured default when config leaves it unset.
const defaultTopK = 8

// DependenciesConfig groups everything needed to construct the API
// dependencies.
type DependenciesConfig struct {
	Ingest ingest.Ingest
	Store  documentResetter
	// History is optional: when nil, session pages 404, sessions are not
	// minted and the sidebar's chat list is simply empty.
	History sessionReader
	// Chat runs the chat use-case for the chat UI: start turn, subscribe to
	// its event stream, cancel it.
	Chat chat.Chat
	// TopK is the configured default result count; requests and the
	// composer fall back to it (then to defaultTopK). Resolved once here so
	// handlers never re-apply the fallback.
	TopK                 int
	MaxTopK              int
	SourcePaths          []string
	SourceIgnorePatterns []string
	MaxSourceFileBytes   int64
	MaxUploadBytes       int64
	Log                  *zap.Logger
}

type dependencies struct {
	ingest               ingest.Ingest
	store                documentResetter
	history              sessionReader
	topK                 int
	sourcePaths          []string
	sourceIgnorePatterns []string
	maxSourceFileBytes   int64
	maxUploadBytes       int64
	turns                *chatapi.Handlers
	hist                 *historyapi.Handlers
	log                  *zap.Logger
}

// NewDependencies builds the HTTP transport over the domain capabilities.
func NewDependencies(cfg DependenciesConfig) *dependencies {
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}
	topK := cfg.TopK
	if topK <= 0 {
		topK = defaultTopK
	}
	maxTopK := cfg.MaxTopK
	if maxTopK <= 0 {
		maxTopK = 50
	}
	return &dependencies{
		ingest:               cfg.Ingest,
		store:                cfg.Store,
		history:              cfg.History,
		topK:                 topK,
		sourcePaths:          cfg.SourcePaths,
		sourceIgnorePatterns: cfg.SourceIgnorePatterns,
		maxSourceFileBytes:   cfg.MaxSourceFileBytes,
		maxUploadBytes:       cfg.MaxUploadBytes,
		turns:                chatapi.New(chatapi.Config{Chat: cfg.Chat, TopK: topK, MaxTopK: maxTopK}),
		hist:                 historyapi.New(historyapi.Config{History: cfg.History, Chat: cfg.Chat, Log: log}),
		log:                  log,
	}
}
