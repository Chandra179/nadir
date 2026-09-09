package chat

import (
	"fmt"
	"testing"
)

func TestBrokerRetainsOrderedFinishedStreamsBeforeRejectingActiveOnes(t *testing.T) {
	b := newBroker()
	first, ok := b.create("first")
	if !ok {
		t.Fatal("create first stream failed")
	}
	second, ok := b.create("second")
	if !ok {
		t.Fatal("create second stream failed")
	}
	second.finish()

	for i := 2; i < maxRetainedTurns; i++ {
		if _, ok := b.create(fmt.Sprintf("turn-%d", i)); !ok {
			t.Fatalf("create stream %d failed", i)
		}
	}
	if _, ok := b.create("new"); !ok {
		t.Fatal("broker rejected a new stream even though a finished stream was retained")
	}
	if b.get("first") != first {
		t.Fatal("oldest active stream was evicted")
	}
	if b.get("second") != nil {
		t.Fatal("finished stream was not evicted in insertion order")
	}
}

func TestTurnStreamReplayLogIsBoundedAndSignalsGap(t *testing.T) {
	s := newTurnStream()
	for i := 0; i < eventBuffer+2; i++ {
		s.publish(EventToken, "token")
	}

	s.mu.Lock()
	logLen := len(s.log)
	s.mu.Unlock()
	if logLen > eventBuffer {
		t.Fatalf("log length = %d, want <= %d", logLen, eventBuffer)
	}

	events, cancel := s.subscribe(1)
	defer cancel()
	first := <-events
	if first.Kind != EventReplayGap {
		t.Fatalf("first replay event kind = %v, want replay gap", first.Kind)
	}
}

func TestCancelFinishedTurnReturnsFalse(t *testing.T) {
	s := newTurnStream()
	s.finish()
	if s.cancelGeneration() {
		t.Fatal("cancelGeneration returned true for a finished stream")
	}
}
