# 0035 — Preserve source scopes and measure the local quality candidate

- **Status:** Adopted on 2026-10-03; historical evaluation assets retired on 2026-10-06
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

## Historical measurements and archived interpretation

The October 2 control and October 3 candidate Go evaluator outputs used the
133-query golden and 64-query broader packs. Each pack records three retrieval runs,
rankings, model/config/corpus provenance and aggregate metrics. The control was
not rerun October 3. These generated measurements describe the removed sample
corpus and cannot establish quality for current uploaded documents. The
historical inputs and results were retired on October 6 and are recoverable
from Git revision `31c84e4`; see the
[evaluator recovery guide](../../eval/README.md#retired-assets-and-recovery).

Earlier manually assessed app runs identified wrong source scopes, omitted
alternatives and lost follow-up modes. Selected sections, narrow missing-source
guards, row-preserving literal attribution and complete source excerpts were
adopted to address those observations. Manual app reviews, repetitions, model
smoke tests and migration/restart checklists are archived; their records and
original hash manifest are recoverable from the preceding Git history and
local backups. They are dated design history. No independent judge calibration
or current owner usefulness is established by those archived interpretations.

## Consequences

Existing source-SHA skipping cannot upgrade unchanged documents to new chunks
or indexing inputs. A full reindex requires all source originals. Use a copied
configuration and unused document/cache collection names, preserve the old
collections/configuration for rollback, and do not reset owner data to reproduce
these experiments.

The task-prefix boundary fix is query-only and requires a fresh/cleared cache
for measurement, not a source reindex. Optional GPU layer placement does not
change models or prefixes and does not itself require reindexing; measured
device numerics and latency can differ. Source window/heading policy still
requires the full migration described above.

Generation must continue to preserve selected modes, answer every requested alternative
and substantiate inline citations beyond the finite measured cases. A retrieved source or a correct number alone
cannot satisfy those requirements. More hardware, a new architecture or owner
interview is not required to keep diagnosing these concrete failures.
