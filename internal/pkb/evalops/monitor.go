package evalops

import (
	"context"
	"log"
	"time"
)

// ScoredChunk is a minimal copy of pkb.ScoredChunk fields needed by evalops.
// Avoids a circular import between pkb and pkb/evalops.
type ScoredChunk struct {
	FilePath  string
	Header    string
	LineStart int
	Score     float32
	Text      string
}

// Monitor samples live search calls, judges context relevance async,
// persists traces, and detects metric drift.
type Monitor struct {
	sampler *Sampler
	judge   ContextJudge  // nil = skip LLM scoring
	store   *TraceStore   // nil = skip persistence
	drift   *DriftDetector // nil = skip drift detection
	workers chan struct{}  // concurrency limiter
}

// Config holds Monitor constructor parameters.
type Config struct {
	SampleRate    float64 // 0.0–1.0 fraction of calls to sample (e.g. 0.05)
	TraceFile     string  // JSONL file path; empty = no persistence
	DriftWindow   int     // rolling window size for drift detection; 0 = disabled
	DriftThresh   float64 // relative drop in context_relevance to alert (e.g. 0.10)
	JudgeBaseURL  string  // OpenAI-compat endpoint, e.g. "http://localhost:11434/v1"
	JudgeModel    string  // e.g. "llama3.1:8b-instruct-q4_K_M"
	JudgeAPIKey   string  // empty for Ollama
	MaxWorkers    int     // async goroutine pool size (default 4)
}

func New(cfg Config) *Monitor {
	m := &Monitor{}

	m.sampler = NewSampler(cfg.SampleRate)

	if cfg.JudgeBaseURL != "" && cfg.JudgeModel != "" {
		m.judge = NewLLMContextJudge(cfg.JudgeBaseURL, cfg.JudgeModel, cfg.JudgeAPIKey)
	}

	if cfg.TraceFile != "" {
		m.store = NewTraceStore(cfg.TraceFile)
	}

	if cfg.DriftWindow > 0 {
		m.drift = NewDriftDetector(cfg.DriftWindow, cfg.DriftThresh)
	}

	workers := cfg.MaxWorkers
	if workers <= 0 {
		workers = 4
	}
	m.workers = make(chan struct{}, workers)

	return m
}

// RecordAsync samples the call and, if selected, fires a background goroutine
// to judge + persist + check drift. Zero hot-path overhead when not sampled.
func (m *Monitor) RecordAsync(query string, chunks []ScoredChunk) {
	if !m.sampler.ShouldSample() {
		return
	}

	snaps := make([]ChunkSnap, len(chunks))
	for i, c := range chunks {
		snaps[i] = ChunkSnap{
			FilePath:  c.FilePath,
			Header:    c.Header,
			LineStart: c.LineStart,
			Score:     c.Score,
		}
	}

	// copy query+chunks before goroutine launch to avoid races
	q := query
	cs := chunks
	ss := snaps

	select {
	case m.workers <- struct{}{}:
	default:
		// pool full — drop sample rather than block hot path
		return
	}

	go func() {
		defer func() { <-m.workers }()
		m.process(q, cs, ss)
	}()
}

func (m *Monitor) process(query string, chunks []ScoredChunk, snaps []ChunkSnap) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var ctxRelevance float64
	if m.judge != nil && len(chunks) > 0 {
		var sum float64
		for i, c := range chunks {
			score, err := m.judge.ScoreContext(ctx, query, c.Text)
			if err != nil {
				continue
			}
			sum += score
			rel := score >= 0.5
			snaps[i].Relevant = &rel
		}
		ctxRelevance = sum / float64(len(chunks))
	}

	if m.store != nil {
		rec := TraceRecord{
			Timestamp:        time.Now().UTC(),
			Query:            query,
			Chunks:           snaps,
			ContextRelevance: ctxRelevance,
		}
		if err := m.store.Append(rec); err != nil {
			log.Printf("evalops: trace write error: %v", err)
		}
	}

	if m.drift != nil && ctxRelevance > 0 {
		isDrift, mean := m.drift.Add(ctxRelevance)
		if isDrift {
			log.Printf("evalops: DRIFT ALERT context_relevance mean=%.3f (threshold breach)", mean)
		}
	}
}
