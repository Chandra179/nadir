package evalops

import (
	"math/rand"
	"sync/atomic"
)

// Sampler implements reservoir-style probabilistic sampling.
// ShouldSample returns true for approximately sampleRate fraction of calls.
// Thread-safe via atomic counter for sequence; rand used only for sampling decision.
type Sampler struct {
	sampleRate float64 // 0.0–1.0; 0.05 = sample 5% of live queries
	counter    atomic.Uint64
}

func NewSampler(rate float64) *Sampler {
	if rate < 0 {
		rate = 0
	}
	if rate > 1 {
		rate = 1
	}
	return &Sampler{sampleRate: rate}
}

func (s *Sampler) ShouldSample() bool {
	if s.sampleRate <= 0 {
		return false
	}
	if s.sampleRate >= 1 {
		return true
	}
	s.counter.Add(1)
	return rand.Float64() < s.sampleRate //nolint:gosec
}
