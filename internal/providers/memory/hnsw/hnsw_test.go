package hnsw_test

import (
	"math"
	"math/rand/v2"
	"sort"
	"sync"
	"testing"

	"nadir/internal/providers/memory/hnsw"
)

func randomVectors(n, dims int, seed uint64) [][]float32 {
	rng := rand.New(rand.NewPCG(seed, seed+1))
	out := make([][]float32, n)
	for i := range out {
		v := make([]float32, dims)
		for j := range v {
			v[j] = float32(rng.NormFloat64())
		}
		out[i] = v
	}
	return out
}

func cosine(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	return dot / math.Sqrt(na*nb)
}

func exactTop(vectors [][]float32, removed map[uint32]bool, q []float32, k int) []uint32 {
	type pair struct {
		id  uint32
		sim float64
	}
	var all []pair
	for i, v := range vectors {
		if !removed[uint32(i)] {
			all = append(all, pair{uint32(i), cosine(q, v)})
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].sim > all[j].sim })
	ids := make([]uint32, 0, k)
	for _, p := range all[:min(k, len(all))] {
		ids = append(ids, p.id)
	}
	return ids
}

func build(t *testing.T, vectors [][]float32) *hnsw.Index {
	t.Helper()
	ix := hnsw.New(hnsw.Config{Seed: 7})
	for i, v := range vectors {
		if err := ix.Add(uint32(i), v); err != nil {
			t.Fatal(err)
		}
	}
	return ix
}

func recall(ix *hnsw.Index, vectors [][]float32, removed map[uint32]bool, queries [][]float32, k int) float64 {
	var found, total int
	for _, q := range queries {
		want := map[uint32]bool{}
		for _, id := range exactTop(vectors, removed, q, k) {
			want[id] = true
		}
		for _, h := range ix.Search(q, k, 64) {
			if want[h.ID] {
				found++
			}
		}
		total += len(want)
	}
	return float64(found) / float64(total)
}

func TestSearchRecallAgainstExact(t *testing.T) {
	vectors := randomVectors(5000, 48, 1)
	ix := build(t, vectors)
	got := recall(ix, vectors, nil, randomVectors(100, 48, 2), 10)
	if got < 0.95 {
		t.Fatalf("recall@10 = %.3f, want >= 0.95", got)
	}
}

func TestSearchOrdersBestFirstAndReportsCosine(t *testing.T) {
	vectors := randomVectors(300, 16, 3)
	ix := build(t, vectors)
	q := vectors[42]
	hits := ix.Search(q, 5, 64)
	if len(hits) != 5 || hits[0].ID != 42 || math.Abs(float64(hits[0].Sim)-1) > 1e-4 {
		t.Fatalf("hits = %+v, want the query vector itself first with similarity 1", hits)
	}
	for i := 1; i < len(hits); i++ {
		if hits[i].Sim > hits[i-1].Sim {
			t.Fatalf("hits not sorted best first: %+v", hits)
		}
	}
	if want := cosine(q, vectors[hits[1].ID]); math.Abs(want-float64(hits[1].Sim)) > 1e-4 {
		t.Fatalf("sim = %v, want cosine %v", hits[1].Sim, want)
	}
}

func TestRemovedIDsNeverReturnedAndRecallHolds(t *testing.T) {
	vectors := randomVectors(3000, 32, 4)
	ix := build(t, vectors)
	removed := map[uint32]bool{}
	for i := 0; i < 3000; i += 3 {
		ix.Remove(uint32(i))
		removed[uint32(i)] = true
	}
	queries := randomVectors(60, 32, 5)
	for _, q := range queries {
		for _, h := range ix.Search(q, 10, 64) {
			if removed[h.ID] {
				t.Fatalf("removed id %d returned", h.ID)
			}
		}
	}
	if got := recall(ix, vectors, removed, queries, 10); got < 0.9 {
		t.Fatalf("recall@10 with tombstones = %.3f, want >= 0.9", got)
	}
	if ix.Len() != 2000 || !ix.NeedsRebuild() {
		t.Fatalf("Len=%d NeedsRebuild=%v, want 2000 and true", ix.Len(), ix.NeedsRebuild())
	}
}

func TestSameSeedBuildsSameResults(t *testing.T) {
	vectors := randomVectors(800, 24, 6)
	a, b := build(t, vectors), build(t, vectors)
	for _, q := range randomVectors(20, 24, 7) {
		x, y := a.Search(q, 10, 40), b.Search(q, 10, 40)
		if len(x) != len(y) {
			t.Fatalf("length differs: %d vs %d", len(x), len(y))
		}
		for i := range x {
			if x[i] != y[i] {
				t.Fatalf("results differ at %d: %+v vs %+v", i, x[i], y[i])
			}
		}
	}
}

func TestEdgeCases(t *testing.T) {
	ix := hnsw.New(hnsw.Config{})
	if got := ix.Search([]float32{1, 0}, 3, 10); got != nil {
		t.Fatalf("empty index returned %+v", got)
	}
	if err := ix.Add(1, nil); err == nil {
		t.Fatal("empty vector accepted")
	}
	if err := ix.Add(1, []float32{1, 0}); err != nil {
		t.Fatal(err)
	}
	if err := ix.Add(1, []float32{0, 1}); err == nil {
		t.Fatal("duplicate id accepted")
	}
	if err := ix.Add(2, []float32{1, 0, 0}); err == nil {
		t.Fatal("wrong dimensions accepted")
	}
	if got := ix.Search([]float32{1, 0, 0}, 3, 10); got != nil {
		t.Fatalf("query with wrong dimensions returned %+v", got)
	}
	if err := ix.Add(3, []float32{0, 0}); err != nil {
		t.Fatalf("zero vector rejected: %v", err)
	}
	ix.Remove(1)
	ix.Remove(1)
	ix.Remove(99)
	hits := ix.Search([]float32{1, 0}, 5, 10)
	if len(hits) != 1 || hits[0].ID != 3 {
		t.Fatalf("hits = %+v, want only the zero vector", hits)
	}
}

func TestConcurrentSearchIsRaceFree(t *testing.T) {
	vectors := randomVectors(1000, 16, 8)
	ix := build(t, vectors)
	queries := randomVectors(32, 16, 9)
	var wg sync.WaitGroup
	for _, q := range queries {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ix.Search(q, 10, 40)
		}()
	}
	wg.Wait()
}

func TestRejectsNonFiniteVectorsWithoutChangingTheIndex(t *testing.T) {
	ix := hnsw.New(hnsw.Config{})
	for _, bad := range [][]float32{{float32(math.NaN()), 1}, {float32(math.Inf(1)), 0}, {0, float32(math.Inf(-1))}} {
		if err := ix.Add(1, bad); err == nil {
			t.Fatalf("Add(%v) succeeded", bad)
		}
	}
	// The failed adds must not have fixed the dimensions or consumed the id.
	if err := ix.Add(1, []float32{1, 0, 0}); err != nil {
		t.Fatalf("valid add after rejected ones: %v", err)
	}
	if got := ix.Search([]float32{float32(math.NaN()), 0, 0}, 1, 10); got != nil {
		t.Fatalf("non-finite query returned %+v", got)
	}
}

// TestRandomOperationsMatchExactSearch interleaves adds, removes (including
// re-adding a removed id) and searches, checking every result against brute
// force: only live ids, correct cosine values, best first.
func TestRandomOperationsMatchExactSearch(t *testing.T) {
	const dims = 12
	rng := rand.New(rand.NewPCG(21, 22))
	ix := hnsw.New(hnsw.Config{Seed: 3})
	live := map[uint32][]float32{}
	var nextID uint32
	for step := 0; step < 4000; step++ {
		switch op := rng.IntN(10); {
		case op < 5 || len(live) < 20:
			v := randomVectors(1, dims, uint64(step)+1000)[0]
			id := nextID
			nextID++
			if err := ix.Add(id, v); err != nil {
				t.Fatal(err)
			}
			live[id] = v
		case op < 7:
			for id := range live { // random live id
				ix.Remove(id)
				delete(live, id)
				break
			}
		default:
			q := randomVectors(1, dims, uint64(step)+9000)[0]
			hits := ix.Search(q, 5, 50)
			if want := min(5, len(live)); len(hits) != want {
				t.Fatalf("step %d: %d hits, want %d from %d live", step, len(hits), want, len(live))
			}
			for i, h := range hits {
				v, ok := live[h.ID]
				if !ok {
					t.Fatalf("step %d: returned id %d is not live", step, h.ID)
				}
				if math.Abs(cosine(q, v)-float64(h.Sim)) > 1e-4 {
					t.Fatalf("step %d: id %d sim %v, cosine %v", step, h.ID, h.Sim, cosine(q, v))
				}
				if i > 0 && h.Sim > hits[i-1].Sim {
					t.Fatalf("step %d: hits not best first: %+v", step, hits)
				}
			}
		}
	}
	if ix.Len() != len(live) {
		t.Fatalf("Len = %d, want %d", ix.Len(), len(live))
	}
}
