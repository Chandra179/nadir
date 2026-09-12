package chat

import (
	"time"

	"go.uber.org/zap"

	"nadir/internal/generator"
	"nadir/internal/rewriter"
	"nadir/internal/search"
)

const (
	defaultRewriteTurns    = 4
	defaultMaxContextToken = 2800
)

// DependenciesConfig groups everything needed to construct the chat
// service.
type DependenciesConfig struct {
	Searcher search.Retriever
	// Generator is optional: when nil, StartTurn ignores Request.Generate.
	Generator generator.Generator
	// History is optional: when nil, no sessions are minted and turns are
	// not persisted.
	History historyStore
	// Rewriter is optional: when set (with History), follow-up turns are
	// rewritten into standalone search queries against the session's recent
	// turns before retrieval. Best-effort — failures fall back to the raw
	// query.
	Rewriter rewriter.Rewriter
	// RewriteTurns caps how many prior turns the rewriter sees
	// (<= 0 → defaultRewriteTurns).
	RewriteTurns int
	// MaxContextTokens budgets the retrieved context inside the prompt
	// (<= 0 → defaultMaxContextToken).
	MaxContextTokens int
	// EventBuffer, MaxEventLogBytes, MaxRetainedTurns, and FinishedTurnTTL
	// bound the in-process replay broker.
	EventBuffer      int
	MaxEventLogBytes int64
	MaxRetainedTurns int
	FinishedTurnTTL  time.Duration
	// PersistTimeout bounds a best-effort history write.
	PersistTimeout time.Duration
	// Model is stamped onto persisted turns for display in history replay.
	Model string
	Log   *zap.Logger
}

type dependencies struct {
	searcher         search.Retriever
	generator        generator.Generator
	history          historyStore
	mutations        *historyMutations
	rewriter         rewriter.Rewriter
	rewriteTurns     int
	maxContextTokens int
	persistTimeout   time.Duration
	broker           *broker
	model            string
	log              *zap.Logger
}

var _ Chat = (*dependencies)(nil)

func NewDependencies(cfg DependenciesConfig) *dependencies {
	rewriteTurns := cfg.RewriteTurns
	if rewriteTurns <= 0 {
		rewriteTurns = defaultRewriteTurns
	}
	maxContextTokens := cfg.MaxContextTokens
	if maxContextTokens <= 0 {
		maxContextTokens = defaultMaxContextToken
	}
	persistTimeout := cfg.PersistTimeout
	if persistTimeout <= 0 {
		persistTimeout = 5 * time.Second
	}
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}
	return &dependencies{
		searcher:         cfg.Searcher,
		generator:        cfg.Generator,
		history:          cfg.History,
		mutations:        newHistoryMutations(),
		rewriter:         cfg.Rewriter,
		rewriteTurns:     rewriteTurns,
		maxContextTokens: maxContextTokens,
		persistTimeout:   persistTimeout,
		broker: newBroker(brokerConfig{
			EventBuffer:      cfg.EventBuffer,
			MaxEventLogBytes: cfg.MaxEventLogBytes,
			MaxRetainedTurns: cfg.MaxRetainedTurns,
			FinishedTurnTTL:  cfg.FinishedTurnTTL,
			Log:              log,
		}),
		model: cfg.Model,
		log:   log,
	}
}
