# TODO

Open work, ordered by what it unblocks. The first items exist because the
current quality and performance numbers are too small to support decisions;
see [ADR 0037](docs/adr/0037-early-sources-and-single-query-embedding.md) for
what the latest measurements did and did not show.

## Measurement credibility

1. **Retrieval golden set (reviewed; first live results).** `eval/golden/retrieval.json`
   holds 57 items (25 fact, 11 multi-chunk, 8 distractor, 6 multi-file, 7
   unanswerable), all marked reviewed on 2026-10-08, so the 50 answerable items
   pass the 30-item gate. `python -m eval retrieval` scores Hit@k, Recall@k and
   MRR with no model involved. The author drafted the items; the reviewer
   accepted them without edits, so questions in the reviewer's own words are
   still wanted. Results on the 13-document `eval/samples` corpus (top_k 10,
   reports under `.local/evaluation/retrieval-*`):

   | Setting | Hit@1 | Hit@3 | Recall@10 | MRR | mean results | mean files |
   | --- | --- | --- | --- | --- | --- | --- |
   | `capOverfetchMul` 3, cap 3 (current) | 0.78 | 0.94 | 0.81 | 0.847 | 8.6 | 4.3 |
   | `capOverfetchMul` 1, cap 3 | 0.78 | 0.94 | 0.81 | 0.843 | 5.2 | 2.5 |
   | `capOverfetchMul` 3, cap 5 | 0.78 | 0.94 | 0.86 | 0.852 | 9.4 | 3.7 |

   The 3× overfetch did not change ranking quality on this set (MRR differs by
   0.004); what it changes is how many results come back, 8.6 of 10 instead of
   5.2, and from how many files. Facts reach Hit@3 = 1.00. The weak spots are
   the multi-chunk items (Recall@10 0.43, which a cap of 3 cannot exceed 0.75
   for four-passage items) and distractors (Hit@10 0.75; for example
   `distractor-01` finds the right file at rank 1 but not the RRF passage).
   The unanswerable items still return chunks with top scores up to 0.83, so
   there is no score threshold to refuse on yet. A first matcher bug (the index
   stores Markdown-stripped text, so backticked snippets never matched) was fixed
   before these numbers. One run per setting; repeat before treating differences
   under about 0.02 as real. The old 133-query `golden.json` from commit
   `31c84e4` was deliberately not recovered: it covers a different four-document
   math corpus that is no longer in the repository, and its labels came from two
   synthetic evaluator passes, not human review. Its `{file, contains}` labelling
   is the same text-anchored design the new set uses.
   **Benchmark rerun (2026-10-08, 13-document corpus, Qwen 3.5 4B, 60 s per
   level after a discarded 20 s warm-up, reports under `.local/benchmark/session-*`).**
   Median/p95 latency by concurrent users (1 / 2 / 4):

   | Workload | Median ms | p95 ms | Workflows/s |
   | --- | --- | --- | --- |
   | retrieval | 100 / 110 / 130 | 110 / 120 / 170 | 0.87 / 1.8 / 3.5 |
   | cache (seed + hit) | 200 / 200 / 230 | 220 / 230 / 270 | 0.80 / 1.6 / 3.2 |
   | chat (full answer) | 8.1 s / 8.1 s / 11 s | 8.1 s / 9.5 s / 13 s | 0.12 / 0.22 / 0.33 |
   | chat time to first token | 6.9 s / 6.9 s / 9.5 s | 6.9 s / 8.3 s / 12 s | |
   | follow-up with rewrite | 15 s / 15 s / 17 s | 15 s / 15 s / 22 s | |

   Retrieval and cache scale linearly to 4 users. Chat does not: throughput
   rises only 2.75× for 4× the users and time to first token grows 38%,
   so generation is the bottleneck. Time to first token is about 85% of the
   chat latency; the answer streams in roughly 1-2 s once it starts. Zero HTTP
   failures. Two retrieval workflows at 4 users were cut off by the run timer.
   The follow-up workload needs a follow-up that depends on the seed ("What
   happens if it times out?"); a self-contained question is not rewritten and the
   workload correctly reports that as a failure. Not yet measured: a cold-model
   request (the smoke test showed about 4.4 s answer-model load) and larger
   corpora.
   **Ragas rerun (2026-10-09).** `eval/golden/ragas-questions.json` (33 fact and
   distractor questions from the golden set, references written by the author and
   not yet human-reviewed), answered by Qwen 3.5 4B and judged by
   `llama3.1:8b-instruct-q4_K_M` (50 minutes to score; details in `docs/evaluation-log.md`).
   Faithfulness 0.74, factual correctness 0.63, context precision 0.96, context
   recall 0.90 (6 of 33 failed with `IncompleteOutputException`, a judge
   `max_tokens` limit, so the run status is `failed`). Spot checks found correct
   answers scored 0 (fact-07, fact-08, fact-21, fact-23), so the 8B judge is not
   reliable for faithfulness or factual correctness; it likely penalises the `[1]`
   citation marker and one-line answers. Next: review the references, try another
   judge or `--judge-reasoning-effort`, raise the judge output limit, and rate
   about 20 answers by hand to calibrate.
2. **Reviewed Ragas question set (40–60).** Review `eval/samples` drafts against
   their passages (the October 7 phi4-mini draft has garbled questions,
   duplicates and a hallucinated reference) or author questions directly. Cover
   single-hop, multi-hop, abstention and follow-ups; do not rely on questions
   about Nadir's own documentation.
3. **Pre-registered answer-model gate for `qwen3.5:4b`.** `gemma3:4b` passed
   (faithfulness 0.881, relevancy 0.803, 133 queries, ADR 0034); the default
   moved to Qwen on October 6 with only a 3-question run. Run the same gate with
   a judge that is not the answer model, three repetitions, and decide.
4. **Judge calibration.** Rate about 30 samples by hand and compare with the
   judge's scores before trusting any absolute value.

## Retrieval policy decisions (need the sets above)

5. **`search.max_chunks_per_file`.** Compare 3 against 5 and unlimited at
   `top_k` 5 and 8 now that the cap backfills (ADR 0037).
6. **Semantic-cache threshold.** `semantic_cache.threshold: 0.90` has never been
   tested: the evaluator always bypasses the cache. Replay paraphrase pairs
   (should hit) and near-miss pairs with different answers (must not hit) through
   the embedder alone and pick the threshold from the false-hit rate.
7. **Adaptive reranking.** Reranking regressed retrieval on EmbeddingGemma
   (ADR 0034). Set the margin from the golden set before any re-enablement.

## Performance evidence

8. **Repeat the Locust workloads with a representative corpus.** The October 6
   runs used two documents, one user and 1–8 samples per workload, so their p95
   is the maximum. Use the 13 `eval/samples` documents, 1/2/4 users, 5–10
   minutes per workload, and discard a warm-up period. Read
   `runs[].summary.server_operations` to separate retrieval, model load, prefill
   and decode; check whether `ollama.embed.load` is non-zero after rewriter or
   generator calls, which indicates models evicting each other on the 6 GB GPU.
9. **Cold-model case.** Measure one request after more than the 5-minute
   `keep_alive` idle separately from warm requests.

## Housekeeping

10. Re-baseline the README and overview tables after items 1–3 and 8.
