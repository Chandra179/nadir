package chat

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	defaultEventBuffer      = 4096
	maxEventBuffer          = 16384
	defaultMaxEventLogBytes = 1 << 20
	maxMaxEventLogBytes     = 16 << 20
	defaultMaxRetainedTurns = 64
	maxMaxRetainedTurns     = 1024
	defaultFinishedTurnTTL  = 10 * time.Minute
)

type brokerConfig struct {
	EventBuffer      int
	MaxEventLogBytes int64
	MaxRetainedTurns int
	FinishedTurnTTL  time.Duration
	Log              *zap.Logger
}

func normalizeBrokerConfig(cfg brokerConfig) brokerConfig {
	if cfg.EventBuffer <= 0 {
		cfg.EventBuffer = defaultEventBuffer
	}
	if cfg.EventBuffer > maxEventBuffer {
		cfg.EventBuffer = maxEventBuffer
	}
	if cfg.MaxEventLogBytes <= 0 {
		cfg.MaxEventLogBytes = defaultMaxEventLogBytes
	}
	if cfg.MaxEventLogBytes > maxMaxEventLogBytes {
		cfg.MaxEventLogBytes = maxMaxEventLogBytes
	}
	if cfg.MaxRetainedTurns <= 0 {
		cfg.MaxRetainedTurns = defaultMaxRetainedTurns
	}
	if cfg.MaxRetainedTurns > maxMaxRetainedTurns {
		cfg.MaxRetainedTurns = maxMaxRetainedTurns
	}
	if cfg.FinishedTurnTTL <= 0 {
		cfg.FinishedTurnTTL = defaultFinishedTurnTTL
	}
	return cfg
}

type subscriber struct {
	ch chan TurnEvent
}

// turnStream is one turn's event log: published events are appended (with a
// monotonic seq) and fanned out to subscribers; new subscribers are
// replayed from their cursor. Safe for concurrent use; exactly one
// goroutine (the generation supervisor) publishes.
type turnStream struct {
	mu               sync.Mutex
	seq              int64
	log              []TurnEvent
	logBytes         int64
	subs             map[*subscriber]struct{}
	finished         bool
	finishedAt       time.Time
	cancel           context.CancelFunc
	eventBuffer      int
	maxEventLogBytes int64
	logger           *zap.Logger
}

func newTurnStream(configs ...brokerConfig) *turnStream {
	var cfg brokerConfig
	if len(configs) > 0 {
		cfg = configs[0]
	}
	cfg = normalizeBrokerConfig(cfg)
	return &turnStream{
		subs:             make(map[*subscriber]struct{}),
		eventBuffer:      cfg.EventBuffer,
		maxEventLogBytes: cfg.MaxEventLogBytes,
		logger:           cfg.Log,
	}
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
	s.logBytes += int64(len(text) + 32)
	// Keep the newest event even if one unusually large event exceeds the
	// configured byte budget; never index an empty log while trimming.
	for len(s.log) > 1 && (len(s.log) > s.eventBuffer || s.logBytes > s.maxEventLogBytes) {
		s.logBytes -= int64(len(s.log[0].Text) + 32)
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
	sub := &subscriber{ch: make(chan TurnEvent, s.eventBuffer+1)}
	if len(s.log) > 0 && since > 0 && since < s.log[0].Seq-1 {
		if s.logger != nil {
			s.logger.Warn("replay cursor fell behind retained event log",
				zap.Int64("since", since), zap.Int64("oldest_seq", s.log[0].Seq))
		}
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
	mu               sync.Mutex
	turns            map[string]*turnStream
	ordered          []string
	maxRetainedTurns int
	finishedTurnTTL  time.Duration
	config           brokerConfig
}

func newBroker(configs ...brokerConfig) *broker {
	var cfg brokerConfig
	if len(configs) > 0 {
		cfg = configs[0]
	}
	cfg = normalizeBrokerConfig(cfg)
	return &broker{
		turns:            make(map[string]*turnStream),
		maxRetainedTurns: cfg.MaxRetainedTurns,
		finishedTurnTTL:  cfg.FinishedTurnTTL,
		config:           cfg,
	}
}

func (b *broker) create(id string) (*turnStream, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pruneLocked()
	if len(b.turns) >= b.maxRetainedTurns {
		return nil, false
	}
	stream := newTurnStream(b.config)
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

// cancelAll asks every retained stream to stop its owning generation. It
// snapshots under the broker lock so stream cancellation cannot block broker
// lookups or retention pruning.
func (b *broker) cancelAll() {
	b.mu.Lock()
	streams := make([]*turnStream, 0, len(b.turns))
	for _, stream := range b.turns {
		streams = append(streams, stream)
	}
	b.mu.Unlock()

	for _, stream := range streams {
		stream.cancelGeneration()
	}
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
		if finished && now.Sub(finishedAt) >= b.finishedTurnTTL {
			delete(b.turns, id)
			continue
		}
		kept = append(kept, id)
	}
	b.ordered = kept

	for len(b.turns) >= b.maxRetainedTurns {
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
