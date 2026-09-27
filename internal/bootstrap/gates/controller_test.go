package gates

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestControllerSharesOperationBudget(t *testing.T) {
	controller := New(Config{
		Indexing: OperationConfig{MaxConcurrent: 1, QueueTimeout: 10 * time.Millisecond},
	})
	release, err := controller.Acquire(context.Background(), Indexing)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	_, err = controller.Acquire(context.Background(), Indexing)
	if !errors.Is(err, ErrCapacity) {
		t.Fatalf("second indexing gate error = %v, want ErrCapacity", err)
	}
}

func TestControllerKeepsOperationBudgetsIndependent(t *testing.T) {
	controller := New(Config{
		Indexing:    OperationConfig{MaxConcurrent: 1, QueueTimeout: time.Millisecond},
		Destructive: OperationConfig{MaxConcurrent: 1, QueueTimeout: time.Millisecond},
	})
	release, err := controller.Acquire(context.Background(), Indexing)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	destructiveRelease, err := controller.Acquire(context.Background(), Destructive)
	if err != nil {
		t.Fatalf("destructive gate blocked by indexing budget: %v", err)
	}
	destructiveRelease()
}
