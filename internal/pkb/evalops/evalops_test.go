package evalops

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// --- Sampler ---

func TestSampler_ZeroRate(t *testing.T) {
	s := NewSampler(0)
	for i := 0; i < 1000; i++ {
		if s.ShouldSample() {
			t.Fatal("rate=0 must never sample")
		}
	}
}

func TestSampler_FullRate(t *testing.T) {
	s := NewSampler(1)
	for i := 0; i < 100; i++ {
		if !s.ShouldSample() {
			t.Fatal("rate=1 must always sample")
		}
	}
}

func TestSampler_ApproximateRate(t *testing.T) {
	const rate = 0.1
	s := NewSampler(rate)
	var hits int
	const n = 10000
	for i := 0; i < n; i++ {
		if s.ShouldSample() {
			hits++
		}
	}
	got := float64(hits) / n
	// allow ±50% relative error (statistical tolerance)
	if got < rate*0.5 || got > rate*1.5 {
		t.Fatalf("rate %.2f want ~%.2f, got %.2f", rate, rate, got)
	}
}

func TestSampler_Clamp(t *testing.T) {
	NewSampler(-1) // must not panic
	NewSampler(2)  // must not panic
}

// --- DriftDetector ---

func TestDriftDetector_NoAlertBeforeBaseline(t *testing.T) {
	d := NewDriftDetector(3, 0.10)
	for i := 0; i < 2; i++ {
		drift, _ := d.Add(0.9)
		if drift {
			t.Fatal("no alert before baseline window full")
		}
	}
}

func TestDriftDetector_BaselineSetOnFullWindow(t *testing.T) {
	d := NewDriftDetector(3, 0.10)
	d.Add(0.9)
	d.Add(0.9)
	drift, mean := d.Add(0.9) // 3rd observation fills window
	if drift {
		t.Fatal("no drift alert on baseline window")
	}
	if mean < 0.89 || mean > 0.91 {
		t.Fatalf("expected mean ~0.9, got %.3f", mean)
	}
}

func TestDriftDetector_AlertOnDrop(t *testing.T) {
	d := NewDriftDetector(3, 0.10)
	// fill baseline at 1.0
	d.Add(1.0)
	d.Add(1.0)
	d.Add(1.0)
	// now drop to 0.5 — 50% drop >> 10% threshold
	drift, _ := d.Add(0.5)
	if !drift {
		t.Fatal("expected drift alert on 50% drop")
	}
}

func TestDriftDetector_NoAlertOnSmallDrop(t *testing.T) {
	d := NewDriftDetector(3, 0.10)
	d.Add(1.0)
	d.Add(1.0)
	d.Add(1.0)
	// 5% drop < 10% threshold
	drift, _ := d.Add(0.95)
	if drift {
		t.Fatal("no alert on drop below threshold")
	}
}

func TestDriftDetector_ConcurrentSafe(t *testing.T) {
	d := NewDriftDetector(10, 0.10)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.Add(0.8) //nolint
		}()
	}
	wg.Wait()
}

// --- TraceStore ---

func TestTraceStore_Append(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "traces*.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	f.Close()

	ts := NewTraceStore(path)
	rel := true
	rec := TraceRecord{
		Timestamp: time.Now().UTC(),
		Query:     "test query",
		Chunks: []ChunkSnap{
			{FilePath: "foo.md", Header: "Intro", LineStart: 1, Score: 0.9, Relevant: &rel},
		},
		ContextRelevance: 0.9,
	}
	if err := ts.Append(rec); err != nil {
		t.Fatal(err)
	}
	if err := ts.Append(rec); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	lines := splitLines(data)
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	var decoded TraceRecord
	if err := json.Unmarshal([]byte(lines[0]), &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if decoded.Query != "test query" {
		t.Fatalf("wrong query: %s", decoded.Query)
	}
}

func splitLines(b []byte) []string {
	var lines []string
	start := 0
	for i, c := range b {
		if c == '\n' {
			line := string(b[start:i])
			if line != "" {
				lines = append(lines, line)
			}
			start = i + 1
		}
	}
	return lines
}

func TestTraceStore_ConcurrentAppend(t *testing.T) {
	f, _ := os.CreateTemp(t.TempDir(), "traces*.jsonl")
	path := f.Name()
	f.Close()

	ts := NewTraceStore(path)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ts.Append(TraceRecord{Query: "q", Timestamp: time.Now().UTC()}) //nolint
		}()
	}
	wg.Wait()

	data, _ := os.ReadFile(path)
	lines := splitLines(data)
	if len(lines) != 20 {
		t.Fatalf("expected 20 lines, got %d", len(lines))
	}
}

// --- LLMContextJudge ---

func makeJudgeServer(response string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := judgeResp{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{{Message: struct {
				Content string `json:"content"`
			}{Content: response}}},
		}
		json.NewEncoder(w).Encode(resp) //nolint
	}))
}

func TestLLMContextJudge_HIGH(t *testing.T) {
	srv := makeJudgeServer("HIGH")
	defer srv.Close()
	j := NewLLMContextJudge(srv.URL+"/v1", "test-model", "")
	score, err := j.ScoreContext(context.Background(), "q", "text")
	if err != nil || score != 1.0 {
		t.Fatalf("HIGH -> 1.0, got %.2f err=%v", score, err)
	}
}

func TestLLMContextJudge_MEDIUM(t *testing.T) {
	srv := makeJudgeServer("MEDIUM")
	defer srv.Close()
	j := NewLLMContextJudge(srv.URL+"/v1", "test-model", "")
	score, err := j.ScoreContext(context.Background(), "q", "text")
	if err != nil || score != 0.5 {
		t.Fatalf("MEDIUM -> 0.5, got %.2f err=%v", score, err)
	}
}

func TestLLMContextJudge_LOW(t *testing.T) {
	srv := makeJudgeServer("LOW")
	defer srv.Close()
	j := NewLLMContextJudge(srv.URL+"/v1", "test-model", "")
	score, err := j.ScoreContext(context.Background(), "q", "text")
	if err != nil || score != 0.25 {
		t.Fatalf("LOW -> 0.25, got %.2f err=%v", score, err)
	}
}

func TestLLMContextJudge_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	j := NewLLMContextJudge(srv.URL+"/v1", "test-model", "")
	_, err := j.ScoreContext(context.Background(), "q", "text")
	if err == nil {
		t.Fatal("expected error on HTTP 500")
	}
}

// --- Monitor ---

func TestMonitor_RecordAsync_NoJudge(t *testing.T) {
	dir := t.TempDir()
	m := New(Config{
		SampleRate: 1.0,
		TraceFile:  dir + "/traces.jsonl",
		DriftWindow: 5,
		DriftThresh: 0.10,
		MaxWorkers:  2,
	})

	m.RecordAsync("test query", []ScoredChunk{
		{FilePath: "a.md", Header: "H1", LineStart: 1, Score: 0.8, Text: "content"},
	})

	// drain workers
	time.Sleep(100 * time.Millisecond)

	data, _ := os.ReadFile(dir + "/traces.jsonl")
	lines := splitLines(data)
	if len(lines) != 1 {
		t.Fatalf("expected 1 trace, got %d", len(lines))
	}
}

func TestMonitor_RecordAsync_SamplingFilters(t *testing.T) {
	dir := t.TempDir()
	m := New(Config{
		SampleRate: 0.0, // never sample
		TraceFile:  dir + "/traces.jsonl",
		MaxWorkers: 2,
	})
	m.RecordAsync("q", []ScoredChunk{{FilePath: "a.md"}})
	time.Sleep(50 * time.Millisecond)

	data, _ := os.ReadFile(dir + "/traces.jsonl")
	if len(splitLines(data)) != 0 {
		t.Fatal("rate=0 should not write traces")
	}
}

func TestMonitor_RecordAsync_PoolFull(t *testing.T) {
	// fill pool, extra calls should drop without blocking
	var processed atomic.Int32
	dir := t.TempDir()
	m := New(Config{
		SampleRate: 1.0,
		TraceFile:  dir + "/traces.jsonl",
		MaxWorkers: 1,
	})
	// saturate the worker pool
	m.workers <- struct{}{}
	// these should not block
	done := make(chan struct{})
	go func() {
		for i := 0; i < 10; i++ {
			m.RecordAsync("q", nil)
			processed.Add(1)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RecordAsync blocked when pool full")
	}
	<-m.workers // release slot
	_ = processed.Load()
}

func TestMonitor_WithLLMJudge(t *testing.T) {
	srv := makeJudgeServer("HIGH")
	defer srv.Close()

	dir := t.TempDir()
	m := New(Config{
		SampleRate:   1.0,
		TraceFile:    dir + "/traces.jsonl",
		DriftWindow:  3,
		DriftThresh:  0.10,
		JudgeBaseURL: srv.URL + "/v1",
		JudgeModel:   "test-model",
		MaxWorkers:   2,
	})

	m.RecordAsync("how does GC work?", []ScoredChunk{
		{FilePath: "gc.md", Header: "GC Phases", LineStart: 10, Score: 0.9, Text: "GC description"},
	})
	time.Sleep(300 * time.Millisecond)

	data, _ := os.ReadFile(dir + "/traces.jsonl")
	lines := splitLines(data)
	if len(lines) != 1 {
		t.Fatalf("expected 1 trace, got %d", len(lines))
	}
	var rec TraceRecord
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatal(err)
	}
	if rec.ContextRelevance != 1.0 {
		t.Fatalf("expected context_relevance=1.0, got %.2f", rec.ContextRelevance)
	}
	if rec.Chunks[0].Relevant == nil || !*rec.Chunks[0].Relevant {
		t.Fatal("chunk should be marked relevant")
	}
}
