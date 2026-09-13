package indexing

import (
	"context"
	"fmt"
	"sync"
)

// DocumentLifecycle coordinates a complete Indexing pass with a destructive
// Document reset. It is process-local by design; distributed deployments need
// a shared lease or fencing implementation at the private lifecycle seam.
type DocumentLifecycle struct {
	mu sync.RWMutex
}

var _ lifecycleCoordinator = (*DocumentLifecycle)(nil)

// NewDocumentLifecycle creates the single-node lifecycle coordinator used by
// the API process.
func NewDocumentLifecycle() *DocumentLifecycle {
	return &DocumentLifecycle{}
}

// BeginIngest allows an Indexing pass to run while excluding destructive
// reset. The matching EndIngest call must be made by the same operation.
func (d *DocumentLifecycle) BeginIngest() {
	d.mu.RLock()
}

// EndIngest releases the read lock acquired by BeginIngest.
func (d *DocumentLifecycle) EndIngest() {
	d.mu.RUnlock()
}

// Reset runs a destructive operation exclusively with respect to indexing.
// The callback is deliberately supplied by the Indexing composition seam so
// the transport never owns lifecycle coordination.
func (d *DocumentLifecycle) Reset(ctx context.Context, operation func(context.Context) error) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return operation(ctx)
}

// DeleteAll publishes the reset through the Document Store and then clears
// semantic-cache entries. The lifecycle lock prevents a complete Indexing
// pass from overlapping the destructive operation.
func (d *dependencies) DeleteAll(ctx context.Context) error {
	if d.reset == nil {
		return fmt.Errorf("document reset is not configured")
	}
	if d.coordinator == nil {
		return fmt.Errorf("document lifecycle coordinator is not configured")
	}
	return d.coordinator.Reset(ctx, func(ctx context.Context) error {
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
}
