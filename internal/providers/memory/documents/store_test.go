package documents_test

import (
	"context"
	"testing"

	"nadir/internal/core/documents/indexing"
	"nadir/internal/core/retrieval/search"
	"nadir/internal/providers/memory/documents"
)

var ctx = context.Background()

func chunk(path string, index int, text string, vector ...float32) indexing.IndexedChunk {
	return indexing.IndexedChunk{Text: text, FilePath: path, Header: "H", ChunkIndex: index, Vector: vector}
}

// modes runs a test on the exact scan and on the HNSW path (index built from the
// first chunk), so every behavior is checked on both.
func modes(t *testing.T, run func(t *testing.T, cfg documents.Config)) {
	t.Helper()
	for name, cfg := range map[string]documents.Config{
		"exact": {ExactOnly: true},
		"hnsw":  {IndexThreshold: 1},
	} {
		t.Run(name, func(t *testing.T) { run(t, cfg) })
	}
}

func newStore(t *testing.T, cfg documents.Config) *documents.Store {
	t.Helper()
	s := documents.New(cfg)
	if err := s.EnsureCollection(ctx, 2); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestReplaceDocumentPublishesNewVersionOnly(t *testing.T) {
	modes(t, func(t *testing.T, cfg documents.Config) {
		s := newStore(t, cfg)
		must(t, s.ReplaceDocument(ctx, "a.md", "v1", []indexing.IndexedChunk{chunk("a.md", 0, "old text", 1, 0), chunk("a.md", 1, "old tail", 1, 0)}))
		must(t, s.ReplaceDocument(ctx, "a.md", "v2", []indexing.IndexedChunk{chunk("a.md", 0, "new text", 1, 0)}))

		got, _ := s.HybridSearch(ctx, []float32{1, 0}, "text", 10, nil)
		if len(got.Fused) != 1 || got.Fused[0].Text != "new text" || got.Fused[0].SourceSHA != "v2" {
			t.Fatalf("fused = %+v, want only the v2 chunk", got.Fused)
		}
		shas, _ := s.GetAllFileSHAs(ctx)
		if shas["a.md"] != "v2" || len(shas) != 1 {
			t.Fatalf("shas = %v", shas)
		}
	})
}

func TestReplaceDocumentRejectsEmptyAndWrongDimensions(t *testing.T) {
	modes(t, func(t *testing.T, cfg documents.Config) {
		s := newStore(t, cfg)
		must(t, s.ReplaceDocument(ctx, "a.md", "v1", []indexing.IndexedChunk{chunk("a.md", 0, "keep me", 1, 0)}))
		if err := s.ReplaceDocument(ctx, "a.md", "v2", nil); err == nil {
			t.Fatal("empty replacement succeeded")
		}
		if err := s.ReplaceDocument(ctx, "a.md", "v2", []indexing.IndexedChunk{chunk("a.md", 0, "bad", 1, 0, 0)}); err == nil {
			t.Fatal("wrong-dimension replacement succeeded")
		}
		if shas, _ := s.GetAllFileSHAs(ctx); shas["a.md"] != "v1" {
			t.Fatalf("failed replacement changed the live version: %v", shas)
		}
	})
}

func TestHybridSearchFusesDenseAndLexicalLegs(t *testing.T) {
	modes(t, func(t *testing.T, cfg documents.Config) {
		s := newStore(t, cfg)
		must(t, s.ReplaceDocument(ctx, "a.md", "s", []indexing.IndexedChunk{
			chunk("a.md", 0, "alpha cats", 1, 0),
			chunk("a.md", 1, "rare zebra token", 0, 1),
		}))
		// The vector points at chunk 0, but only chunk 1 contains the query term.
		got, _ := s.HybridSearch(ctx, []float32{1, 0}, "what is the zebra", 2, nil)
		if len(got.Dense) == 0 || got.Dense[0].ChunkIndex != 0 {
			t.Fatalf("dense leg = %+v, want chunk 0 first", got.Dense)
		}
		if len(got.Lexical) != 1 || got.Lexical[0].ChunkIndex != 1 {
			t.Fatalf("lexical leg = %+v, want only chunk 1", got.Lexical)
		}
		if len(got.Fused) != 2 {
			t.Fatalf("fused = %+v, want both chunks", got.Fused)
		}
	})
}

func TestFilterAndKeywordSearch(t *testing.T) {
	modes(t, func(t *testing.T, cfg documents.Config) {
		s := newStore(t, cfg)
		must(t, s.ReplaceDocument(ctx, "a.md", "sa", []indexing.IndexedChunk{chunk("a.md", 0, "shared word", 1, 0)}))
		must(t, s.ReplaceDocument(ctx, "b.md", "sb", []indexing.IndexedChunk{chunk("b.md", 0, "shared word", 1, 0)}))

		got, _ := s.KeywordSearch(ctx, "shared", 10, &search.Filter{FilePath: "b.md"})
		if len(got) != 1 || got[0].FilePath != "b.md" {
			t.Fatalf("keyword = %+v, want only b.md", got)
		}
		hybrid, _ := s.HybridSearch(ctx, []float32{1, 0}, "shared", 10, &search.Filter{SourceSHA: "sa"})
		if len(hybrid.Fused) != 1 || hybrid.Fused[0].FilePath != "a.md" {
			t.Fatalf("hybrid = %+v, want only a.md", hybrid.Fused)
		}
	})
}

func TestDeleteAndSnapshotRoundTrip(t *testing.T) {
	modes(t, func(t *testing.T, cfg documents.Config) {
		s := newStore(t, cfg)
		must(t, s.ReplaceDocument(ctx, "a.md", "sa", []indexing.IndexedChunk{chunk("a.md", 0, "persist me", 1, 0)}))
		must(t, s.ReplaceDocument(ctx, "b.md", "sb", []indexing.IndexedChunk{chunk("b.md", 0, "drop me", 0, 1)}))
		must(t, s.DeleteDocument(ctx, "b.md"))

		data, err := s.Snapshot()
		must(t, err)
		restored := documents.New(cfg)
		must(t, restored.Restore(data))
		must(t, restored.EnsureCollection(ctx, 2))

		got, _ := restored.HybridSearch(ctx, []float32{1, 0}, "persist", 5, nil)
		if len(got.Fused) != 1 || got.Fused[0].Text != "persist me" {
			t.Fatalf("restored fused = %+v", got.Fused)
		}

		must(t, restored.DeleteAll(ctx))
		if shas, _ := restored.GetAllFileSHAs(ctx); len(shas) != 0 {
			t.Fatalf("shas after DeleteAll = %v", shas)
		}
	})
}

func TestEnsureCollectionClearsOnDimensionChange(t *testing.T) {
	modes(t, func(t *testing.T, cfg documents.Config) {
		s := newStore(t, cfg)
		must(t, s.ReplaceDocument(ctx, "a.md", "sa", []indexing.IndexedChunk{chunk("a.md", 0, "x", 1, 0)}))
		must(t, s.EnsureCollection(ctx, 3))
		if shas, _ := s.GetAllFileSHAs(ctx); len(shas) != 0 {
			t.Fatalf("stale vectors survived a dimension change: %v", shas)
		}
	})
}

func must(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotIsCompactAndLossless(t *testing.T) {
	modes(t, func(t *testing.T, cfg documents.Config) {
		s := documents.New(cfg)
		must(t, s.EnsureCollection(ctx, 768))
		vector := make([]float32, 768)
		for i := range vector {
			vector[i] = float32(i)*0.000123 - 0.0456
		}
		must(t, s.ReplaceDocument(ctx, "a.md", "sa", []indexing.IndexedChunk{{Text: "hello world", FilePath: "a.md", Vector: vector}}))

		data, err := s.Snapshot()
		must(t, err)
		if len(data) > 5000 {
			t.Fatalf("snapshot of one 768-dim chunk is %d bytes, want a compact encoding", len(data))
		}
		restored := documents.New(cfg)
		must(t, restored.Restore(data))
		got, _ := restored.HybridSearch(ctx, vector, "hello", 1, nil)
		if len(got.Dense) != 1 || got.Dense[0].Score < 0.9999 {
			t.Fatalf("restored vector no longer matches itself: %+v", got.Dense)
		}
	})
}
