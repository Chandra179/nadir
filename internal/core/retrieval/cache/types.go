package cache

import "time"

// Candidate is the cache-owned representation of a retrieval result. Search
// maps its own candidates at this boundary instead of sharing a grab-bag type.
type Candidate struct {
	Text        string
	WindowText  string
	FilePath    string
	Header      string
	SectionPath string
	LineStart   int
	ChunkIndex  int
	SourceSHA   string
	Score       float32
}

// Entry is the provider-neutral cache record exchanged with a persistence
// Adapter. Expiration and version validation remain cache policy.
type Entry struct {
	Version  string
	CachedAt time.Time
	Results  []Candidate
	// RequestedTopK is the result count the cached search asked for. Zero
	// means the record predates the field; callers then treat len(Results) as
	// the request size.
	RequestedTopK int
}

// Lookup is a cache hit: the stored results and the request size they answer.
type Lookup struct {
	Results       []Candidate
	RequestedTopK int
}
