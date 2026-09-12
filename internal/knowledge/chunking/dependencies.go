package chunker

import (
	"regexp"
)

const (
	ProviderSentenceWindow = "sentence-window"

	defaultWindowSize   = 3
	defaultTOCThreshold = 0.6
)

var (
	reHTMLComment = regexp.MustCompile(`<!--.*?-->`)
	reTOCLine     = regexp.MustCompile(`(?m)^.{1,80}\s+\d+\s*$`)
	sentenceRe    = regexp.MustCompile(`[.!?]+[\s]+`)
)

// DependenciesConfig groups everything needed to construct a Chunker.
type DependenciesConfig struct {
	Provider     string
	ChunkSize    int
	ChunkOverlap int
	WindowSize   int // sentences before+after each sentence; used by sentence-window provider
}

// dependencies chunks documents using either a recursive text splitter
// (default) or a sentence-window strategy, selected by Provider.
type dependencies struct {
	provider     string
	chunkSize    int
	chunkOverlap int
	tocThreshold float64
	windowSize   int
}

var _ Chunker = (*dependencies)(nil)

func NewDependencies(cfg DependenciesConfig) *dependencies {
	windowSize := cfg.WindowSize
	if windowSize <= 0 {
		windowSize = defaultWindowSize
	}
	return &dependencies{
		provider:     cfg.Provider,
		chunkSize:    cfg.ChunkSize,
		chunkOverlap: cfg.ChunkOverlap,
		tocThreshold: defaultTOCThreshold,
		windowSize:   windowSize,
	}
}
