package store

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type resetTestOps struct {
	current      string
	collections  []string
	created      []string
	deleted      []string
	switchTarget string
	switchErr    error
	createErr    error
	deleteErr    map[string]error
}

func (f *resetTestOps) active(context.Context, string) (string, bool, error) {
	return f.current, f.current != "", nil
}

func (f *resetTestOps) create(_ context.Context, name string) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.created = append(f.created, name)
	f.collections = append(f.collections, name)
	return nil
}

func (f *resetTestOps) switchAlias(_ context.Context, _ string, target string, _ bool) error {
	if f.switchErr != nil {
		return f.switchErr
	}
	f.switchTarget = target
	f.current = target
	return nil
}

func (f *resetTestOps) list(context.Context) ([]string, error) {
	return append([]string(nil), f.collections...), nil
}

func (f *resetTestOps) delete(_ context.Context, name string) error {
	if err := f.deleteErr[name]; err != nil {
		return err
	}
	f.deleted = append(f.deleted, name)
	return nil
}

func (f *resetTestOps) callbacks() resetCollectionOps {
	return resetCollectionOps{
		active:      f.active,
		create:      f.create,
		switchAlias: f.switchAlias,
		list:        f.list,
		delete:      f.delete,
	}
}

func TestResetCollectionFailedCreateKeepsPreviousActiveCollection(t *testing.T) {
	f := &resetTestOps{
		current:   "documents_chunks",
		createErr: errors.New("schema unavailable"),
		deleteErr: map[string]error{},
	}

	err := resetCollection(context.Background(), "documents_chunks", "documents_chunks__active", f.callbacks())
	if err == nil || !strings.Contains(err.Error(), "create staged document collection") {
		t.Fatalf("reset error = %v, want staged-create error", err)
	}
	if f.current != "documents_chunks" {
		t.Fatalf("active collection changed after failed create: %q", f.current)
	}
	if f.switchTarget != "" {
		t.Fatalf("alias switched after failed create to %q", f.switchTarget)
	}
}

func TestResetCollectionFailedPublishCleansStageAndKeepsPreviousActiveCollection(t *testing.T) {
	f := &resetTestOps{
		current:     "documents_chunks",
		collections: []string{"documents_chunks"},
		switchErr:   errors.New("alias update unavailable"),
		deleteErr:   map[string]error{},
	}

	err := resetCollection(context.Background(), "documents_chunks", "documents_chunks__active", f.callbacks())
	if err == nil || !strings.Contains(err.Error(), "publish document collection generation") {
		t.Fatalf("reset error = %v, want publish error", err)
	}
	if f.current != "documents_chunks" {
		t.Fatalf("active collection changed after failed publish: %q", f.current)
	}
	if len(f.created) != 1 || len(f.deleted) != 1 || f.deleted[0] != f.created[0] {
		t.Fatalf("staged collection cleanup = created %v, deleted %v", f.created, f.deleted)
	}
}

func TestResetCollectionPublishesBeforeRetryableCleanup(t *testing.T) {
	old := "documents_chunks"
	f := &resetTestOps{
		current:     old,
		collections: []string{old, "documents_chunks" + generationSeparator + "orphan"},
		deleteErr:   map[string]error{old: errors.New("temporary delete failure")},
	}

	err := resetCollection(context.Background(), old, activeAliasName(old), f.callbacks())
	if err == nil || !strings.Contains(err.Error(), "cleanup is retryable") {
		t.Fatalf("reset error = %v, want retryable cleanup error", err)
	}
	if f.current == old || f.switchTarget == "" {
		t.Fatalf("reset did not publish a new generation: current=%q target=%q", f.current, f.switchTarget)
	}
	if f.switchTarget == old {
		t.Fatalf("reset switched alias back to old collection")
	}
}

func TestResetCollectionCleansRetiredGenerationsWithoutDeletingPublishedOne(t *testing.T) {
	old := "documents_chunks"
	orphan := old + generationSeparator + "orphan"
	f := &resetTestOps{
		current:     old,
		collections: []string{old, orphan},
		deleteErr:   map[string]error{},
	}

	if err := resetCollection(context.Background(), old, activeAliasName(old), f.callbacks()); err != nil {
		t.Fatal(err)
	}
	for _, deleted := range f.deleted {
		if deleted == f.switchTarget {
			t.Fatalf("published collection %q was deleted", deleted)
		}
	}
	if len(f.deleted) != 2 {
		t.Fatalf("deleted retired collections = %v, want old and orphan", f.deleted)
	}
}
