package inference

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestGateBoundsConcurrentWork(t *testing.T) {
	gate := NewGate(1, time.Second)
	release, err := gate.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	started := make(chan func(), 1)
	go func() {
		release, err := gate.Acquire(context.Background())
		if err != nil {
			t.Errorf("second Acquire() error = %v", err)
			return
		}
		started <- release
	}()

	select {
	case release := <-started:
		release()
		t.Fatal("second operation acquired a slot early")
	case <-time.After(20 * time.Millisecond):
	}

	release()
	select {
	case release := <-started:
		release()
	case <-time.After(time.Second):
		t.Fatal("second operation did not acquire after release")
	}
}

func TestGateTimesOutWhenBusy(t *testing.T) {
	gate := NewGate(1, 10*time.Millisecond)
	release, err := gate.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	_, err = gate.Acquire(context.Background())
	if !errors.Is(err, ErrCapacity) {
		t.Fatalf("Acquire() error = %v, want ErrCapacity", err)
	}
}

func TestGateHonorsContextCancellation(t *testing.T) {
	gate := NewGate(1, time.Second)
	release, err := gate.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = gate.Acquire(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Acquire() error = %v, want context.Canceled", err)
	}
}

func TestGateReleaseIsIdempotent(t *testing.T) {
	gate := NewGate(1, time.Second)
	release, err := gate.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	release()
	release()

	next, err := gate.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire() after idempotent release error = %v", err)
	}
	next()
}
