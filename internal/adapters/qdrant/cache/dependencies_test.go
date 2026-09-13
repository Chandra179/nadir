package cache

import "testing"

func TestNewDependenciesRequiresSharedClients(t *testing.T) {
	if _, err := NewDependencies(DependenciesConfig{}); err == nil {
		t.Fatal("NewDependencies accepted an empty Qdrant client set")
	}
}
