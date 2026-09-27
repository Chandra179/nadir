package cache

import "time"

// Candidate is the cache-owned representation of a retrieval result. Search
// maps its own candidates at this boundary instead of sharing a grab-bag type.
type Candidate struct {
	Text       string
	WindowText string
	FilePath   string
	Header     string
	LineStart  int
	ChunkIndex int
	SourceSHA  string
	Score      float32
}

// Entry is the provider-neutral cache record exchanged with a persistence
// Adapter. Expiration and version validation remain cache policy.
type Entry struct {
	Version  string
	CachedAt time.Time
	Results  []Candidate
}
