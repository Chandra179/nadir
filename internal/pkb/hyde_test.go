package pkb

import (
	"context"
	"errors"
	"math"
	"testing"
)

// stubGenerator returns a fixed document or an error.
type stubGenerator struct {
	doc string
	err error
}

func (s *stubGenerator) Generate(_ context.Context, _ string) (string, error) {
	return s.doc, s.err
}

// stubEmbedder returns a fixed vector.
type stubEmbedder struct {
	vec []float32
	err error
}

func (s *stubEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return s.vec, s.err
}
func (s *stubEmbedder) Dimensions() int { return len(s.vec) }

// stubStore captures the vector passed to HybridSearch.
type stubStore struct {
	Store
	gotVec []float32
}

func (s *stubStore) HybridSearch(_ context.Context, vec []float32, _ string, _ int) ([]ScoredChunk, error) {
	s.gotVec = vec
	return nil, nil
}

func TestAverageVectors(t *testing.T) {
	tests := []struct {
		name string
		vecs [][]float32
		want []float32 // expected direction after L2 norm; we compare angles
	}{
		{
			name: "single vector",
			vecs: [][]float32{{3, 4}},
			want: []float32{0.6, 0.8},
		},
		{
			name: "two identical vectors",
			vecs: [][]float32{{1, 0}, {1, 0}},
			want: []float32{1, 0},
		},
		{
			name: "two orthogonal vectors average to diagonal",
			vecs: [][]float32{{1, 0}, {0, 1}},
			// avg = {0.5, 0.5}, L2-norm → {0.707, 0.707}
			want: []float32{float32(1 / math.Sqrt2), float32(1 / math.Sqrt2)},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := averageVectors(tc.vecs)
			if len(got) != len(tc.want) {
				t.Fatalf("len got=%d want=%d", len(got), len(tc.want))
			}
			for i := range got {
				if math.Abs(float64(got[i]-tc.want[i])) > 1e-5 {
					t.Errorf("dim %d: got %.6f want %.6f", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestAverageVectors_Empty(t *testing.T) {
	if got := averageVectors(nil); got != nil {
		t.Errorf("expected nil for empty input, got %v", got)
	}
}

func TestL2Normalize_ZeroVector(t *testing.T) {
	v := []float32{0, 0, 0}
	got := l2Normalize(v)
	for _, x := range got {
		if x != 0 {
			t.Errorf("expected zero vector, got %v", got)
		}
	}
}

func TestHyDESearcher_PassesAveragedVecToStore(t *testing.T) {
	fixedVec := []float32{1, 0, 0}
	gen := &stubGenerator{doc: "hypothetical answer text"}
	emb := &stubEmbedder{vec: fixedVec}
	store := &stubStore{}

	searcher := NewHyDESearcher(gen, emb, store, 1)
	_, err := searcher.Search(context.Background(), "what is X?", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// vector passed to store should be L2-normalized average of fixedVec
	want := l2Normalize([]float32{1, 0, 0})
	if len(store.gotVec) != len(want) {
		t.Fatalf("store got vec len %d want %d", len(store.gotVec), len(want))
	}
	for i := range want {
		if math.Abs(float64(store.gotVec[i]-want[i])) > 1e-5 {
			t.Errorf("dim %d: got %.6f want %.6f", i, store.gotVec[i], want[i])
		}
	}
}

func TestHyDESearcher_AllGenerationsFail(t *testing.T) {
	gen := &stubGenerator{err: errors.New("LLM timeout")}
	emb := &stubEmbedder{vec: []float32{1, 0}}
	store := &stubStore{}

	searcher := NewHyDESearcher(gen, emb, store, 3)
	_, err := searcher.Search(context.Background(), "query", 5)
	if err == nil {
		t.Fatal("expected error when all generations fail")
	}
}

func TestHyDESearcher_PartialFailureSucceeds(t *testing.T) {
	calls := 0
	gen := &funcGenerator{fn: func(_ context.Context, _ string) (string, error) {
		calls++
		if calls == 1 {
			return "", errors.New("first call fails")
		}
		return "hypothetical doc", nil
	}}
	emb := &stubEmbedder{vec: []float32{0, 1}}
	store := &stubStore{}

	searcher := NewHyDESearcher(gen, emb, store, 3)
	_, err := searcher.Search(context.Background(), "query", 5)
	if err != nil {
		t.Fatalf("expected success with partial failures, got: %v", err)
	}
}

// funcGenerator is a HyDEGenerator backed by a plain function for table tests.
type funcGenerator struct {
	fn func(context.Context, string) (string, error)
}

func (f *funcGenerator) Generate(ctx context.Context, q string) (string, error) {
	return f.fn(ctx, q)
}
