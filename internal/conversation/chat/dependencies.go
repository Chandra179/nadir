package chat

import (
	"sync"
	"time"

	"go.uber.org/zap"

	"nadir/internal/conversation/generation"
	"nadir/internal/retrieval/rewriting"
	"nadir/internal/retrieval/search"
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
	Generator generation.Generator
	// History is optional: when nil, no sessions are minted and turns are
	// not persisted.
	History historyStore
	// Rewriter is optional: when set (with History), follow-up turns are
	// rewritten into standalone search queries against the session's recent
	// turns before retrieval. Best-effort — failures fall back to the raw
	// query.
	Rewriter rewriting.Rewriter
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
	generator        generation.Generator
	history          historyStore
	mutations        *historyMutations
	rewriter         rewriting.Rewriter
	rewriteTurns     int
	maxContextTokens int
	persistTimeout   time.Duration
	broker           *broker
	model            string
	log              *zap.Logger
	generations      sync.WaitGroup
	persists         sync.WaitGroup
	lifecycleMu      sync.Mutex
	activeStarts     int
	activeDone       chan struct{}
	draining         bool
}

var _ Chat = (*dependencies)(nil)

// NewDependencies constructs the Chat lifecycle over retrieval and optional
// generation, history, and rewriting capabilities.
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
	activeDone := make(chan struct{})
	close(activeDone)
	return &dependencies{
		searcher:         cfg.Searcher,
		generator:        cfg.Generator,
		history:          cfg.History,
		mutations:        newHistoryMutations(cfg.History),
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
		model:      cfg.Model,
		log:        log,
		activeDone: activeDone,
	}
}
