package eval

import (
	"context"
	"fmt"
	"io"
	"text/tabwriter"

	"nadir/internal/store"
)

// Granularity controls the scoring unit.
type Granularity int

const (
	// FileLevel deduplicates chunks by file path; the ranked list is files.
	// Appropriate for "did the right document surface?" evaluation.
	FileLevel Granularity = iota
	// ChunkLevel keeps every chunk as a separate item; the ranked list is chunks.
	// Appropriate for paper-comparable retrieval numbers (BEIR/MTEB score at passage level).
	// Relevance is still file-level from the golden set: any chunk from a relevant file counts.
	ChunkLevel
)

// Retriever is the minimal search surface the runner needs.
// *search.Service satisfies this.
type Retriever interface {
	Search(ctx context.Context, query string, topK int, filter *store.SearchFilter) ([]store.ScoredChunk, error)
}

// Runner evaluates a Retriever against a GoldenSet.
type Runner struct {
	Searcher    Retriever
	TopK        int
	Granularity Granularity
}

// Run executes every golden query and returns an aggregated Report.
// fetchK controls how many candidates to pull from the retriever per query;
// it should be >= the largest k you score at (e.g. 10) so Recall@10 is meaningful.
func (r *Runner) Run(ctx context.Context, gs *GoldenSet, fetchK int) (Report, error) {
	if fetchK <= 0 {
		fetchK = 10
	}
	retrievedPerQuery := make([][]string, len(gs.Queries))
	gradedPerQuery := make([]map[string]int, len(gs.Queries))
	queries := make([]string, len(gs.Queries))

	for i, gq := range gs.Queries {
		queries[i] = gq.Query
		graded := gq.GradedRelevance()
		gradedPerQuery[i] = graded

		chunks, err := r.Searcher.Search(ctx, gq.Query, fetchK, nil)
		if err != nil {
			return Report{}, fmt.Errorf("eval: query %d %q: %w", i, gq.Query, err)
		}
		retrievedPerQuery[i] = r.rankItems(chunks, graded)
	}

	return Aggregate(retrievedPerQuery, gradedPerQuery, queries), nil
}

// rankItems converts retrieved chunks into a ranked list of identifiers,
// either at file level (deduped) or chunk level (all chunks), mapping each
// to its expected-form identifier via MatchFile so it joins the graded set.
func (r *Runner) rankItems(chunks []store.ScoredChunk, graded map[string]int) []string {
	switch r.Granularity {
	case ChunkLevel:
		return rankChunks(chunks, graded)
	default:
		return dedupRankedFiles(chunks, graded)
	}
}

func dedupRankedFiles(chunks []store.ScoredChunk, graded map[string]int) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(chunks))
	for _, c := range chunks {
		id := mapToExpected(c.FilePath, graded)
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func rankChunks(chunks []store.ScoredChunk, graded map[string]int) []string {
	out := make([]string, 0, len(chunks))
	for _, c := range chunks {
		id := mapToExpected(c.FilePath, graded)
		out = append(out, id)
	}
	return out
}

func mapToExpected(retrievedPath string, graded map[string]int) string {
	for e := range graded {
		if MatchFile(retrievedPath, e) {
			return e
		}
	}
	return retrievedPath
}

// PrintReport writes a human-readable summary + per-query table to w.
func PrintReport(w io.Writer, rep Report) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "Queries:\t%d\n", rep.NumQueries)
	fmt.Fprintf(tw, "Recall@5:\t%.4f\t[%.4f, %.4f]\n", rep.RecallAt5, rep.RecallAt5CI[0], rep.RecallAt5CI[1])
	fmt.Fprintf(tw, "Recall@10:\t%.4f\t[%.4f, %.4f]\n", rep.RecallAt10, rep.RecallAt10CI[0], rep.RecallAt10CI[1])
	fmt.Fprintf(tw, "Precision@5:\t%.4f\n", rep.PrecisionAt5)
	fmt.Fprintf(tw, "NDCG@10:\t%.4f\t[%.4f, %.4f]\n", rep.NDCG10, rep.NDCG10CI[0], rep.NDCG10CI[1])
	fmt.Fprintf(tw, "NDCG@10 (exp):\t%.4f\n", rep.NDCG10Exp)
	fmt.Fprintf(tw, "MAP:\t%.4f\t[%.4f, %.4f]\n", rep.MAP, rep.MAPCI[0], rep.MAPCI[1])
	fmt.Fprintf(tw, "MRR:\t%.4f\n", rep.MRR)
	fmt.Fprintf(tw, "Success@5:\t%.4f\n", rep.SuccessAt5)
	tw.Flush()

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Per-query:")
	tw = tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "QUERY\tNDCG@10\tMAP\tRR\tR@5\tR@10\tRETRIEVED (top 10)")
	for _, q := range rep.PerQuery {
		top := q.Retrieved
		if len(top) > 10 {
			top = top[:10]
		}
		fmt.Fprintf(tw, "%s\t%.3f\t%.3f\t%.3f\t%.3f\t%.3f\t%v\n",
			truncate(q.Query, 40), q.NDCG10, q.AP, q.RR, q.RecallAt5, q.RecallAt10, top)
	}
	tw.Flush()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
