package api

import (
	"context"
	"time"

	"go.uber.org/zap"

	"nadir/internal/conversation/chat"
	conversationhistory "nadir/internal/conversation/history"
	"nadir/internal/knowledge/indexing"
	chatapi "nadir/internal/transport/http/chat"
	historyapi "nadir/internal/transport/http/history"
)

// defaultTopK backs the configured default when config leaves it unset.
const defaultTopK = 8

// DependenciesConfig groups everything needed to construct the API
// dependencies.
type DependenciesConfig struct {
	Ingest indexing.Ingest
	Reset  func(context.Context) error
	// History is optional: when nil, session pages 404, sessions are not
	// minted and the sidebar's chat list is simply empty.
	History conversationhistory.Reader
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
	Readiness            ReadinessFunc
	ReadinessTimeout     time.Duration
	Log                  *zap.Logger
}

type dependencies struct {
	ingest               indexing.Ingest
	reset                func(context.Context) error
	topK                 int
	sourcePaths          []string
	sourceIgnorePatterns []string
	maxSourceFileBytes   int64
	maxUploadBytes       int64
	readiness            ReadinessFunc
	readinessTimeout     time.Duration
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
	readinessTimeout := cfg.ReadinessTimeout
	if readinessTimeout <= 0 {
		readinessTimeout = readinessTimeoutDefault
	}
	return &dependencies{
		ingest:               cfg.Ingest,
		reset:                cfg.Reset,
		topK:                 topK,
		sourcePaths:          cfg.SourcePaths,
		sourceIgnorePatterns: cfg.SourceIgnorePatterns,
		maxSourceFileBytes:   cfg.MaxSourceFileBytes,
		maxUploadBytes:       cfg.MaxUploadBytes,
		readiness:            cfg.Readiness,
		readinessTimeout:     readinessTimeout,
		turns:                chatapi.NewDependencies(chatapi.DependenciesConfig{Chat: cfg.Chat, TopK: topK, MaxTopK: maxTopK}),
		hist:                 historyapi.NewDependencies(historyapi.DependenciesConfig{History: cfg.History, Chat: cfg.Chat, Log: log}),
		log:                  log,
	}
}
