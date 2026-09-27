package gates

import (
	"context"
	"testing"
	"time"
)

func TestBackgroundRunnerBoundsAndDrains(t *testing.T) {
	runner := NewBackgroundRunner(1, time.Second)
	started := make(chan struct{})
	release := make(chan struct{})
	parent, cancelParent := context.WithCancel(context.Background())
	if !runner.Submit(parent, func(ctx context.Context) {
		close(started)
		<-release
		if ctx.Err() != nil {
			t.Errorf("job cancelled with request: %v", ctx.Err())
		}
	}) {
		t.Fatal("first job rejected")
	}
	<-started
	cancelParent()
	if runner.Submit(context.Background(), func(context.Context) {}) {
		t.Fatal("accepted job above limit")
	}
	done := make(chan error, 1)
	go func() { done <- runner.Close(context.Background()) }()
	select {
	case <-done:
		t.Fatal("closed before job finished")
	case <-time.After(10 * time.Millisecond):
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if runner.Submit(context.Background(), func(context.Context) {}) {
		t.Fatal("accepted job after close")
	}
}

func TestBackgroundRunnerCancelsAtShutdownDeadline(t *testing.T) {
	runner := NewBackgroundRunner(1, time.Second)
	started := make(chan struct{})
	finished := make(chan struct{})
	if !runner.Submit(context.Background(), func(ctx context.Context) { close(started); <-ctx.Done(); close(finished) }) {
		t.Fatal("job rejected")
	}
	<-started
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := runner.Close(shutdown); err == nil {
		t.Fatal("expected shutdown deadline error")
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("job was not cancelled")
	}
}
