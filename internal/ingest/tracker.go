package ingest

import (
	"sort"
	"sync"
	"time"
)

// EventStatus is the state of one file within an in-progress run.
type EventStatus string

const (
	EventRunning   EventStatus = "running"
	EventProcessed EventStatus = "processed"
	EventSkipped   EventStatus = "skipped"
	EventFailed    EventStatus = "failed"
)

// RunEvent is the latest known state of a single file in the current run.
type RunEvent struct {
	Path   string
	Status EventStatus
	Detail string
	At     time.Time
}

// RunStatus is a snapshot of the current (or most recently finished) run,
// for dashboard polling.
type RunStatus struct {
	Target    string
	Running   bool
	StartedAt time.Time
	Events    []RunEvent // most recently updated first
}

// RunSummary is a completed run, kept for dashboard history.
type RunSummary struct {
	Target     string
	Processed  int
	Skipped    int
	Failed     int
	Err        string
	StartedAt  time.Time
	FinishedAt time.Time
}

const (
	maxTrackedEvents = 200
	maxRunHistory    = 20
)

// tracker holds in-memory, best-effort progress and history for dashboard
// display. It is not persisted; a server restart clears it.
type tracker struct {
	mu        sync.Mutex
	running   bool
	target    string
	startedAt time.Time
	events    []RunEvent
	index     map[string]int
	history   []RunSummary
}

func (t *tracker) start(target string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.running = true
	t.target = target
	t.startedAt = time.Now()
	t.events = nil
	t.index = make(map[string]int)
}

// record upserts the current status for a file: a second call for the same
// path (e.g. running -> processed) replaces the row instead of appending.
func (t *tracker) record(path string, status EventStatus, detail string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	ev := RunEvent{Path: path, Status: status, Detail: detail, At: time.Now()}
	if i, ok := t.index[path]; ok {
		t.events[i] = ev
		return
	}
	t.events = append(t.events, ev)
	t.index[path] = len(t.events) - 1
	if len(t.events) > maxTrackedEvents {
		t.events = t.events[len(t.events)-maxTrackedEvents:]
		t.index = make(map[string]int, len(t.events))
		for i, e := range t.events {
			t.index[e.Path] = i
		}
	}
}

func (t *tracker) finish(target string, res Result, runErr error, startedAt time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.running = false
	errMsg := ""
	if runErr != nil {
		errMsg = runErr.Error()
	}
	t.history = append(t.history, RunSummary{
		Target:     target,
		Processed:  res.Processed,
		Skipped:    res.Skipped,
		Failed:     res.Failed,
		Err:        errMsg,
		StartedAt:  startedAt,
		FinishedAt: time.Now(),
	})
	if len(t.history) > maxRunHistory {
		t.history = t.history[len(t.history)-maxRunHistory:]
	}
}

func (t *tracker) status() RunStatus {
	t.mu.Lock()
	defer t.mu.Unlock()
	events := make([]RunEvent, len(t.events))
	copy(events, t.events)
	sort.Slice(events, func(i, j int) bool { return events[i].At.After(events[j].At) })
	return RunStatus{Target: t.target, Running: t.running, StartedAt: t.startedAt, Events: events}
}

func (t *tracker) historySnapshot() []RunSummary {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]RunSummary, len(t.history))
	copy(out, t.history)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// Status returns a snapshot of the current or most recently finished run.
func (d *dependencies) Status() RunStatus {
	return d.tr.status()
}

// History returns completed runs, most recent first.
func (d *dependencies) History() []RunSummary {
	return d.tr.historySnapshot()
}
