package chat

import (
	"context"
	"sync"
)

// eventBuffer bounds a subscriber's queue. Generation is capped by the
// context-token budget (~2800 tokens), so a full replay never blocks the
// publisher; overflowing would mean a lost token.
const eventBuffer = 4096

type subscriber struct {
	ch chan TurnEvent
}

// turnStream is one turn's event log: published events are appended (with a
// monotonic seq) and fanned out to subscribers; new subscribers are
// replayed from their cursor. Safe for concurrent use; exactly one
// goroutine (the generation supervisor) publishes.
type turnStream struct {
	mu       sync.Mutex
	seq      int64
	log      []TurnEvent
	subs     map[*subscriber]struct{}
	finished bool
	cancel   context.CancelFunc
}

func newTurnStream() *turnStream {
	return &turnStream{subs: make(map[*subscriber]struct{})}
}

// cancelGeneration aborts the owning generation (if any). The supervisor
// observes the abort as a stream end and persists the partial answer.
func (s *turnStream) cancelGeneration() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
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
	for sub := range s.subs {
		select {
		case sub.ch <- ev:
		default: // buffer is sized above any turn's event count
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

	sub := &subscriber{ch: make(chan TurnEvent, eventBuffer)}
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
	mu    sync.Mutex
	turns map[string]*turnStream
}

func newBroker() *broker {
	return &broker{turns: make(map[string]*turnStream)}
}

func (b *broker) create(id string) *turnStream {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.turns) >= maxRetainedTurns {
		for turnID, stream := range b.turns {
			if stream.finished {
				delete(b.turns, turnID)
				break
			}
		}
	}
	stream := newTurnStream()
	b.turns[id] = stream
	return stream
}

func (b *broker) get(id string) *turnStream {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.turns[id]
}

// maxRetainedTurns bounds the in-memory event logs; finished streams are
// evicted oldest-insertion-first (map order) once the cap is hit.
const maxRetainedTurns = 64
