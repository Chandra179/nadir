package indexing

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestIndexingDeleteAllRequiresExplicitCoordinator(t *testing.T) {
	coordinator := NewDependencies(DependenciesConfig{
		Reset: func(context.Context) error { return nil },
	})

	err := coordinator.DeleteAll(context.Background())
	if err == nil || !strings.Contains(err.Error(), "lifecycle coordinator is not configured") {
		t.Fatalf("DeleteAll() error = %v, want explicit coordinator error", err)
	}
}

func TestIndexingDeleteAllClearsCacheAfterSuccessfulReset(t *testing.T) {
	var reset bool
	var cleared bool
	coordinator := &dependencies{
		coordinator: NewDocumentLifecycle(),
		reset: func(context.Context) error {
			reset = true
			return nil
		},
		clearCache: func(context.Context) error {
			cleared = true
			return nil
		},
	}

	if err := coordinator.DeleteAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reset || !cleared {
		t.Fatalf("reset composition = reset=%v cache_cleared=%v, want true/true", reset, cleared)
	}
}

func TestIndexingDeleteAllDoesNotClearCacheAfterResetFailure(t *testing.T) {
	var cleared bool
	coordinator := &dependencies{
		coordinator: NewDocumentLifecycle(),
		reset:       func(context.Context) error { return context.Canceled },
		clearCache: func(context.Context) error {
			cleared = true
			return nil
		},
	}

	if err := coordinator.DeleteAll(context.Background()); err == nil {
		t.Fatal("DeleteAll() succeeded despite reset failure")
	}
	if cleared {
		t.Fatal("cache was cleared after reset failure")
	}
}

func TestIndexingDeleteAllWaitsForIndexingBeforeReset(t *testing.T) {
	lifecycle := NewDocumentLifecycle()
	var reset bool
	coordinator := &dependencies{
		coordinator: lifecycle,
		reset: func(context.Context) error {
			reset = true
			return nil
		},
	}
	lifecycle.BeginIngest()

	done := make(chan struct{})
	go func() {
		_ = coordinator.DeleteAll(context.Background())
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("reset ran while indexing was active")
	case <-time.After(25 * time.Millisecond):
	}
	if reset {
		t.Fatal("reset reached the store before indexing ended")
	}

	lifecycle.EndIngest()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reset did not run after indexing ended")
	}
	if !reset {
		t.Fatal("reset did not reach the store")
	}
}
