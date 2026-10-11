package documents_test

import (
	"fmt"
	"math"
	"math/rand/v2"
	"testing"
	"time"

	"nadir/internal/core/documents/indexing"
	"nadir/internal/core/retrieval/search"
	"nadir/internal/providers/memory/documents"
)

const annDims = 32

func randomVector(rng *rand.Rand, dims int) []float32 {
	v := make([]float32, dims)
	for i := range v {
		v[i] = float32(rng.NormFloat64())
	}
	return v
}

// structured draws vectors near a 32-dimensional subspace of dims, which is
// closer to real text embeddings than isotropic noise. In 768 isotropic
// dimensions every pair is nearly orthogonal, so no ANN index can find
// meaningful neighbours and a benchmark on such data would mislead.
func structured(dims int) func(*rand.Rand) []float32 {
	const latent = 32
	proj := rand.New(rand.NewPCG(99, 100))
	basis := make([][]float32, latent)
	for i := range basis {
		basis[i] = randomVector(proj, dims)
	}
	return func(rng *rand.Rand) []float32 {
		v := make([]float32, dims)
		for _, b := range basis {
			z := float32(rng.NormFloat64())
			for j := range v {
				v[j] += z * b[j]
			}
		}
		for j := range v {
			v[j] += 0.5 * float32(rng.NormFloat64())
		}
		return v
	}
}

func fill(tb testing.TB, s *documents.Store, docs, perDoc, dims int, seed uint64) {
	tb.Helper()
	fillWith(tb, s, docs, perDoc, dims, seed, func(rng *rand.Rand) []float32 { return randomVector(rng, dims) })
}

// fillWith loads docs documents of perDoc chunks each. The same seed gives the
// same corpus, so an exact and an indexed store can be compared.
func fillWith(tb testing.TB, s *documents.Store, docs, perDoc, dims int, seed uint64, vector func(*rand.Rand) []float32) {
	tb.Helper()
	rng := rand.New(rand.NewPCG(seed, seed+1))
	must(tb, s.EnsureCollection(ctx, dims))
	for d := 0; d < docs; d++ {
		path := fmt.Sprintf("doc%03d.md", d)
		chunks := make([]indexing.IndexedChunk, perDoc)
		for c := range chunks {
			chunks[c] = indexing.IndexedChunk{
				Text: fmt.Sprintf("chunk %d of %s topic%d", c, path, rng.IntN(50)), FilePath: path,
				Header: "H", ChunkIndex: c, Vector: vector(rng),
			}
		}
		must(tb, s.ReplaceDocument(ctx, path, fmt.Sprintf("sha-%s-v1", path), chunks))
	}
}

func key(c search.SearchCandidate) string { return fmt.Sprintf("%s#%d", c.FilePath, c.ChunkIndex) }

func TestIndexedDenseLegMatchesExactScan(t *testing.T) {
	exact := documents.New(documents.Config{ExactOnly: true})
	indexed := documents.New(documents.Config{IndexThreshold: 2000})
	fill(t, exact, 60, 100, annDims, 1)
	fill(t, indexed, 60, 100, annDims, 1)

	rng := rand.New(rand.NewPCG(9, 10))
	var found, total int
	for q := 0; q < 50; q++ {
		vector := randomVector(rng, annDims)
		want, _ := exact.HybridSearch(ctx, vector, "", 10, nil)
		got, _ := indexed.HybridSearch(ctx, vector, "", 10, nil)
		wantIDs := map[string]float32{}
		for _, c := range want.Dense {
			wantIDs[key(c)] = c.Score
		}
		for _, c := range got.Dense {
			if score, ok := wantIDs[key(c)]; ok {
				found++
				if score != c.Score {
					t.Fatalf("%s: indexed score %v differs from exact %v", key(c), c.Score, score)
				}
			}
		}
		total += len(want.Dense)
	}
	if recall := float64(found) / float64(total); recall < 0.95 {
		t.Fatalf("dense recall@10 of the index = %.3f, want >= 0.95", recall)
	}
}

func TestIndexedStoreNeverReturnsReplacedVersions(t *testing.T) {
	s := documents.New(documents.Config{IndexThreshold: 500})
	fill(t, s, 20, 50, annDims, 2)
	rng := rand.New(rand.NewPCG(3, 4))
	// Replace every document with a version holding fewer chunks, enough
	// times to force tombstone rebuilds.
	for round := 2; round <= 4; round++ {
		for d := 0; d < 20; d++ {
			path := fmt.Sprintf("doc%03d.md", d)
			chunks := make([]indexing.IndexedChunk, 40)
			for c := range chunks {
				chunks[c] = indexing.IndexedChunk{Text: "fresh", FilePath: path, ChunkIndex: c, Vector: randomVector(rng, annDims)}
			}
			must(t, s.ReplaceDocument(ctx, path, fmt.Sprintf("sha-%s-v%d", path, round), chunks))
		}
	}
	for q := 0; q < 30; q++ {
		got, _ := s.HybridSearch(ctx, randomVector(rng, annDims), "", 20, nil)
		for _, c := range got.Dense {
			if want := fmt.Sprintf("sha-%s-v4", c.FilePath); c.SourceSHA != want {
				t.Fatalf("stale result %s has sha %s, want %s", key(c), c.SourceSHA, want)
			}
		}
	}
}

func TestFilteredQueriesStayExactOnALargeCorpus(t *testing.T) {
	exact := documents.New(documents.Config{ExactOnly: true})
	indexed := documents.New(documents.Config{IndexThreshold: 100})
	fill(t, exact, 30, 100, annDims, 5)
	fill(t, indexed, 30, 100, annDims, 5)
	vector := randomVector(rand.New(rand.NewPCG(6, 7)), annDims)
	filter := &search.Filter{FilePath: "doc007.md"}

	want, _ := exact.HybridSearch(ctx, vector, "", 10, filter)
	got, _ := indexed.HybridSearch(ctx, vector, "", 10, filter)
	if len(got.Dense) != 10 {
		t.Fatalf("filtered dense leg has %d results, want 10", len(got.Dense))
	}
	for i := range want.Dense {
		if key(got.Dense[i]) != key(want.Dense[i]) || got.Dense[i].FilePath != "doc007.md" {
			t.Fatalf("filtered result %d = %s, want exact %s", i, key(got.Dense[i]), key(want.Dense[i]))
		}
	}
}

func TestSnapshotRestoreRebuildsTheIndex(t *testing.T) {
	cfg := documents.Config{IndexThreshold: 500}
	s := documents.New(cfg)
	fill(t, s, 20, 50, annDims, 8)
	data, err := s.Snapshot()
	must(t, err)
	restored := documents.New(cfg)
	must(t, restored.Restore(data))

	rng := rand.New(rand.NewPCG(11, 12))
	for q := 0; q < 20; q++ {
		vector := randomVector(rng, annDims)
		a, _ := s.HybridSearch(ctx, vector, "", 10, nil)
		b, _ := restored.HybridSearch(ctx, vector, "", 10, nil)
		if len(a.Dense) != len(b.Dense) || (len(a.Dense) > 0 && key(a.Dense[0]) != key(b.Dense[0])) {
			t.Fatalf("restored store disagrees with the original: %v vs %v", a.Dense, b.Dense)
		}
	}
}

func BenchmarkHybridSearch(b *testing.B) {
	const dims = 768
	for _, chunks := range []int{1_000, 10_000, 50_000} {
		for _, mode := range []struct {
			name string
			cfg  documents.Config
		}{{"exact", documents.Config{ExactOnly: true}}, {"hnsw", documents.Config{IndexThreshold: 1}}} {
			s := documents.New(mode.cfg)
			start := time.Now()
			gen := structured(dims)
			fillWith(b, s, chunks/100, 100, dims, 1, gen)
			b.Logf("%s/%d: loaded in %s", mode.name, chunks, time.Since(start).Round(time.Millisecond))
			rng := rand.New(rand.NewPCG(2, 3))
			queries := make([][]float32, 32)
			for i := range queries {
				queries[i] = gen(rng)
			}
			b.Run(fmt.Sprintf("%s/%d", mode.name, chunks), func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					if _, err := s.HybridSearch(ctx, queries[i%len(queries)], "topic7 chunk", 10, nil); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// BenchmarkKeywordSearch isolates the lexical leg, which scans every chunk
// whatever the dense leg does.
func BenchmarkKeywordSearch(b *testing.B) {
	for _, chunks := range []int{1_000, 10_000, 50_000} {
		s := documents.New(documents.Config{ExactOnly: true})
		fill(b, s, chunks/100, 100, 8, 1)
		b.Run(fmt.Sprint(chunks), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if _, err := s.KeywordSearch(ctx, "topic7 chunk", 10, nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestUnusableVectorKeepsTheStoreExactAndWorking(t *testing.T) {
	s := documents.New(documents.Config{IndexThreshold: 5})
	must(t, s.EnsureCollection(ctx, 2))
	nan := float32(math.NaN())
	must(t, s.ReplaceDocument(ctx, "bad.md", "s", []indexing.IndexedChunk{{Text: "broken", FilePath: "bad.md", Vector: []float32{nan, 1}}}))
	for i := 0; i < 20; i++ { // crosses the threshold repeatedly; must not retry a doomed build
		path := fmt.Sprintf("ok%02d.md", i)
		must(t, s.ReplaceDocument(ctx, path, "s", []indexing.IndexedChunk{{Text: "fine", FilePath: path, Vector: []float32{1, float32(i)}}}))
	}
	got, err := s.HybridSearch(ctx, []float32{1, 0}, "", 3, nil)
	must(t, err)
	if len(got.Dense) != 3 || got.Dense[0].FilePath != "ok00.md" {
		t.Fatalf("dense = %+v, want ok00.md first", got.Dense)
	}
}
