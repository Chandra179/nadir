package chat

import "go.uber.org/zap"

// DependenciesConfig groups everything needed to construct the chat
// service.
type DependenciesConfig struct {
	Searcher Searcher
	// Generator is optional: when nil, Ask ignores Request.Generate.
	Generator Generator
	// History is optional: when nil, no sessions are minted and turns are
	// not persisted.
	History History
	// Model is stamped onto persisted turns for display in history replay.
	Model string
	Log   *zap.Logger
}

type dependencies struct {
	searcher  Searcher
	generator Generator
	history   History
	model     string
	log       *zap.Logger
}

var _ Chat = (*dependencies)(nil)

func NewDependencies(cfg DependenciesConfig) *dependencies {
	return &dependencies{
		searcher:  cfg.Searcher,
		generator: cfg.Generator,
		history:   cfg.History,
		model:     cfg.Model,
		log:       cfg.Log,
	}
}
