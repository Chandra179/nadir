# 0035 — Preserve source scopes and measure the local quality candidate

- **Status:** Accepted for the finite personal/local checklist on 2026-10-03; measured source/model/config provenance is retained with the acceptance evidence
- **Date:** 2026-10-02

## Context

The saved personal/local question pack exposed source-boundary and subject
loss. Recursive overlap carried an automatic-acknowledgement warning into an
unlabeled manual chunk. Repeated leaves such as `Properties` lost their parent
method. Natural English question words also gave unrelated passages a lexical
rank contribution. The embedding task-prefix function additionally mutated the
shared fragment slice, adding task instructions to lexical search and fusion. Mapped citation numbers did not establish supported answers.

The candidate keeps the existing 512/64 recursive chunks, EmbeddingGemma,
Gemma 3 4B answer model, Gemma 3 1B rewriter, native Qdrant RRF and disabled
reranker/fusion. No sample text or fixed evaluation query was rewritten.

## Source policy

- Retain a complete normalized answer window only for sections at most twice
  the chunk size. Central embedding text and source anchors remain bounded.
- Split at short leading standalone bold labels and keep the label in evidence.
- Persist full `SectionPath` through indexing, retrieval and cache payloads;
  retain `Header` as the filterable leaf. Qualify an indexing heading only
  when it occurs under distinct Markdown parent paths. Unique-heading labels
  use `leaf > label`, avoiding broad ancestor noise.
- Apply embedding task prefixes to separate dense inputs, preserving raw lexical
  fragments. A failing-then-passing regression verifies both batch inputs and
  hybrid query text; this invariant fix is independently confirmed.
- Filter common English function words only in hybrid sparse queries. Keep
  negation, numbers, document vectors and literal keyword lookup unchanged.
- Present admitted sources in retrieval order; render numeric source footnote
  markers as document notes without source-local numbers in the prompt snapshot.
  Stored chunks, raw source identity and ordinary bracketed values stay intact.
- Retain bounded prior answer context when a successful unresolved conditional
  or ordinal rewrite returns unchanged. Named standalone subjects do not inherit
  an unrelated prior answer. The original generation question remains authoritative.

## Historical evidence and disposition (2026-10-02)

Fresh isolated collections and fixed 14-file hashes provide a same-session
control. The 133-query golden and 64-query broader retrieval packs each run
three times. Aggregates below are the evaluator's median across runs:

| Metric | Golden control | Working candidate | Broader control | Working candidate |
|---|---:|---:|---:|---:|
| Hit@5 | 0.9774 | 1.0000 | 0.9464 | 0.9464 |
| Recall@5 | 0.9687 | 0.9862 | 0.9464 | 0.9464 |
| MRR@10 | 0.8169 | 0.8416 | 0.7738 | 0.8229 |
| nDCG@5 | 0.8369 | 0.8599 | 0.8180 | 0.8544 |
| Distractor hit@5 | 0.2256 | 0.2782 | 0.2188 | 0.2344 |

This is the latest corrected-prefix arm, not the earlier retained candidate.
Registered Hit/MRR/nDCG guards pass, but distractor exposure increases. Central
text rescoring confirms golden gains without window annotation inflation;
broader central-only MRR/nDCG are 0.8140/0.8478. See [registration](../../test/evaluation/reports/quality-plan-20261002.json),
[golden](../../test/evaluation/reports/quality-prefix-retrieval-20261002.json),
[broader](../../test/evaluation/reports/quality-prefix-representative-retrieval-20261002.json)
and [central audit](../../test/evaluation/reports/quality-prefix-central-evidence-audit-20261002.json).

Full app replay reviews at **28/30 supported, 5/5 declines and 2/5 follow-ups**;
broader results are **49/56 and 7/8**. The fresh control is 23/30, 5/5, 4/5 and
47/56, 5/8. All 104 turns complete without operational failures or unmapped IDs.
Wrong cited sections, omitted alternatives and lost follow-up mode mean semantic
no-regression and local acceptance fail. **This policy is not accepted as a
quality release.** See the [latest direct review](../../test/evaluation/reports/quality-prefix-agent-review-20261002.json).

The earlier retained arm's 26/30, 4/5 and 52/56 remain a separate historical
measurement. All-ancestor indexing failed broader Hit@5. Stronger prompts,
message roles, quote planning and scope filtering introduced false claims or
wrong citations and were rejected. Three earlier app repetitions retained the
mode failure; captured-prompt experiments are diagnostic, not full app runs.

An installed-8B generator/rewriter experiment confirmed repeated model eviction
with GPU-default embeddings. Explicit CPU embedding made both models resident
on existing hardware, but the full 104-case [semantic review](../../test/evaluation/reports/quality-cpue-model-agent-review-20261002.json)
still fails at 26/30, 5/5, 3/5 and 49/56, 8/8. The 8B model is not adopted.
Optional placement is implemented through existing config/provider seams;
`embedder.num_gpu` is unset by default and explicit zero is preserved. The
[direct-provider smoke](../../test/evaluation/reports/quality-cpu-direct-smoke-20261002.json)
verifies operational wiring and [coexistence](../../test/evaluation/reports/quality-cpu-direct-residence-20261002.json),
not semantic acceptance. None of these reviews is independent human calibration.

## Acceptance update (2026-10-03)

Explicit persisted selected sections, narrow missing-source guards, row-preserving
literal attribution and complete source excerpts address the observed failures.
The unchanged full app packs review at 30/30, 5/5, 5/5 and 56/56, 8/8; 13 fragile
cases pass three repetitions. Hit/MRR/nDCG stay within the registered 0.01 limit
on both retrieval packs, three runs each. No new material failures were found
against the saved October 2 fresh control; it was not rerun on October 3.
Defaults, questions, source hashes and ranked evidence remain unchanged.

A copied-config migration imports 15 real notes, archives 32 exact synthetic
load originals, preserves the old document/cache/config and existing history,
and passes full app, duplicate-import and persisted-subject restart checks.
See [fitness evidence](../P1_EVIDENCE.md) and
[direct review](../../test/evaluation/reports/p1-accepted-agent-review-20261003.json).
This accepts the finite local policy and measured workflow; it does not claim
universal citation entailment, independent judge calibration or owner usefulness.

## Consequences

Existing source-SHA skipping cannot upgrade unchanged documents to new chunks
or indexing inputs. A full reindex requires all source originals. Use a copied
configuration and unused document/cache collection names, preserve the old
collections/configuration for rollback, and do not reset owner data to reproduce
these experiments. See [safe migration](../LOCAL_V1.md#reindexing-the-working-source-policy-safely).

The task-prefix boundary fix is query-only and requires a fresh/cleared cache
for measurement, not a source reindex. Optional GPU layer placement does not
change models or prefixes and does not itself require reindexing; measured
device numerics and latency can differ. Source window/heading policy still
requires the full migration described above.

Generation must continue to preserve selected modes, answer every requested alternative
and substantiate inline citations beyond the finite measured cases. A retrieved source or a correct number alone
cannot satisfy those requirements. More hardware, a new architecture or owner
interview is not required to keep diagnosing these concrete failures.
