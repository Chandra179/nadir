package chat

import (
	"go.uber.org/zap"

	"nadir/internal/rewriter"
)

const defaultRewriteTurns = 4

// DependenciesConfig groups everything needed to construct the chat
// service.
type DependenciesConfig struct {
	Searcher Searcher
	// Generator is optional: when nil, Ask ignores Request.Generate.
	Generator Generator
	// History is optional: when nil, no sessions are minted and turns are
	// not persisted.
	History History
	// Rewriter is optional: when set (with History), follow-up turns are
	// rewritten into standalone search queries against the session's recent
	// turns before retrieval. Best-effort — failures fall back to the raw
	// query.
	Rewriter rewriter.Rewriter
	// RewriteTurns caps how many prior turns the rewriter sees
	// (<= 0 → defaultRewriteTurns).
	RewriteTurns int
	// Model is stamped onto persisted turns for display in history replay.
	Model string
	Log   *zap.Logger
}

type dependencies struct {
	searcher     Searcher
	generator    Generator
	history      History
	rewriter     rewriter.Rewriter
	rewriteTurns int
	model        string
	log          *zap.Logger
}

var _ Chat = (*dependencies)(nil)

func NewDependencies(cfg DependenciesConfig) *dependencies {
	rewriteTurns := cfg.RewriteTurns
	if rewriteTurns <= 0 {
		rewriteTurns = defaultRewriteTurns
	}
	return &dependencies{
		searcher:     cfg.Searcher,
		generator:    cfg.Generator,
		history:      cfg.History,
		rewriter:     cfg.Rewriter,
		rewriteTurns: rewriteTurns,
		model:        cfg.Model,
		log:          cfg.Log,
	}
}
