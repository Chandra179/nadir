# Search Eval: Approach, Formulas, Goals

## What We Measure

Two standard IR metrics evaluated at K=5 (topK=5).

---

## Metrics

### MRR@K — Mean Reciprocal Rank

Measures where the **first** relevant result appears. Higher = relevant doc appears earlier.

```
MRR@K = (1/|Q|) * Σ_{q∈Q} (1 / rank_q)
```

- `rank_q` = position (1-indexed) of first relevant result for query q
- If no relevant result in top-K: contribute 0
- Range: [0, 1]. Perfect = 1.0 (every query has relevant doc at rank 1)

**Implementation** (`eval_search_test.go:199-201`):
```go
if firstRank > 0 {
    mrr5 += 1.0 / float64(firstRank)
}
// ...
mrr5 /= n
```
Correct.

---

### Recall@K

Measures what fraction of queries have **at least one** relevant result in top-K.

```
Recall@K = (1/|Q|) * Σ_{q∈Q} 1[∃ relevant doc in top-K results for q]
```

- Binary per query: 1 if any relevant hit exists, 0 otherwise
- Range: [0, 1]. Perfect = 1.0 (every query gets at least one hit)

**Implementation** (`eval_search_test.go:202-205`):
```go
if anyRelevant {
    recall5++
}
// ...
recall5 /= n
```
Correct.

---

## Relevance Definition

Pluggable via `RelevanceJudge` interface (`eval_judge.go`):

```go
type RelevanceJudge interface {
    IsRelevant(ctx context.Context, query string, chunk ScoredChunk) (bool, error)
}
```

| Implementation | File | Accuracy | Cost |
|----------------|------|----------|------|
| `FolderJudge` | `eval_judge_folder.go` | Coarse | Free |
| `LLMJudge` | `eval_judge_llm.go` | Silver | ~$0.01/pair |
| `qrelsJudge` | `eval_search_test.go` | Gold | One-time |

**FolderJudge** (default): folder-level substring match on `FilePath`.
```
"golang" ∈ filePath → relevant
```
Categories: `golang`, `system-design`, `fundamental`, `ml`

**LLMJudge**: calls OpenAI-compatible endpoint. Swap backend via `EVAL_LLM_BASE_URL` + `EVAL_LLM_MODEL`.

**qrelsJudge**: loads pre-labeled `testdata/qrels.jsonl`. Falls back to `FolderJudge` for unlabeled queries.
Format: `{"query": "...", "chunk_id": "filepath:linestart", "file_path": "...", "relevant": true}`

Select judge via `EVAL_JUDGE` env var: `folder` (default) | `llm` | `qrels`.

---

## Baseline Results

| Date | Embedder | Dims | Chunker | Chunk size/overlap | Retrieval | Judge | MRR@5 | Recall@5 | NDCG@5 | Precision@5 |
|------|----------|------|---------|-------------------|-----------|-------|-------|----------|--------|-------------|
| 2026-04-19 | nomic-embed-text | 768 | RecursiveChunker | 512/64 | HybridSearch (dense+BM25 RRF) | folder | 0.7750 | 0.8000 | 0.7587 | 0.5200 |
| 2026-04-19 | nomic-embed-text | 768 | RecursiveChunker+lists/blockquotes | 512/64 | HybridSearch (dense+client-side BM25 RRF k=60) + contextual enrichment | qrels | 0.5625 | 0.7500 | 0.5974 | 0.3500 |
| 2026-04-19 | nomic-embed-text | 768 | RecursiveChunker+lists/blockquotes | 512/64 | HybridSearch (dense+client-side BM25 RRF k=60) + contextual enrichment | llm | 0.6083 | 0.7000 | 0.6164 | 0.3400 |

## Target Thresholds

| Metric | Minimum | Good | Excellent |
|--------|---------|------|-----------|
| MRR@5  | 0.50    | 0.70 | 0.85+     |
| Recall@5 | 0.70  | 0.85 | 0.95+     |
| NDCG@5 | 0.50    | 0.70 | 0.85+     |
| Precision@5 | 0.30 | 0.50 | 0.70+  |

Based on BEIR benchmark baselines for BM25 + dense hybrid retrieval.

---

## NDCG@K — Normalized Discounted Cumulative Gain

Weights rank position — rank 1 hit worth more than rank 5. **Implemented.**

```
DCG@K  = Σ_{i=1}^{K} rel_i / log2(i+1)   (i is 1-indexed, so denominator = log2(rank+1))
IDCG@K = DCG of perfect ranking (all relevant docs at top positions)
NDCG@K = DCG@K / IDCG@K
```

Binary relevance (0 or 1). IDCG uses `min(relevantCount, K)` hits placed at ranks 1..N since total corpus relevance unknown.

**Implementation** (`eval_search_test.go`):
```go
dcg += 1.0 / math.Log2(float64(rank+2))  // rank is 0-indexed → rank+2 = i+1 where i=rank+1

idcg := 0.0
for i := 0; i < relevantCount && i < topK; i++ {
    idcg += 1.0 / math.Log2(float64(i+2))
}
if idcg > 0 {
    ndcg5 += dcg / idcg
}
```
Correct.

---

## Precision@K

Fraction of top-K results that are relevant. **Implemented.**

```
P@K = |{relevant docs in top-K}| / K
```

High recall + low precision = noisy results (many irrelevant docs in top-K).

**Implementation** (`eval_search_test.go`):
```go
precision5 += float64(relevantCount) / float64(topK)
// ...
precision5 /= n
```
Correct.

---

## Eval Set

20 queries across 4 topic domains (5 each):

| Domain | Queries |
|--------|---------|
| `golang` | GPM scheduler, goroutine leaks, channels, loop scoping, strings.Builder |
| `system-design` | consistent hashing, Snowflake ID, rate limiting, cache stampede, virtual nodes |
| `fundamental` | B-Tree vs LSM, ACID isolation, N+1 problem, Kafka routing, CPU fetch-execute |
| `ml` | gradient descent, activation functions, dropout, MoE, transformer attention |

---

## References

- Voorhees 1999 — MRR introduced for TREC QA track
- Thakur et al. 2021 — [BEIR: Heterogeneous Benchmark for IR](https://arxiv.org/abs/2104.08663) — uses nDCG@10
- Es et al. 2023 — [RAGAS: Automated Evaluation of RAG](https://arxiv.org/abs/2309.15217) — context recall, faithfulness, answer relevancy
- MS MARCO leaderboard — uses MRR@10
