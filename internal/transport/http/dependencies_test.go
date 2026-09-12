package api

import "testing"

func TestNewDependenciesResolvesTransportDefaultsOnce(t *testing.T) {
	d := NewDependencies(DependenciesConfig{})
	if d.topK != defaultTopK {
		t.Fatalf("topK = %d, want %d", d.topK, defaultTopK)
	}
	if d.turns == nil || d.hist == nil {
		t.Fatal("composition root did not construct feature transports")
	}
}
