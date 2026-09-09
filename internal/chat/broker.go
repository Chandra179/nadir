package chat

import (
	"context"
	"sync"
	"time"
)

// eventBuffer bounds each subscriber queue and maxEventLogBytes bounds the
// retained replay log. Both limits are independent: a client may disconnect
// for a long time without allowing a turn to grow without bound.
const eventBuffer = 4096
const maxEventLogBytes = 1 << 20

const (
	maxRetainedTurns = 64
	finishedTurnTTL  = 10 * time.Minute
)

type subscriber struct {
	ch chan TurnEvent
}

// turnStream is one turn's event log: published events are appended (with a
// monotonic seq) and fanned out to subscribers; new subscribers are
// replayed from their cursor. Safe for concurrent use; exactly one
// goroutine (the generation supervisor) publishes.
type turnStream struct {
	mu         sync.Mutex
	seq        int64
	log        []TurnEvent
	logBytes   int
	subs       map[*subscriber]struct{}
	finished   bool
	finishedAt time.Time
	cancel     context.CancelFunc
}

func newTurnStream() *turnStream {
	return &turnStream{subs: make(map[*subscriber]struct{})}
}

// cancelGeneration aborts the owning generation (if any). The supervisor
// observes the abort as a stream end and persists the partial answer.
func (s *turnStream) cancelGeneration() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finished || s.cancel == nil {
		return false
	}
	s.cancel()
	return true
}

func (s *turnStream) setCancel(cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancel = cancel
}

func (s *turnStream) publish(kind EventKind, text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finished {
		return
	}
	s.seq++
	ev := TurnEvent{Seq: s.seq, Kind: kind, Text: text}
	s.log = append(s.log, ev)
	s.logBytes += len(text) + 32
	for len(s.log) > eventBuffer || s.logBytes > maxEventLogBytes {
		s.logBytes -= len(s.log[0].Text) + 32
		s.log = s.log[1:]
	}
	for sub := range s.subs {
		select {
		case sub.ch <- ev:
		default:
			// Do not silently lose tokens. Close a slow subscriber so its
			// EventSource reconnects with its last cursor and receives a
			// replay (or an explicit resync event if the log was trimmed).
			delete(s.subs, sub)
			close(sub.ch)
		}
	}
}

// finish marks the log complete and closes every subscriber channel; later
// subscribers get a replay-and-close.
func (s *turnStream) finish() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finished {
		return
	}
	s.finished = true
	s.finishedAt = time.Now()
	for sub := range s.subs {
		close(sub.ch)
	}
	s.subs = nil
}

// subscribe replays the log after since into a fresh queue, then attaches
// to live events. The returned cancel detaches the subscriber exactly once.
func (s *turnStream) subscribe(since int64) (<-chan TurnEvent, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// One extra slot is reserved for EventReplayGap when the cursor is older
	// than the retained window.
	sub := &subscriber{ch: make(chan TurnEvent, eventBuffer+1)}
	if len(s.log) > 0 && since > 0 && since < s.log[0].Seq-1 {
		sub.ch <- TurnEvent{
			Seq:  s.log[0].Seq,
			Kind: EventReplayGap,
			Text: "stream history was trimmed; reload the saved turn",
		}
	}
	for _, ev := range s.log {
		if ev.Seq > since {
			sub.ch <- ev
		}
	}
	if s.finished {
		close(sub.ch)
		return sub.ch, func() {}
	}
	s.subs[sub] = struct{}{}

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			if _, ok := s.subs[sub]; ok {
				delete(s.subs, sub)
				close(sub.ch)
			}
		})
	}
	return sub.ch, cancel
}

// broker holds every in-flight or recently finished turn stream so
// transports can subscribe by turn id — including reconnects, which replay
// from their Last-Event-ID cursor instead of failing.
type broker struct {
	mu      sync.Mutex
	turns   map[string]*turnStream
	ordered []string
}

func newBroker() *broker {
	return &broker{turns: make(map[string]*turnStream)}
}

func (b *broker) create(id string) (*turnStream, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pruneLocked()
	if len(b.turns) >= maxRetainedTurns {
		return nil, false
	}
	stream := newTurnStream()
	b.turns[id] = stream
	b.ordered = append(b.ordered, id)
	return stream, true
}

func (b *broker) get(id string) *turnStream {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pruneLocked()
	return b.turns[id]
}

func (b *broker) pruneLocked() {
	now := time.Now()
	kept := b.ordered[:0]
	for _, id := range b.ordered {
		stream, ok := b.turns[id]
		if !ok {
			continue
		}
		stream.mu.Lock()
		finished := stream.finished
		finishedAt := stream.finishedAt
		stream.mu.Unlock()
		if finished && now.Sub(finishedAt) >= finishedTurnTTL {
			delete(b.turns, id)
			continue
		}
		kept = append(kept, id)
	}
	b.ordered = kept

	for len(b.turns) >= maxRetainedTurns {
		removed := false
		for i, id := range b.ordered {
			stream := b.turns[id]
			stream.mu.Lock()
			finished := stream.finished
			stream.mu.Unlock()
			if !finished {
				continue
			}
			delete(b.turns, id)
			b.ordered = append(b.ordered[:i], b.ordered[i+1:]...)
			removed = true
			break
		}
		if !removed {
			break
		}
	}
}
