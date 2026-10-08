# Evaluation log

Dated records of evaluation sessions: what ran, how long it took, the results and
any problems. Commands and report formats live in the [evaluator guide](../eval/README.md);
the current summary is in the [overview](overview.md). Raw reports stay in the ignored
`.local/evaluation/` directory.

## 2026-10-09: Ragas answer-quality run

**Setup**

| Item | Value |
|---|---|
| Code | commit `0959105` plus uncommitted changes (working tree dirty) |
| Questions | `eval/golden/ragas-questions.json`: 33 questions (25 fact, 8 distractor) taken from the retrieval golden set, SHA-256 `aee0652d…` |
| References | One-sentence answers written by the author from the labelled passages; **not yet reviewed by a human** |
| Corpus | The 13 documents in `eval/samples`, dedicated collections (`eval_retrieval_*`), port 8200 |
| Answer model | Qwen 3.5 4B, `top_k` 5, reranking off, semantic cache skipped |
| Embedding model | EmbeddingGemma 300M |
| Judge | `llama3.1:8b-instruct-q4_K_M` via Ollama (not the answer model), Ragas 0.4.3, 300 s per-metric timeout, no reasoning-effort setting |
| Commands | `python -m eval collect …` then `python -m eval score …` (collect and score run separately) |

**Duration**

| Phase | Started (UTC) | Ended (UTC) | Time |
|---|---|---|---|
| Collect 33 answers | 16:26:44 | 16:30:34 | 3 min 50 s |
| Score 4 metrics × 33 | 16:37:38 | 17:28:08 | 50 min 30 s |

Total about 54 minutes. The first scores were slow (about 2 minutes each), which
led to a wrong estimate of 4 hours; the pace increased later. Plan about an hour
for this set with this judge.

**Results**

| Score | Mean | Scored |
|---|---:|---:|
| Faithfulness | 0.743 | 33 of 33 |
| Factual correctness (F1) | 0.635 | 33 of 33 |
| Context precision | 0.961 | 33 of 33 |
| Context recall | 0.904 | 27 of 33 |

The collection run completed with 33 of 33 answers and no errors. The scoring
run is recorded as `failed` because six scores failed.

**Issues and errors**

1. **Judge output limit.** Six `context_recall` scores failed with
   `IncompleteOutputException: output is incomplete due to a max_tokens length limit`
   (fact-07, fact-11, fact-20, distractor-01, distractor-04, distractor-08). Context
   recall is the mean of the 27 that scored. Fix to try: raise the judge's output
   limit or use a judge that writes shorter structured output.
2. **Judge scores correct answers as wrong.** A hand check of five answers found
   four correct that the judge still marked down: fact-07 and fact-08 got
   faithfulness 0.0, fact-23 got faithfulness 0.0, and fact-21 ("17" against the
   reference "17") got factual correctness 0.0. The judge probably treats the `[1]`
   source marker and very short answers as unsupported claims. Treat faithfulness
   and factual correctness as a floor, not as answer quality. Context precision
   and recall do not depend on the answer text and agree with the retrieval
   golden-set check.
3. **One genuinely weak answer.** distractor-05 (difference between N:1, 1:1 and
   M:N scheduling) answered "CPU time", which misses the point.
4. **Small, unreviewed set.** 33 questions with author-written references; the
   earlier October 6 run used 3 questions graded by the answer model itself, so the
   two runs are not comparable.

**Next steps:** review the references; rate about 20 answers by hand to calibrate
the judge; try another judge or `--judge-reasoning-effort`; raise the judge output
limit; rerun.
