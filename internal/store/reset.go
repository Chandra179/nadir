package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	qdrant "github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const generationSeparator = "__generation_"

// resetCollectionOps is the narrow reset protocol used by the Qdrant Adapter.
// Keeping the protocol independent from generated Qdrant clients makes the
// destructive path fault-injectable without replacing the whole Store.
type resetCollectionOps struct {
	active      func(context.Context, string) (collection string, found bool, err error)
	create      func(context.Context, string) error
	switchAlias func(context.Context, string, string, bool) error
	list        func(context.Context) ([]string, error)
	delete      func(context.Context, string) error
}

// resetCollection builds and publishes an empty collection generation before
// retiring the current collection. The alias switch is the only visibility
// change: a failed build or switch leaves the previous collection addressable.
// Retired collections are cleaned after publication; cleanup errors are
// returned so a caller can retry without invalidating the new generation.
func resetCollection(ctx context.Context, baseName, aliasName string, ops resetCollectionOps) error {
	current, aliasFound, err := ops.active(ctx, aliasName)
	if err != nil {
		return fmt.Errorf("resolve active document collection: %w", err)
	}

	staged := baseName + generationSeparator + uuid.NewString()
	if err := ops.create(ctx, staged); err != nil {
		cleanupErr := ops.delete(ctx, staged)
		return joinResetErrors(fmt.Errorf("create staged document collection: %w", err), cleanupErr)
	}

	cleanupStaged := func() error {
		if err := ops.delete(ctx, staged); err != nil {
			return fmt.Errorf("cleanup staged document collection %q: %w", staged, err)
		}
		return nil
	}

	if err := ops.switchAlias(ctx, aliasName, staged, aliasFound); err != nil {
		return joinResetErrors(fmt.Errorf("publish document collection generation: %w", err), cleanupStaged())
	}

	// The current collection may be the configured legacy collection name or a
	// previous generated collection. Both are safe to retire only after the
	// alias points at the staged generation.
	retired := make(map[string]struct{})
	if current != "" && current != staged {
		retired[current] = struct{}{}
	}
	collections, err := ops.list(ctx)
	if err != nil {
		return fmt.Errorf("reset published but could not list retired collections: %w", err)
	}
	for _, name := range collections {
		if name == staged || name == aliasName {
			continue
		}
		if name == baseName || strings.HasPrefix(name, baseName+generationSeparator) {
			retired[name] = struct{}{}
		}
	}

	var cleanupErrs []error
	for name := range retired {
		if err := ops.delete(ctx, name); err != nil {
			cleanupErrs = append(cleanupErrs, fmt.Errorf("delete retired document collection %q: %w", name, err))
		}
	}
	if len(cleanupErrs) > 0 {
		return fmt.Errorf("document reset published successfully; cleanup is retryable: %w", errors.Join(cleanupErrs...))
	}
	return nil
}

func joinResetErrors(primary, cleanup error) error {
	if cleanup == nil {
		return primary
	}
	return errors.Join(primary, cleanup)
}

func activeAliasName(baseName string) string {
	return baseName + "__active"
}

func (s *dependencies) activeCollection(ctx context.Context, aliasName string) (string, bool, error) {
	resp, err := s.collection.ListAliases(ctx, &qdrant.ListAliasesRequest{})
	if err != nil {
		return "", false, err
	}
	for _, alias := range resp.GetAliases() {
		if alias.GetAliasName() == aliasName {
			return alias.GetCollectionName(), true, nil
		}
	}
	return "", false, nil
}

func (s *dependencies) switchActiveAlias(ctx context.Context, aliasName, target string, exists bool) error {
	create := &qdrant.AliasOperations{
		Action: &qdrant.AliasOperations_CreateAlias{
			CreateAlias: &qdrant.CreateAlias{AliasName: aliasName, CollectionName: target},
		},
	}
	actions := []*qdrant.AliasOperations{create}
	if exists {
		deleteAlias := &qdrant.AliasOperations{
			Action: &qdrant.AliasOperations_DeleteAlias{
				DeleteAlias: &qdrant.DeleteAlias{AliasName: aliasName},
			},
		}
		actions = []*qdrant.AliasOperations{deleteAlias, create}
	}
	_, err := s.collection.UpdateAliases(ctx, &qdrant.ChangeAliases{Actions: actions})
	return err
}

func (s *dependencies) listCollections(ctx context.Context) ([]string, error) {
	resp, err := s.collection.List(ctx, &qdrant.ListCollectionsRequest{})
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(resp.GetCollections()))
	for _, collection := range resp.GetCollections() {
		if name := collection.GetName(); name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}

func (s *dependencies) deleteCollection(ctx context.Context, name string) error {
	_, err := s.collection.Delete(ctx, &qdrant.DeleteCollection{CollectionName: name})
	if status.Code(err) == codes.NotFound {
		return nil
	}
	return err
}
