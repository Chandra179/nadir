# Personal/local v1

The goal is a dependable reading companion for one person's Markdown technical
notes on the current laptop. The user operates Qdrant, Ollama and the local
launcher/dashboard. Desktop packaging, PDF conversion, public hosting and
multiuser/distributed operation are outside this finish line.

The principal workflow is: import notes, ask a question, inspect its evidence,
ask a follow-up, cancel if needed and return to the saved conversation after
restart. An unavailable private/live value should be declined clearly.

## Acceptance criteria

| Area | Required outcome | Evidence |
|---|---|---|
| Setup | Documented models match configuration; enabled missing models prevent readiness with an installation hint | Role endpoint tests and live readiness in the lifecycle report |
| Import | Successful sources are visible, unchanged files skip, each failure has a reason, disabled PDF import does not crash | Runtime regression and mixed live import |
| Source updates | New source version replaces old evidence and invalidates cached results | Canary replacement and exact-repeat cache check |
| Chat lifecycle | Cancellation persists a partial answer; history and inventory survive restart; replay and citations work in the browser | Live API and browser checks |
| Supported questions | At least 27/30 useful, supported answers to the saved questions | Latest default-model candidate: **28/30**, count met; semantic regression guard still fails |
| Unsupported questions | 5/5 clear declines, without sample values or general facts substituted for actual values | **5/5**, saved question run and capability tests |
| Follow-ups | 5/5 preserve the intended subject and answer the follow-up with supported citations | Latest default-model direct review: **2/5**, currently unmet |

The 40-question pack was authored and saved before the baseline run. It covers
the 14-file sample corpus: 30 supported questions, five unsupported/private/live
questions and five follow-ups using generated history. It is simulated general
user testing by Codex, not real owner queries, production data or independent
human review. Source hashes keep questions and evidence fixed across reruns.
Review evaluates the actual question; fixture claim lists sometimes include
additional facts that the question did not ask for.

## Recorded evidence

- [Questions](../test/evaluation/daily-use-questions.json)
- [Baseline answers](../test/evaluation/reports/daily-use-baseline-20261001.json)
- [2026-10-01 answers](../test/evaluation/reports/daily-use-local-v1-20261001.json)
- [2026-10-01 serving provenance](../test/evaluation/reports/daily-use-local-v1-provenance-20261001.json)
- [2026-10-01 direct review](../test/evaluation/reports/daily-use-agent-review-20261001.json)
- [Lifecycle checks](../test/evaluation/reports/daily-use-lifecycle-20261001.json)
- [Validation and test-environment cleanup](../test/evaluation/reports/daily-use-validation-20261001.json)
- [Browser citation screenshot](../test/evaluation/reports/daily-use-citation-20261001.jpg)
- [PR #15 local validation and billing blocker](../test/evaluation/reports/frontend-pr15-review-20261001.json)

The fresh same-session control reproduced the earlier result: 23/30 supported,
5/5 unsupported and 4/5 follow-ups. The latest full default-model candidate adds
the task-prefix boundary fix. Each arm below completed the same 104 questions;
counts are direct Codex review against actual questions and cited evidence.

| Arm | Daily supported | Daily declines | Follow-ups | Broader supported | Broader declines |
|---|---:|---:|---:|---:|---:|
| Fresh control | 23/30 | 5/5 | 4/5 | 47/56 | 5/8 |
| Earlier retained candidate | 26/30 | 5/5 | 4/5 | 52/56 | 7/8 |
| Latest candidate, default models, corrected prefix | **28/30** | **5/5** | **2/5** | **49/56** | **7/8** |
| Installed 8B generator/rewriter, CPU embeddings | 26/30 | 5/5 | 3/5 | 49/56 | 8/8 |

**Local acceptance and the semantic no-regression guard still fail.** The latest
candidate omits manifest formats, cites publishing text for backpressure, and
uses wrong sections for Secant and lock follow-ups. The selected Manual mode
still changes to the Auto warning. Broader failures include wrong-source sine,
unnamed convergence, embedded-fintech delivery, structural/causal alternatives,
AP/CP alternatives, an unsupported lock condition and an unavailable Redis IP.
Zero unmapped citation IDs does not establish citation support.

The corrected-prefix default-model run has zero operational failures across all
104 turns. Daily submission-to-first-token p50/p95 is **0.747/2.879 seconds**;
three immediate capability declines are excluded. Broader streaming p50/p95 is
**0.700/0.812 seconds**, excluding five immediate declines. The earlier retained
candidate's 0.73/2.59 seconds and 52/56 broader answers remain historical results,
not interchangeable with the latest candidate. Three earlier pre-selected app
repetitions reproduced the mode failure; all attempts remain saved.

Retrieval uses unchanged 133-query golden and 64-query broader packs, three runs
each. With the prefix fix, golden Hit@5 rises **0.977 → 1.000**, Recall@5
**0.969 → 0.986**, MRR@10 **0.817 → 0.842**, and nDCG@5 **0.837 → 0.860**.
Distractor hits increase **0.226 → 0.278**. Broader Hit@5 remains **0.946**,
MRR rises **0.774 → 0.823**, nDCG **0.818 → 0.854**, and distractors rise
**0.219 → 0.234**. The registered retrieval bars pass. An offline central-text
rescore confirms golden gains without window annotation inflation; broader
central-only MRR/nDCG are 0.814/0.848. These gains do not replace semantic acceptance.

Latest evidence (run filenames retain their 20261002 identifiers):

- [Registration and candidate decisions](../test/evaluation/reports/quality-plan-20261002.json)
- [Latest daily answers](../test/evaluation/reports/quality-prefix-daily-20261002.json)
- [Latest broader answers](../test/evaluation/reports/quality-prefix-representative-answers-20261002.json)
- [Default-model serving provenance](../test/evaluation/reports/quality-prefix-provenance-20261002.json)
- [Latest direct review](../test/evaluation/reports/quality-prefix-agent-review-20261002.json)
- [Golden retrieval](../test/evaluation/reports/quality-prefix-retrieval-20261002.json)
- [Broader retrieval](../test/evaluation/reports/quality-prefix-representative-retrieval-20261002.json)
- [Central-evidence audit](../test/evaluation/reports/quality-prefix-central-evidence-audit-20261002.json)
- [Earlier control/retained review and repetitions](../test/evaluation/reports/quality-agent-review-20261002.json)
- [Browser citation proof](../test/evaluation/reports/quality-citation-20261002.jpg)
- [Browser reading view](../test/evaluation/reports/quality-reading-view-20261002.jpg)
- [Validation and environment limitations](../test/evaluation/reports/quality-validation-20261002.json)

## Hardware experiment and optional CPU embedding

The already-installed 8B model with GPU-default embeddings repeatedly incurred
roughly 9–18 second first-token times. That diagnostic run was interrupted after
24 cases and is explicitly incomplete. The separate CPU-embedding comparison
completed both retrieval packs three times and all 104 app questions. Hit,
Recall, MRR and nDCG match the corrected-prefix ranking aggregates; CPU golden
retrieval p50/p95 is **141/222 ms**, broader **177/346 ms**. Golden distractor
exposure is 0.271 rather than 0.278; device placement did not preserve every rank.

The 8B CPU-embedding daily first-token p50/p95 is **1.115/13.097 seconds**,
including the first cold load and later delays. The subsequent broader run is
**0.861/1.161 seconds**. Both have zero operational failures. The larger model
still invents a manual crash guarantee, reverses a hot/cold qualifier, omits
partition-key and mathematical conditions, uses wrong citations and falsely
declines available storage evidence. It is **not adopted as a default**.

The implementation extends the existing embedding provider and configuration:
`embedder.num_gpu` / `EMBEDDER_NUM_GPU` is optional. Omitted preserves Ollama's
placement; `0` uses CPU, `-1` requests automatic placement, and positive values
request GPU layers. The shipped value remains unset. Config validation and HTTP
payload tests cover unset and explicit zero. Model, prefixes and default
request payload are preserved. Placement alone does not require reindexing;
measure device numerics, ranks, coexistence and latency for each hardware profile.

The direct-provider binary completed the pre-selected 13-case smoke without an
options proxy. Embeddings used **0 GPU bytes** while generation used
**5,277,982,720 GPU bytes**; both were resident together. The smoke's first-token
p50/p95 is **1.065/6.946 seconds** and its direct semantic review is **9/13**.
This verifies wiring and resource feasibility, not full quality acceptance.

- [CPU comparison provenance](../test/evaluation/reports/quality-cpue-model-provenance-20261002.json)
- [CPU comparison daily answers](../test/evaluation/reports/quality-cpue-model-daily-20261002.json)
- [CPU comparison broader answers](../test/evaluation/reports/quality-cpue-model-representative-answers-20261002.json)
- [Full 8B semantic review](../test/evaluation/reports/quality-cpue-model-agent-review-20261002.json)
- [Direct-provider provenance](../test/evaluation/reports/quality-cpu-direct-provenance-20261002.json)
- [Direct-provider smoke](../test/evaluation/reports/quality-cpu-direct-smoke-20261002.json)
- [Direct-provider review](../test/evaluation/reports/quality-cpu-direct-smoke-review-20261002.json)
- [Resident models after smoke](../test/evaluation/reports/quality-cpu-direct-residence-20261002.json)
- [Rejected diagnostic variants](../test/evaluation/reports/quality-diagnostic-decisions-20261002.json)

Intermediate reports remain diagnostic. All-ancestor indexing failed broader
Hit@5. Stronger prompts, altered message roles, simpler source labels, quote
planning and scope filtering introduced false claims, omissions or wrong
citations. A history-derived scope filter removed the Auto warning but replaced
it with an unsupported crash-recovery guarantee, so it was rejected. Captured
prompt replays are not full app runs. The first label arm's broader report has
only ten cases and is explicitly incomplete.

## Working source and retrieval changes (2026-10-02)

Short recursive sections retain a complete answer window bounded to twice the
chunk size; central text remains the embedding unit. Standalone bold labels
preserve scopes such as automatic versus manual acknowledgement. Full heading
ancestry is persisted through document, retrieval and cache payloads and shown
in citation labels; the filterable leaf header remains separate. Only leaf
headings repeated under distinct Markdown parents are qualified for indexing.
Broad ancestor prefixes for every heading regressed retrieval and were removed.

The embedding task prefix previously mutated the shared fragment slice and
also reached lexical search and fusion. A failing-then-passing regression now
verifies separate prefixed dense inputs and raw lexical fragments. This query-only
fix does not require a source reindex; use a fresh/cleared cache when measuring it.

Hybrid sparse queries omit common English function words while retaining
negation and numbers. Document sparse vectors and explicit keyword lookup retain
their original terms. Native Qdrant RRF, configured models, chunk size, reranker
and fusion settings stay as measured. Sources are presented in retrieval order.
Numeric source footnote markers are labeled as document footnotes without
source-local numbers in the admitted snapshot; original stored text and ordinary
bracketed values remain intact. Unchanged unresolved conditional/ordinal rewrites
retain bounded prior answer context for retrieval. This still does not reliably
preserve the selected mode in generation.

## Reindexing the working source policy safely

Existing source-SHA deduplication skips unchanged files; merely restarting or
pressing Import does not rebuild their chunks. The source policy therefore
requires a full reindex before evaluating it on an existing corpus. No owner
collection has been reset or migrated during these experiments.

1. Keep the current config and old collections. Make a separate config copy.
2. Choose an unused `qdrant.collection` and unused `semantic_cache.collection`
   in that copy, so old document/cache payloads cannot mask the new policy.
   Preserve `history.collection` if existing conversations should remain visible.
3. Confirm that every desired original is available, including files previously
   uploaded through the dashboard; fresh imports cannot reconstruct missing originals
   from the old inventory. Configure all note directories and re-upload other files.
4. Start with the copied config (`go run ./cmd/api --config /path/to/copy.yaml`).
   Check environment overrides: `QDRANT_COLLECTION` overrides the copied value.
   There is no `SEMANTIC_CACHE_COLLECTION` override; edit that YAML field.
5. Import into the new collection and inspect inventory, errors and saved questions.
   Keep the old config/collections for rollback. Retire them only after validation
   and a deliberate owner decision.

## Completed fixes

Generation keeps the original question and puts bounded conversation references
before it. Prior-turn citation numbers are removed from reference answers so
they cannot be reused for different sources in the next turn. An unchanged
ordinal reference such as "the second one" retains bounded prior user context
for retrieval; standalone questions with local pronouns do not inherit an
unrelated previous subject. Rewriting requests complete questions and bounds output. Explicit
English requests about an owned system's present state receive a capability
decline; this rule is intentionally narrow and is not a general answerability
classifier. Static technical/source questions still use retrieval.

Readiness checks installed generator/rewriter metadata at the configured role
endpoint without loading models into the GPU. Successful inference and available
capacity are measured by actual turns. Documentation now pulls both configured
models and treats the reranker as optional and off by default.

The dashboard keeps repeated rewrite context inside Inspect so it does not
flood the normal reading view. Search/prompt details remain inspectable.

The dashboard lists active indexed file names and per-file import errors. Only
successful sources appear attached. Its last-import summary lasts for the server
run; the file inventory is persisted in Qdrant. A disabled Docling adapter now
returns a nil interface and a clear PDF import failure instead of crashing an
indexing worker. Grouped citation markers now link each known source.

The live browser suite deletes only the session it creates, and the API proxy
can target a separate local test server. All live tests used an isolated,
ephemeral Qdrant container; existing user collections and containers were not
reset or modified.

## Remaining priorities and bottlenecks

The finish line remains open. First preserve the selected subject/mode through
rewriting, retrieval and generation, and explicitly decline a conditional the
admitted evidence cannot substantiate. Then verify material inline claims against
their actual cited section and preserve requested alternatives and restrictions.
The current default-model failures are `notes-23`, `notes-27`, `followup-01`,
`followup-02` and `followup-03`; the latest direct review records broader failures.
Do not copy failures or counts from an earlier arm into the latest checklist.

The sample's Manual section states at-least-once semantics and nack options but
does not explicitly describe crash redelivery. Do not invent a recovery step to
satisfy a fixture or change the saved questions to make the model pass. Returning
a supported limitation is valid. No additional hardware or human answer is
required to continue correcting the observed subject and citation failures.

The laptop has 6 GiB GPU memory and about 15 GiB RAM. Keep the measured default
retrieval path and one interactive stream; the earlier eight-stream load showed
model-serving queue latency. A CUDA reranker competing with the answer model is
outside the current memory budget. Single-user development can continue on this
hardware.

The read-only GitHub review observed open issues #13 (evaluator/calibration)
and #14 (dependency updates), with PR #15 open and mergeable. The latest observed
hosted check annotation says the job did not start because of an account billing
lock. An additional refresh initially could not run because automatic approval review
hit a Codex usage limit. A later approved retry completed: the newest listed run
remains September 30, with no newer hosted evidence. The current billing balance
is not independently verified. See the [saved GitHub observation](../test/evaluation/reports/quality-github-status-20261002.json).
Local development continues. The owner must restore hosted Actions access before
hosted evidence and the dependency merge can be completed.
The compatible brace-expansion patch is applied in the local lockfile; the batch's
old lockfile needs that patch too. The [ready patch](../test/evaluation/reports/frontend-pr15-audit-fix.patch)
and validation report are saved. The 2026-10-01 tooling validation used Node 24 LTS. The additional
2026-10-02 Vite/browser check used the available Node 25.6.1 runtime; it does not
replace the earlier supported-toolchain validation.

Independent human calibration remains outstanding for the exported 39-case judge
packet. It is necessary before calling automated judge scores calibrated; it is
not a prerequisite for fixing this local app. Owner testing on real notes is the
remaining evidence of personal usefulness, while the simulated questions already
provide concrete engineering failures to work on.

## Saved owner questions

No new owner answer is needed for the current engineering failures. When owner
usefulness testing starts, save ten common questions against the actual notes
before running them, and decide whether useful citation-backed reading or an
explicit inability to answer is the desired outcome for each. Owner review of
those answers remains distinct from these simulated questions and from the
39-case judge-calibration packet.

A saved source-coverage question for later owner review: should the actual notes
include an authoritative explanation of manual acknowledgement crash recovery?
The existing sample does not establish that sequence. Until appropriate evidence
is indexed, the app should state that limitation rather than infer a guarantee.
This question does not block engineering work on subject preservation.
