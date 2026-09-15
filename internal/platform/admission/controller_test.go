package admission

import (
	"context"
	"errors"
	"testing"
	"time"

	"nadir/internal/platform/inference"
)

func TestControllerSharesOperationBudget(t *testing.T) {
	controller := New(Config{
		Retrieval: OperationConfig{MaxConcurrent: 1, QueueTimeout: 10 * time.Millisecond},
	})
	release, err := controller.Acquire(context.Background(), Retrieval)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	_, err = controller.Acquire(context.Background(), Retrieval)
	if !errors.Is(err, inference.ErrCapacity) {
		t.Fatalf("second retrieval admission error = %v, want ErrCapacity", err)
	}
}

func TestControllerKeepsOperationBudgetsIndependent(t *testing.T) {
	controller := New(Config{
		Retrieval:  OperationConfig{MaxConcurrent: 1, QueueTimeout: time.Millisecond},
		Generation: OperationConfig{MaxConcurrent: 1, QueueTimeout: time.Millisecond},
	})
	release, err := controller.Acquire(context.Background(), Retrieval)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	generationRelease, err := controller.Acquire(context.Background(), Generation)
	if err != nil {
		t.Fatalf("generation admission blocked by retrieval budget: %v", err)
	}
	generationRelease()
}
