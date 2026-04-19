# Search Eval: Guide, Formulas, Goals

## Quick Start — Pick Your Target

```
Want fast daily feedback?           → make eval-tf
Want to compare TF vs SPLADE?       → make splade  (separate terminal)
                                      make eval-splade
Want self-contained CI run?         → make eval-fresh
Want LLM as judge instead of qrels? → make eval-llm
```

### Prerequisites by target

| Target | Prereqs | Qdrant backend | Ingest | Time |
|--------|---------|----------------|--------|------|
| `eval-tf` | `make up && make ingest` | live (existing) | skipped | ~30s |
| `eval-splade` | `make splade` + `make up && make ingest` | live (existing) | skipped | ~30s |
| `eval-fresh` | none | ephemeral container | full re-ingest | ~5-10 min |
| `eval-llm` | `make up && make ingest` | live (existing) | skipped | ~5 min + LLM cost |

### Why two backends?

**Live (`EVAL_STORE=live`, default):** connects to already-running Qdrant (from `make up`). Skips
ingest — assumes data already there from `make ingest`. Fast. Use for iteration.

**Container (`EVAL_STORE=container`):** spins up fresh ephemeral Qdrant via testcontainers, runs
full ingest inside the test, tears down after. Slow but self-contained. Required for CI or when
you need SPLADE vectors stored (live ingest uses HTTP server which uses config-driven scorer;
container ingest runs with whatever profile the test loop has active).

### Why SPLADE needs container mode for a true comparison

`make ingest` (HTTP server path) stores TF sparse vectors. If you run eval-splade against live
Qdrant, the BM25 leg searches over TF vectors even though the scorer is SPLADE — mismatched.
`eval-fresh` re-ingests with SPLADE sidecar active → sparse vectors in Qdrant are SPLADE vectors →
apples-to-apples comparison.

---

## Environment Variables

| Variable | Default | Values | Purpose |
|----------|---------|--------|---------|
| `EVAL_STORE` | `live` | `live` \| `container` | Qdrant backend |
| `EVAL_JUDGE` | `qrels` | `qrels` \| `llm` | Relevance judge |
| `EVAL_QDRANT_ADDR` | from config.yaml | any gRPC addr | Override Qdrant addr |
| `EVAL_QDRANT_COLLECTION` | from config.yaml | any string | Override collection |
| `EVAL_QRELS_PATH` | `testdata/qrels.jsonl` | any path | Override qrels file |
| `EVAL_LLM_BASE_URL` | from config.yaml | OpenAI-compat URL | LLM judge endpoint |
| `EVAL_LLM_MODEL` | from config.yaml | any model name | LLM judge model |
| `EVAL_LLM_API_KEY` | — | any string | LLM judge API key |

---

## Eval Profiles (`testdata/eval_profiles.jsonl`)

Each profile is one row: `name`, `sparse_scorer` (`tf`\|`splade`), `chunk_size`, `chunk_overlap`.
`TestSearchEval` runs one sub-test per profile. Add profiles to compare chunker configs.

---

## Judges

### qrels judge (gold, default)

Loads `testdata/qrels.jsonl`. Pre-labeled ground truth. Fast, deterministic, reproducible.
Regenerate after major index changes: `make eval-tf` runs `gen-qrels` first automatically.

Format: `{"query": "...", "chunk_id": "filepath:linestart", "file_path": "...", "relevant": true}`

### LLM judge (silver)

Calls OpenAI-compatible endpoint per (query, chunk) pair. No pre-built ground truth needed.
Flexible but slow and costs tokens. Good for exploring new query sets before committing qrels.

---

## Metrics

### MRR@K — Mean Reciprocal Rank

Where does the **first** relevant result appear? Higher = relevant doc appears earlier.

```
MRR@K = (1/|Q|) * Σ_{q∈Q} (1 / rank_q)
```

- `rank_q` = position (1-indexed) of first relevant result for query q
- No relevant result in top-K → contribute 0
- Range: [0, 1]. Perfect = 1.0

---

### HitRate@K (Success@K)

Fraction of queries with **at least one** relevant result in top-K.

```
HitRate@K = (1/|Q|) * Σ_{q∈Q} 1[∃ relevant doc in top-K for q]
```

- Binary per query: 1 if any hit, 0 otherwise
- Not true IR Recall@K (which requires total relevant count per query in corpus)
- Appropriate for RAG: generator needs ≥1 good chunk in context

---

### NDCG@K — Normalized Discounted Cumulative Gain

Weights rank position — rank 1 hit worth more than rank 5.

```
DCG@K  = Σ_{i=1}^{K} rel_i / log2(i+1)
IDCG@K = DCG of perfect ranking
NDCG@K = DCG@K / IDCG@K
```

Binary relevance (0 or 1). IDCG uses `min(relevantCount, K)` hits at top positions.

---

### Precision@K

Fraction of top-K results that are relevant.

```
P@K = |{relevant docs in top-K}| / K
```

High recall + low precision = noisy results.

---

## Target Thresholds

Based on BEIR benchmark baselines for BM25 + dense hybrid retrieval.

| Metric | Minimum | Good | Excellent |
|--------|---------|------|-----------|
| MRR@5 | 0.50 | 0.70 | 0.85+ |
| HitRate@5 | 0.70 | 0.85 | 0.95+ |
| NDCG@5 | 0.50 | 0.70 | 0.85+ |
| Precision@5 | 0.30 | 0.50 | 0.70+ |

---

## References

- Voorhees 1999 — MRR introduced for TREC QA track
- Thakur et al. 2021 — [BEIR: Heterogeneous Benchmark for IR](https://arxiv.org/abs/2104.08663) — uses nDCG@10
- Es et al. 2023 — [RAGAS: Automated Evaluation of RAG](https://arxiv.org/abs/2309.15217) — context recall, faithfulness, answer relevancy
- MS MARCO leaderboard — uses MRR@10
