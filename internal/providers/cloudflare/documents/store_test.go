package documents

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	indexing "nadir/internal/core/documents/indexing"
	"nadir/internal/core/retrieval/search"
)

var (
	axisX = []float32{1, 0, 0}
	axisY = []float32{0, 1, 0}
	axisZ = []float32{0, 0, 1}
)

func TestEnsureStoreChecksTheIndex(t *testing.T) {
	f, s := newFakeCloud(t)
	ctx := context.Background()
	if err := s.EnsureStore(ctx, 3); err != nil {
		t.Fatalf("EnsureStore is not idempotent: %v", err)
	}
	if err := s.EnsureStore(ctx, 768); err == nil || !strings.Contains(err.Error(), "3 dimensions, want 768") {
		t.Fatalf("dimension err = %v", err)
	}
	f.metric = "euclidean"
	if err := s.EnsureStore(ctx, 3); err == nil || !strings.Contains(err.Error(), "want cosine") {
		t.Fatalf("metric err = %v", err)
	}
	f.metric = "cosine"
	f.indexes = f.indexes[:2] // the sha_key index is missing
	err := s.EnsureStore(ctx, 3)
	if err == nil || !strings.Contains(err.Error(), `"sha_key"`) || !strings.Contains(err.Error(), "wrangler vectorize create-metadata-index docs --property-name=sha_key --type=string") {
		t.Fatalf("missing metadata index err = %v", err)
	}
}

func TestOperationsRequireEnsureStore(t *testing.T) {
	s, err := NewDependencies(DependenciesConfig{DB: nopDB{}, Index: nopIndex{}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := s.HybridSearch(ctx, axisX, "q", 3, nil); err == nil || !strings.Contains(err.Error(), "not initialized") {
		t.Fatalf("search err = %v", err)
	}
	if err := s.ReplaceDocument(ctx, "a.md", "sha", nil); err == nil {
		t.Fatal("ReplaceDocument before EnsureStore must fail")
	}
	if err := s.DeleteAll(ctx); err == nil || !strings.Contains(err.Error(), "not initialized") {
		t.Fatalf("reset err = %v", err)
	}
	if _, err := NewDependencies(DependenciesConfig{}); err == nil {
		t.Fatal("NewDependencies without a database and index must fail")
	}
}

func TestReplaceDocumentValidatesVersionIdentity(t *testing.T) {
	_, s := newFakeCloud(t)
	for name, tt := range map[string]struct {
		path, sha string
		chunk     indexing.IndexedChunk
		want      string
	}{
		"missing path":        {sha: "sha", want: "file path is required"},
		"missing sha":         {path: "doc.md", want: "source SHA is required"},
		"chunk path mismatch": {path: "doc.md", sha: "sha", chunk: indexing.IndexedChunk{FilePath: "other.md", SourceSHA: "sha"}, want: `does not match "doc.md"`},
		"chunk sha mismatch":  {path: "doc.md", sha: "sha", chunk: indexing.IndexedChunk{FilePath: "doc.md", SourceSHA: "other"}, want: `does not match "sha"`},
	} {
		t.Run(name, func(t *testing.T) {
			err := s.ReplaceDocument(context.Background(), tt.path, tt.sha, []indexing.IndexedChunk{tt.chunk})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func publish(t *testing.T, s *dependencies, path, sha string, chunks ...indexing.IndexedChunk) {
	t.Helper()
	if err := s.ReplaceDocument(context.Background(), path, sha, chunks); err != nil {
		t.Fatal(err)
	}
}

func TestHybridSearchFusesDenseAndKeywordLegs(t *testing.T) {
	_, s := newFakeCloud(t)
	ctx := context.Background()
	publish(t, s, "calc.md", "v1",
		chunk("calc.md", "v1", "Derivatives", "the power rule differentiates x to the n", 0, axisX),
		chunk("calc.md", "v1", "Limits", "a limit describes approach behaviour", 1, axisY),
		chunk("calc.md", "v1", "Integrals", "integration reverses differentiation", 2, axisZ),
	)

	result, err := s.HybridSearch(ctx, axisX, "power rule", 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Fused) == 0 || result.Fused[0].Header != "Derivatives" {
		t.Fatalf("fused = %+v, want the chunk both legs rank first", result.Fused)
	}
	top := result.Fused[0]
	if top.Text == "" || top.WindowText != "window: "+top.Text || top.SectionPath != "root > Derivatives" || top.FilePath != "calc.md" || top.SourceSHA != "v1" || top.LineStart != 0 {
		t.Fatalf("candidate fields were not carried through: %+v", top)
	}
	wantTop := float32(2) / float32(rrfK+1) // first in both legs
	if top.Score != wantTop {
		t.Fatalf("fused score = %v, want %v", top.Score, wantTop)
	}
	if len(result.Dense) != 3 || result.Dense[0].Header != "Derivatives" || result.Dense[0].Score < 0.99 {
		t.Fatalf("dense leg = %+v", result.Dense)
	}
	if len(result.Lexical) != 1 || result.Lexical[0].Header != "Derivatives" || result.Lexical[0].Score <= 0 {
		t.Fatalf("keyword leg = %+v, want one BM25 match with a positive score", result.Lexical)
	}
}

func TestHybridSearchOmitsComponentLegsUnlessAdaptive(t *testing.T) {
	_, s := newFakeCloud(t)
	s.adaptiveSignals = false
	publish(t, s, "a.md", "v1", chunk("a.md", "v1", "H", "alpha beta", 0, axisX))
	result, err := s.HybridSearch(context.Background(), axisX, "alpha", 3, nil)
	if err != nil || len(result.Fused) != 1 || result.Dense != nil || result.Lexical != nil {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
}

func TestQueryWithOnlyQuestionWordsUsesTheDenseLegAlone(t *testing.T) {
	_, s := newFakeCloud(t)
	publish(t, s, "a.md", "v1", chunk("a.md", "v1", "H", "what is it", 0, axisX))
	result, err := s.HybridSearch(context.Background(), axisX, "what is it", 3, nil)
	if err != nil || len(result.Fused) != 1 || len(result.Lexical) != 0 {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
}

func TestFiltersApplyToBothLegs(t *testing.T) {
	_, s := newFakeCloud(t)
	ctx := context.Background()
	long := strings.Repeat("deeply/nested/", 8) + "same-prefix-a.md" // longer than a 64-byte string index
	other := strings.Repeat("deeply/nested/", 8) + "same-prefix-b.md"
	publish(t, s, long, "sa", chunk(long, "sa", "Intro", "shared keyword zebra", 0, axisX))
	publish(t, s, other, "sb", chunk(other, "sb", "Intro", "shared keyword zebra", 0, axisX))

	for name, filter := range map[string]*search.Filter{
		"file":   {FilePath: long},
		"header": {FilePath: other, Header: "Intro"},
		"sha":    {SourceSHA: "sb"},
	} {
		result, err := s.HybridSearch(ctx, axisX, "zebra", 5, filter)
		if err != nil || len(result.Fused) != 1 {
			t.Fatalf("%s: fused = %+v, err = %v", name, result.Fused, err)
		}
		got := result.Fused[0]
		if !matchesFilter(got, filter) {
			t.Fatalf("%s: %+v escaped the filter", name, got)
		}
		if len(result.Dense) != 1 || len(result.Lexical) != 1 {
			t.Fatalf("%s: a filter must narrow both legs: dense=%d lexical=%d", name, len(result.Dense), len(result.Lexical))
		}
	}
	if result, _ := s.HybridSearch(ctx, axisX, "zebra", 5, &search.Filter{Header: "No such header"}); len(result.Fused) != 0 {
		t.Fatalf("an unmatched filter returned %+v", result.Fused)
	}
	keyword, err := s.KeywordSearch(ctx, "zebra", 5, &search.Filter{FilePath: long})
	if err != nil || len(keyword) != 1 || keyword[0].FilePath != long {
		t.Fatalf("filtered keyword search = %+v, err = %v", keyword, err)
	}
}

func TestKeywordSearchKeepsEveryTermAndRanksByBM25(t *testing.T) {
	_, s := newFakeCloud(t)
	ctx := context.Background()
	publish(t, s, "k.md", "v1",
		chunk("k.md", "v1", "A", "the secant formula", 0, axisX),
		chunk("k.md", "v1", "B", "secant secant secant slope", 1, axisY),
		chunk("k.md", "v1", "C", "unrelated tangent text", 2, axisZ),
	)
	results, err := s.KeywordSearch(ctx, "secant", 5, nil)
	if err != nil || len(results) != 2 {
		t.Fatalf("results = %+v, err = %v", results, err)
	}
	if results[0].Header != "B" || results[0].Score < results[1].Score {
		t.Fatalf("results = %+v, want the denser match first", results)
	}
	// Explicit keyword lookups keep stopwords, unlike natural-language queries.
	if res, _ := s.KeywordSearch(ctx, "the", 5, nil); len(res) != 1 || res[0].Header != "A" {
		t.Fatalf("stopword lookup = %+v", res)
	}
	if res, err := s.KeywordSearch(ctx, "  ... ", 5, nil); res != nil || err != nil {
		t.Fatalf("a punctuation-only keyword = %v, %v", res, err)
	}
	// Terms are quoted, so FTS operators in user text cannot change the query.
	if _, err := s.KeywordSearch(ctx, `secant" OR NOT (`, 5, nil); err != nil {
		t.Fatalf("FTS syntax in the keyword broke the query: %v", err)
	}
}

func TestKeywordLegIndexesTheContextualText(t *testing.T) {
	_, s := newFakeCloud(t)
	c := chunk("ctx.md", "v1", "Heading Words", "body only", 0, axisX)
	publish(t, s, "ctx.md", "v1", c)
	if res, _ := s.KeywordSearch(context.Background(), "heading", 5, nil); len(res) != 1 {
		t.Fatalf("the header must be searchable through the contextual text: %+v", res)
	}
	enriched := chunk("ctx2.md", "v1", "H", "plain body", 0, axisY)
	enriched.SparseText = "enriched uniqueterm context"
	publish(t, s, "ctx2.md", "v1", enriched)
	if res, _ := s.KeywordSearch(context.Background(), "uniqueterm", 5, nil); len(res) != 1 || res[0].FilePath != "ctx2.md" {
		t.Fatalf("explicit sparse text must be indexed: %+v", res)
	}
}

func TestReplacementSwitchesVersionsAndCleansUp(t *testing.T) {
	f, s := newFakeCloud(t)
	ctx := context.Background()
	publish(t, s, "doc.md", "v1", chunk("doc.md", "v1", "Old", "oldcontent here", 0, axisX), chunk("doc.md", "v1", "Old2", "oldcontent more", 1, axisY))
	publish(t, s, "doc.md", "v2", chunk("doc.md", "v2", "New", "newcontent here", 0, axisZ))

	if res, _ := s.KeywordSearch(ctx, "oldcontent", 5, nil); len(res) != 0 {
		t.Fatalf("the retired version is still searchable: %+v", res)
	}
	if res, _ := s.KeywordSearch(ctx, "newcontent", 5, nil); len(res) != 1 {
		t.Fatalf("the new version is not searchable: %+v", res)
	}
	dense, _ := s.HybridSearch(ctx, axisX, "newcontent", 5, nil)
	for _, c := range dense.Dense {
		if c.SourceSHA != "v2" {
			t.Fatalf("dense leg returned a retired chunk: %+v", c)
		}
	}
	if n := f.count(t, `SELECT COUNT(*) FROM doc_chunks`); n != 1 {
		t.Fatalf("%d chunk rows remain, want only the new version's 1", n)
	}
	if n := f.count(t, `SELECT COUNT(*) FROM doc_fts`); n != 1 {
		t.Fatalf("%d keyword rows remain, want 1", n)
	}
	if n := f.count(t, `SELECT COUNT(*) FROM doc_versions`); n != 1 {
		t.Fatalf("%d manifest rows remain, want 1", n)
	}
	if f.vectorCount() != 1 {
		t.Fatalf("%d vectors remain, want 1", f.vectorCount())
	}
	shas, err := s.GetAllFileSHAs(ctx)
	if err != nil || len(shas) != 1 || shas["doc.md"] != "v2" {
		t.Fatalf("shas = %v, err = %v", shas, err)
	}
}

func TestFailedActivationKeepsThePreviousVersionVisible(t *testing.T) {
	f, s := newFakeCloud(t)
	ctx := context.Background()
	publish(t, s, "doc.md", "v1", chunk("doc.md", "v1", "Old", "oldcontent", 0, axisX))
	f.failSQL = "UPDATE doc_versions SET active = 1"
	err := s.ReplaceDocument(ctx, "doc.md", "v2", []indexing.IndexedChunk{chunk("doc.md", "v2", "New", "newcontent", 0, axisY)})
	f.failSQL = ""
	if err == nil || indexing.WasPublished(err) {
		t.Fatalf("err = %v, want an unpublished failure", err)
	}
	if res, _ := s.KeywordSearch(ctx, "oldcontent", 5, nil); len(res) != 1 {
		t.Fatal("the previous version must stay visible after a failed activation")
	}
	if res, _ := s.KeywordSearch(ctx, "newcontent", 5, nil); len(res) != 0 {
		t.Fatalf("a staged version must stay invisible: %+v", res)
	}
	if dense, _ := s.HybridSearch(ctx, axisY, "x", 5, nil); len(dense.Dense) != 1 || dense.Dense[0].SourceSHA != "v1" {
		t.Fatalf("a staged vector leaked into dense results: %+v", dense.Dense)
	}

	// The retry stages again and publishes.
	publish(t, s, "doc.md", "v2", chunk("doc.md", "v2", "New", "newcontent", 0, axisY))
	if res, _ := s.KeywordSearch(ctx, "newcontent", 5, nil); len(res) != 1 {
		t.Fatal("the retry did not publish the new version")
	}
}

func TestCleanupFailureAfterActivationIsAPublicationError(t *testing.T) {
	f, s := newFakeCloud(t)
	ctx := context.Background()
	publish(t, s, "doc.md", "v1", chunk("doc.md", "v1", "Old", "oldcontent", 0, axisX))

	f.failVecPath = "/delete_by_ids"
	err := s.ReplaceDocument(ctx, "doc.md", "v2", []indexing.IndexedChunk{chunk("doc.md", "v2", "New", "newcontent", 0, axisY)})
	f.failVecPath = ""
	if !indexing.WasPublished(err) {
		t.Fatalf("err = %v, want a PublicationError: the new version is already active", err)
	}
	if res, _ := s.KeywordSearch(ctx, "newcontent", 5, nil); len(res) != 1 {
		t.Fatal("the new version must be visible")
	}
	if res, _ := s.KeywordSearch(ctx, "oldcontent", 5, nil); len(res) != 0 {
		t.Fatal("the retired version must be hidden even though cleanup failed")
	}

	// A retry of the same version cleans up without restaging.
	before := f.upsertBatches
	if err := s.ReplaceDocument(ctx, "doc.md", "v2", []indexing.IndexedChunk{chunk("doc.md", "v2", "New", "newcontent", 0, axisY)}); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if f.upsertBatches != before {
		t.Fatal("a published version must never be restaged")
	}
	if n := f.count(t, `SELECT COUNT(*) FROM doc_chunks`); n != 1 || f.vectorCount() != 1 {
		t.Fatalf("after the retry %d chunk rows, %d vectors remain, want 1 each", n, f.vectorCount())
	}
}

func TestPublishedVersionIsImmutable(t *testing.T) {
	_, s := newFakeCloud(t)
	ctx := context.Background()
	publish(t, s, "doc.md", "v1", chunk("doc.md", "v1", "H", "one", 0, axisX), chunk("doc.md", "v1", "H", "two", 1, axisY))
	err := s.ReplaceDocument(ctx, "doc.md", "v1", []indexing.IndexedChunk{chunk("doc.md", "v1", "H", "one", 0, axisX)})
	if err == nil || !strings.Contains(err.Error(), "contains 2 chunks, expected 1") {
		t.Fatalf("err = %v", err)
	}
}

func TestEmptyReplacementPublishesAbsence(t *testing.T) {
	f, s := newFakeCloud(t)
	ctx := context.Background()
	publish(t, s, "doc.md", "v1", chunk("doc.md", "v1", "H", "content", 0, axisX))
	publish(t, s, "doc.md", "v2") // the new source is empty
	if res, _ := s.KeywordSearch(ctx, "content", 5, nil); len(res) != 0 {
		t.Fatalf("empty content must publish absence: %+v", res)
	}
	if shas, _ := s.GetAllFileSHAs(ctx); len(shas) != 0 {
		t.Fatalf("shas = %v", shas)
	}
	if n := f.count(t, `SELECT COUNT(*) FROM doc_chunks`); n != 0 || f.vectorCount() != 0 {
		t.Fatalf("%d rows, %d vectors left after publishing absence", n, f.vectorCount())
	}
}

func TestDeleteDocument(t *testing.T) {
	f, s := newFakeCloud(t)
	ctx := context.Background()
	publish(t, s, "gone.md", "v1", chunk("gone.md", "v1", "H", "goneword", 0, axisX))
	publish(t, s, "kept.md", "v1", chunk("kept.md", "v1", "H", "keptword", 0, axisY))
	if err := s.DeleteDocument(ctx, "gone.md"); err != nil {
		t.Fatal(err)
	}
	if res, _ := s.KeywordSearch(ctx, "goneword", 5, nil); len(res) != 0 {
		t.Fatal("a deleted document is still searchable")
	}
	if res, _ := s.KeywordSearch(ctx, "keptword", 5, nil); len(res) != 1 {
		t.Fatal("deleting one document removed another")
	}
	if f.vectorCount() != 1 || f.count(t, `SELECT COUNT(*) FROM doc_chunks`) != 1 || f.count(t, `SELECT COUNT(*) FROM doc_fts`) != 1 {
		t.Fatal("deletion left vectors or rows behind")
	}
	if err := s.DeleteDocument(ctx, "never-existed.md"); err != nil {
		t.Fatalf("deleting a missing document must be a no-op: %v", err)
	}
	if err := s.DeleteDocument(ctx, " "); err == nil {
		t.Fatal("an empty path must be rejected")
	}
}

func TestResetPublishesAnEmptyGenerationAndCleansTheOldOne(t *testing.T) {
	f, s := newFakeCloud(t)
	ctx := context.Background()
	publish(t, s, "a.md", "v1", chunk("a.md", "v1", "H", "before reset", 0, axisX))
	if err := s.DeleteAll(ctx); err != nil {
		t.Fatal(err)
	}
	if res, _ := s.KeywordSearch(ctx, "reset", 5, nil); len(res) != 0 {
		t.Fatal("the old corpus is searchable after reset")
	}
	if dense, _ := s.HybridSearch(ctx, axisX, "x", 5, nil); len(dense.Fused) != 0 {
		t.Fatalf("dense results after reset: %+v", dense.Fused)
	}
	if shas, _ := s.GetAllFileSHAs(ctx); len(shas) != 0 {
		t.Fatalf("shas after reset = %v", shas)
	}
	if f.vectorCount() != 0 || f.count(t, `SELECT COUNT(*) FROM doc_chunks`) != 0 || f.count(t, `SELECT COUNT(*) FROM doc_versions`) != 0 {
		t.Fatal("the retired generation was not cleaned")
	}

	// The same version can be published again into the new generation.
	publish(t, s, "a.md", "v1", chunk("a.md", "v1", "H", "after reset", 0, axisX))
	if res, _ := s.KeywordSearch(ctx, "after", 5, nil); len(res) != 1 {
		t.Fatalf("re-ingest after reset: %+v", res)
	}
	if f.count(t, `SELECT generation FROM doc_state`) != 2 {
		t.Fatal("reset must advance the generation")
	}
}

func TestResetCleanupFailureIsRetryable(t *testing.T) {
	f, s := newFakeCloud(t)
	ctx := context.Background()
	publish(t, s, "a.md", "v1", chunk("a.md", "v1", "H", "before reset", 0, axisX))

	f.failVecPath = "/delete_by_ids"
	err := s.DeleteAll(ctx)
	f.failVecPath = ""
	if !indexing.WasPublished(err) {
		t.Fatalf("err = %v, want a PublicationError: the new generation is already published", err)
	}
	if res, _ := s.KeywordSearch(ctx, "reset", 5, nil); len(res) != 0 {
		t.Fatal("the old corpus must be hidden once the new generation is published")
	}
	if err := s.DeleteAll(ctx); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if f.vectorCount() != 0 || f.count(t, `SELECT COUNT(*) FROM doc_chunks`) != 0 {
		t.Fatal("the retry did not finish cleanup")
	}
}

func TestFailedResetPublishLeavesTheCorpusSearchable(t *testing.T) {
	f, s := newFakeCloud(t)
	ctx := context.Background()
	publish(t, s, "a.md", "v1", chunk("a.md", "v1", "H", "stillhere", 0, axisX))
	f.failSQL = "UPDATE doc_state"
	err := s.DeleteAll(ctx)
	f.failSQL = ""
	if err == nil || indexing.WasPublished(err) {
		t.Fatalf("err = %v, want an unpublished failure", err)
	}
	if res, _ := s.KeywordSearch(ctx, "stillhere", 5, nil); len(res) != 1 {
		t.Fatal("a failed reset must leave the previous corpus addressable")
	}
}

func TestDenseLegHonorsVectorizeTopKCap(t *testing.T) {
	_, s := newFakeCloud(t)
	var chunks []indexing.IndexedChunk
	for i := 0; i < 5; i++ {
		chunks = append(chunks, chunk("big.md", "v1", "H", fmt.Sprintf("word%d", i), i, []float32{1, float32(i) * 0.1, 0}))
	}
	publish(t, s, "big.md", "v1", chunks...)
	// topK 50 * prefetch 5 = 250 would exceed Vectorize's cap; the fake rejects it.
	result, err := s.HybridSearch(context.Background(), axisX, "word0", 50, nil)
	if err != nil || len(result.Dense) != 5 {
		t.Fatalf("dense = %d, err = %v", len(result.Dense), err)
	}
}

func TestLargeDocumentsStageInBatches(t *testing.T) {
	f, s := newFakeCloud(t)
	var chunks []indexing.IndexedChunk
	for i := 0; i < vectorsPerBatch+30; i++ {
		chunks = append(chunks, chunk("long.md", "v1", "H", fmt.Sprintf("term%d", i), i, axisX))
	}
	publish(t, s, "long.md", "v1", chunks...)
	if f.upsertBatches != 2 {
		t.Fatalf("%d vector batches, want 2", f.upsertBatches)
	}
	if n := f.count(t, `SELECT COUNT(*) FROM doc_chunks`); n != len(chunks) || f.vectorCount() != len(chunks) {
		t.Fatalf("%d rows, %d vectors, want %d", n, f.vectorCount(), len(chunks))
	}
}

func TestReciprocalRankFusion(t *testing.T) {
	a := search.SearchCandidate{FilePath: "a", ChunkIndex: 0}
	b := search.SearchCandidate{FilePath: "b", ChunkIndex: 0}
	c := search.SearchCandidate{FilePath: "c", ChunkIndex: 0}
	got := reciprocalRankFusion(2, []search.SearchCandidate{a, b}, []search.SearchCandidate{c, b})
	if len(got) != 2 || got[0].FilePath != "b" || got[1].FilePath != "a" {
		t.Fatalf("fused = %+v, want b (in both legs) then a (first seen)", got)
	}
	want := float32(1.0/float64(rrfK+2) + 1.0/float64(rrfK+2))
	if got[0].Score != want {
		t.Fatalf("score = %v, want %v", got[0].Score, want)
	}
	if out := reciprocalRankFusion(5); len(out) != 0 {
		t.Fatalf("no lists = %+v", out)
	}
}

func TestErrorsAreWrapped(t *testing.T) {
	f, s := newFakeCloud(t)
	f.failVecPath = "/query"
	_, err := s.HybridSearch(context.Background(), axisX, "word", 3, nil)
	if err == nil || !strings.Contains(err.Error(), "dense leg") || !strings.Contains(err.Error(), "vectorize down") {
		t.Fatalf("dense err = %v", err)
	}
	f.failVecPath = ""
	f.failSQL = "FROM doc_fts"
	_, err = s.HybridSearch(context.Background(), axisX, "word", 3, nil)
	if err == nil || !strings.Contains(err.Error(), "keyword leg") {
		t.Fatalf("keyword err = %v", err)
	}
	if !errors.Is(err, err) {
		t.Fatal("unreachable")
	}
}
