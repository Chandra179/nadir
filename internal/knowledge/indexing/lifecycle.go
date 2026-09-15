package indexing

import (
	"context"
	"fmt"
	"sync"

	"nadir/internal/platform/observability"
)

// DocumentLifecycle coordinates a complete Indexing pass with a destructive
// Document reset. It is process-local by design; distributed deployments need
// a shared lease or fencing implementation at the private lifecycle seam.
type DocumentLifecycle struct {
	mu           sync.Mutex
	activeIngest int
	resetting    bool
	changed      chan struct{}
}

var _ lifecycleCoordinator = (*DocumentLifecycle)(nil)

// NewDocumentLifecycle creates the single-node lifecycle coordinator used by
// the API process.
func NewDocumentLifecycle() *DocumentLifecycle {
	return &DocumentLifecycle{changed: make(chan struct{})}
}

// BeginIngest allows an Indexing pass to run while excluding destructive
// reset. The matching EndIngest call must be made by the same operation.
func (d *DocumentLifecycle) BeginIngest(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		d.mu.Lock()
		if !d.resetting {
			d.activeIngest++
			d.mu.Unlock()
			return nil
		}
		changed := d.changed
		d.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

// EndIngest releases the read lock acquired by BeginIngest.
func (d *DocumentLifecycle) EndIngest() {
	d.mu.Lock()
	if d.activeIngest > 0 {
		d.activeIngest--
		if d.activeIngest == 0 {
			d.notifyLocked()
		}
	}
	d.mu.Unlock()
}

// Reset runs a destructive operation exclusively with respect to indexing.
// The callback is deliberately supplied by the Indexing composition seam so
// the transport never owns lifecycle coordination.
func (d *DocumentLifecycle) Reset(ctx context.Context, operation func(context.Context) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		d.mu.Lock()
		if !d.resetting && d.activeIngest == 0 {
			d.resetting = true
			d.mu.Unlock()
			break
		}
		changed := d.changed
		d.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}

	defer func() {
		d.mu.Lock()
		d.resetting = false
		d.notifyLocked()
		d.mu.Unlock()
	}()
	return operation(ctx)
}

func (d *DocumentLifecycle) notifyLocked() {
	close(d.changed)
	d.changed = make(chan struct{})
}

// DeleteAll publishes the reset through the Document Store and then clears
// semantic-cache entries. The lifecycle lock prevents a complete Indexing
// pass from overlapping the destructive operation.
func (d *dependencies) DeleteAll(ctx context.Context) error {
	ctx, operation := observability.Start(ctx, d.telemetry, d.log, "reset")
	var operationErr error
	defer func() {
		outcome := "success"
		if operationErr != nil {
			outcome = "error"
		}
		operation.End(outcome, operationErr)
	}()
	if d.reset == nil {
		operationErr = fmt.Errorf("document reset is not configured")
		return operationErr
	}
	if d.coordinator == nil {
		operationErr = fmt.Errorf("document lifecycle coordinator is not configured")
		return operationErr
	}
	releaseAdmission := func() {}
	if d.destructiveAdmission != nil {
		var err error
		releaseAdmission, err = d.destructiveAdmission(ctx)
		if err != nil {
			operationErr = fmt.Errorf("destructive operation admission: %w", err)
			return operationErr
		}
		defer releaseAdmission()
	}
	operationErr = d.coordinator.Reset(ctx, func(ctx context.Context) error {
		if err := d.reset(ctx); err != nil {
			return err
		}
		if d.clearCache != nil {
			if err := d.clearCache(ctx); err != nil {
				return fmt.Errorf("clear semantic cache: %w", err)
			}
		}
		return nil
	})
	return operationErr
}
