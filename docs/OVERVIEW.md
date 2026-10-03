---
title: "Nadir"
description: "Nadir is a private-document RAG chat app with hybrid search."
seoTitle: "Nadir: Private-Document RAG Chat with Hybrid Search"
seoDescription: "Nadir is a private-document RAG chat app with hybrid search."
answerSummary: "Nadir is a private-document RAG chat app with hybrid search."
tags: [system-design, llm, rag]
links:
  github: "https://github.com/Chandra179/nadir"
created: 2026-09-10
---

# Nadir: A Private-Document RAG Chat with Hybrid Search

Nadir is a private document search and question-answering application. It
turns your documents into a searchable knowledge base, then answers questions
using the relevant passages from those documents.

It is useful for:

- personal notes and study material;
- technical documentation;
- research papers and manuals;
- internal knowledge bases; and
- any collection of text that needs reliable, document-grounded answers.

Nadir can run locally, so your documents and questions do not need to leave
your environment.

## How it works

```text
Documents → prepare and index → search knowledge base → generate grounded answer
                                      ↑                         ↓
                              your question ← conversation history
```

Nadir presents this workflow through a responsive browser dashboard. The
dashboard sends structured requests to the application and receives generated
answers as a live stream, while the application remains responsible for
retrieval, ordering, persistence, and data safety.

### 1. Add documents

Nadir reads supported documents and breaks them into smaller passages. Each
passage keeps useful context such as its source and position, so answers can
be traced back to the original material.

### 2. Ask a question

Nadir searches the indexed passages for the information most relevant to the
question. Follow-up questions can use earlier turns in the same conversation.

### 3. Review the answer

When answer generation is enabled, Nadir creates a response from the selected
passages and shows the supporting context. The answer is streamed as it is
generated, so the user can start reading immediately.

## Main features

- **Private local search** — documents, queries, and answers can remain on
  your own machine or network.
- **Semantic search** — finds passages with a similar meaning even when they
  do not use exactly the same words.
- **Keyword search** — finds exact terms, names, numbers, and identifiers.
- **Hybrid retrieval** — combines semantic and keyword results for better
  coverage.
- **Optional reranking** — uses a stronger relevance model to improve the
  order of the best candidates.
- **Grounded answers** — generates answers from retrieved document passages
  instead of relying only on the language model's memory.
- **Conversation history** — keeps sessions and allows follow-up questions.
- **In-place editing** — edit an earlier question and replace that point in
  the conversation, including all later turns.
- **Semantic caching** — reuses results for sufficiently similar questions to
  reduce repeated work.
- **Incremental indexing** — unchanged documents are skipped during later
  indexing runs.
- **Optional enrichment** — improves document discoverability by adding
  contextual descriptions during indexing (off by default).
- **PDF support** — PDFs can be converted to searchable text when conversion
  support is enabled.

## Algorithms

### Chunking

Large documents are divided at headings, paragraphs, and sentence boundaries.
If a section is still too large, it is split into bounded pieces — about 512
characters by default. This gives the search engine focused passages without
losing the document structure.

### Embeddings

An embedding model converts each passage and question into a numerical vector.
Nadir uses EmbeddingGemma 300M, a compact embedding model served by Ollama,
which produces 768-dimension vectors. Texts with similar meaning produce
vectors that are close together, enabling meaning-based search.

### BM25 keyword search

BM25 scores how well the exact words in a question match each passage. It is
especially useful for names, commands, product terms, formulas, and numbers.
In Nadir this keyword index lives inside Qdrant next to the vectors — no
separate search engine is needed.

### Reciprocal Rank Fusion

Semantic search and BM25 produce separate ranked lists. Reciprocal Rank Fusion
combines their positions rather than comparing incompatible score values. A
passage that ranks well in either list can therefore contribute to the final
result. Fusion runs inside Qdrant by default; a weighted variant with optional
exact-match and heading boosts can be enabled in configuration.

### Reranking

The first search stage retrieves a broader set of candidates quickly. An
optional cross-encoder — the BAAI BGE reranker v2-M3, running in a small
Python sidecar — then reads the question and each leading passage together
and gives them a more precise relevance order.

### Semantic cache

Nadir compares a new question with cached questions using vector similarity.
When the meaning is close enough, it can reuse the previous retrieval result
instead of repeating the full search.

### Grounded generation

The selected passages are placed into a prompt with their source context. The
language model — Gemma 3 4B, served by Ollama — is instructed to answer from
that material with a bounded output length, which reduces unsupported claims
and makes the result easier to verify.

## Models

Nadir ships with small, local-first defaults. Every model runs on your own
machine: the text models are served by Ollama, and the optional reranker by a
small Python sidecar.

| Role | Default model | Notes |
|---|---|---|
| Embeddings | EmbeddingGemma 300M (`embeddinggemma-300m-q8`, 768 dimensions) | task-instruction prefixes are applied at query and index time; changing the model or prefixes requires a reindex |
| Keyword search | Qdrant sparse index (BM25-style) | part of the search index, not a separate model |
| Answer generation | Gemma 3 4B (`gemma3:4b`) | streamed; the 1B variant is the low-latency fallback |
| Follow-up rewriting | Gemma 3 1B (`gemma3:1b`) | runs only for follow-up turns; falls back to the original wording on failure |
| Contextual enrichment (optional) | Gemma 3 1B (`gemma3:1b`) | off by default; enabling it requires a reindex |
| Reranking (optional) | BAAI BGE reranker v2-M3 | off by default; handles one request at a time |

Every model is swappable in `internal/bootstrap/configuration/config.yaml`
(with environment overrides such as `EMBEDDER_MODEL`, `GENERATOR_MODEL`, and
`REWRITE_MODEL`). Each enabled text-model role declares its own Ollama
endpoint and model — none inherits another role's — and startup readiness
checks that each enabled model is installed at its own endpoint.

## Under the hood

Documents flow through one pipeline when they are added; questions flow
through another when they are asked. A small amount of bookkeeping keeps the
heavy work of each from colliding. None of this changes how Nadir is used —
it explains why it stays fast and predictable.

### How the chat is built

The code follows a hexagonal architecture — also known as ports and adapters.
The application's rules live at the center and know nothing about the outside
world; everything external connects through a narrow interface (a "seam") that
the center defines. Three rings, from the inside out:

```text
   browser
      │  HTTP + server-sent events
      ▼
   HTTP edge          translates requests, streams answers
      │
      ▼
   core               conversation · retrieval · documents
      │               owns the rules; defines the interfaces
      ▼
   providers          Ollama · Qdrant · reranker · Docling

   bootstrap wires these rings together at startup
```

- **Core** owns the rules. The conversation service runs the turn lifecycle —
  sessions, edits, generation supervision; retrieval runs the search pipeline;
  documents run indexing. Core packages define the interfaces they need and
  never import the HTTP layer, the model adapters, or the frontend. This is an
  enforced rule, not a convention.
- **Adapters** sit at the boundary. The HTTP edge translates requests and
  streams events; provider adapters speak to Ollama, Qdrant, the reranker
  sidecar, and Docling. Each adapter implements an interface the core defined,
  so any of them can be replaced without touching the rules.
- **Bootstrap** is the composition root: the only code that builds the whole
  object graph at startup, shared by the API server and the evaluation CLI.

Two decisions explain the chat behavior described later in this document. A
turn is started by an HTTP request but never owned by it: generation runs
detached from the request that started it and appends to a bounded event log,
and the browser subscribes to a stream endpoint that replays from its last
received event. That is why closing the page, reopening it, or reconnecting
never kills an answer. And because the conversation service — not the
transport — owns history changes, an edit or a cancel is coordinated with any
generation still in flight instead of racing it.

The repository's design records describe the same shape as bounded contexts
with consumer-owned capability seams ([ADR 0020](adr/0020-consumer-owned-capability-seams.md),
[ADR 0022](adr/0022-bounded-context-layout.md)), a domain-owned event log for
streaming ([ADR 0006](adr/0006-chat-streams-over-domain-owned-event-log.md)),
and a shared runtime composition ([ADR 0026](adr/0026-shared-runtime-composition.md)).
The engineering-level details live in the
[architecture guide](../AGENTS.md#architecture).

### Finding the right passages

```text
        your question
             │
             ▼
   ┌───────────────────┐  was a very similar question
   │  semantic cache   │ searched recently?
   └────────┬──────────┘ ─── yes ──▶ reuse that result
            │ no
            ▼
   break the question into its sub-questions, if it has several
            │
            ▼
   for each part: search by meaning + search by exact words,
   then combine the two rankings
            │
            ▼
   keep the best passages — at most a few per document
            │
            ▼
   the reranker re-reads the question together with each
   leading passage and puts them in a finer order
            │
            ▼
   final passages ──▶ remembered in the cache for next time
```

Three ideas do most of the work. The semantic cache answers a repeated or
rephrased question from memory, so an earlier search is reused instead of
repeated. Meaning-based search finds passages written with different words,
while exact-word search catches the names, formulas, and identifiers that
meaning alone can miss; the two rankings are combined into one. Finally the
reranker — the slowest, most careful step — reads the question together with
each leading passage. If it is unavailable, the search order is kept rather
than losing the answer. Results are capped per document, so one long file
cannot crowd out the rest.

### How a conversation turn works

```text
        you ask a question
              │
   editing an earlier question? ──▶ that turn and everything after
              │                    it are replaced by the new answer
              ▼
   a follow-up question? ──▶ rewritten into a standalone question
              │              using the recent conversation
              ▼
     retrieve passages (diagram above)
              │
              ▼
     generate the answer from those passages, streamed as it is written
              │
    ├─ closing the page does not stop it: the answer finishes and is saved
    ├─ cancelling keeps what was already written
    └─ reopening the page shows the answer from where you left off
```

A follow-up question is first rewritten into a standalone question using the
recent conversation, so "what about the second one?" searches for something
meaningful; if that step fails, the original wording is used. Only an
explicit cancel stops a running answer. Closing the page does not: the answer
finishes in the background and is saved, so reopening shows it from where you
left off. Editing an earlier question trims that branch of the conversation —
the edited question is answered against everything before it, and everything
after it is replaced. The conversation itself is stored, so history survives
a restart; an answer that was still being written is saved up to that point.

### Keeping heavy work orderly

```text
 ┌──────────────────────────────────────────────────┐
 │ indexing:         one run at a time              │
 │                                                  │
 │ destructive:      one at a time, and never       │
 │ reset, edit or    while an indexing run is       │
 │ delete chats      in progress                    │
 │                                                  │
 │ busy?             new work is turned away        │
 │                   promptly instead of piling up  │
 └──────────────────────────────────────────────────┘

   talking to the AI model is a separate matter: the model
   server itself decides how many requests it handles at once
```

This prevents two indexing runs from racing each other, and a reset from
deleting content halfway through an indexing run. Turning work away quickly
keeps the app responsive instead of letting hidden queues grow. Reranking has
its own one-at-a-time queue for the same reason.

### What "ready" means

Readiness checks the search index, the embedding model, any enabled reranker,
and installed metadata for each enabled answer/rewrite model at its own
endpoint. A missing model returns an installation hint. Model metadata does
not establish inference capacity or residency; actual turns verify serving
on the current hardware without readiness loading and swapping answer models.

## Current evidence

These are the latest engineering measurements. The 133-query fixture uses the
sample documents and synthetic user-intent queries, so it is a regression
signal rather than production release evidence.

| Area | Latest result |
|---|---|
| Corrected defaults on the repaired evaluator (report schema v2, reset+reindex for per-chunk lines) | Golden pack, no reranker, 3 runs / 399 pooled requests: HitRate@5 **0.977**, Recall@5 **0.969**, MRR@10 **0.817**, nDCG@5 **0.837**, p50/p95 **99/127 ms** — identical across all three runs, so retrieval is deterministic on this stack and the earlier ±0.015 variance concern did not reproduce. Representative 14-document pack (64 queries incl. 8 unsupported): HitRate@5 **0.946**, MRR@10 **0.774**, p50 **102 ms**; generation gemma3:4b judged by the observed-3.8B phi4-mini: faithfulness **0.822**, relevancy **0.811**, context precision/recall **0.578/0.643**, judge coverage 100% with zero failures, **8/8 correct abstentions** (mean abstention 1.0) — although the judge scored 4 of those correct abstentions faithfulness 0, which is the first item for the blind human calibration ([golden](../test/evaluation/reports/retrieval-golden-defaults-20260930.json), [representative+generation](../test/evaluation/reports/generation-representative-defaults-20260930.json))  |
| Concurrent load on the corrected defaults (concurrency 8, 30 req/workload, zero failures) | `long_retrieval` p50/p95 **1.5/2.0 s** — the old ~30 s retrieval p95 behind chat streams is resolved; `large_ingestion` p95 **2.8 s**; `chat_streams` p50 13.2 s with **first-token p50 10.8 s** — the remaining head-of-line is model serving (Ollama serializing eight 4b generations), not retrieval ([report](../test/evaluation/reports/load-defaults-20260930.json))  |
| Daily-use answer quality and streaming latency | Latest corrected-prefix default-model run: **28/30 supported, 5/5 declines, 2/5 follow-ups**; broader **49/56 and 7/8**. All 104 turns complete without operational failures; daily first-token p50/p95 **0.747/2.879 s** excludes three immediate declines. Wrong citations and follow-up subject loss fail the semantic guard. Installed 8B with CPU embeddings completes all 104 but still fails quality (**26/30, 5/5, 3/5**); cold daily p95 **13.097 s**, subsequent broader **1.161 s**. Optional CPU placement is implemented and directly verified; model defaults remain. See [latest review](../test/evaluation/reports/quality-prefix-agent-review-20261002.json), [8B review](../test/evaluation/reports/quality-cpue-model-agent-review-20261002.json) and [full evidence/migration](LOCAL_V1.md). Simulated results are not human calibration or product-completion percentages. |
| Semantic cache and follow-up rewriting, 2026-09-30 | The committed report contains 5 cache cases with **0 hits**, so hit correctness is unknown; 3/4 rewrites were observed with one retrieval regression. The earlier 7/7 traps and 4/4 paraphrase claims were not supported by this artifact. The new [lifecycle check](../test/evaluation/reports/daily-use-lifecycle-20261001.json) verifies an exact-repeat cache hit and invalidation after a source update; this does not measure paraphrase quality. ([older report](../test/evaluation/reports/user-paths-defaults-20260930.json)) |
| Context-selection experiment, budget arm (pre-registered vs same-session control) | Admitted-context budget 1400 vs 2800 tokens: context precision **0.541 vs 0.578**, recall 0.614 vs 0.643, `context_selection` misses 15 vs 12 — the arm FAILED its bar (needed precision +≥0.05) and is rejected; tail chunks carry usable evidence. Budget stays 2800 ([arm](../test/evaluation/reports/generation-representative-ctx1400-20260930.json), [control](../test/evaluation/reports/generation-representative-defaults-20260930.json))  |
| Hybrid Retrieval, no reranker | HitRate@5 **0.805**, MRR@10 **0.657**, p50/p95 **19/23 ms**  |
| BGE reranker on CPU | MRR@10 **0.697**, nDCG@5 **0.709**; rerank p50/p95 **9.4/18.6 s**  |
| BGE reranker on GPU (laptop RTX) | Same quality: MRR@10 **0.697**, nDCG@5 **0.709**; rerank p50/p95 **0.44/0.70 s** (≈21× faster); peak VRAM about **3.7 GiB**  |
| BGE reranker quantized to 8-bit integers on CPU | Quality matched or beat the full-precision reranker (MRR@10 **0.725**, nDCG@5 **0.731**); rerank p50/p95 **5.4/9.7 s** — only about 1.7× faster than full precision, so the portable default stays full precision ([report](../test/evaluation/reports/e2e-podman-rerank-onnx-int8-20260927.json))  |
| EmbeddingGemma, fresh reindex on the current build | HitRate@5 **0.955** over the four-document fixture and **0.940** over the full corpus, MRR@10 **0.742 / 0.795**, at embed p50/p95 **95/106 ms** — versus same-day Nomic controls of 0.797 / 0.759; EmbeddingGemma is now the default (ADR 0032)  |
| Chunker correctness fixes (code blocks, list-item separators, oversized re-split, result identity, original-query fragment) | Full corpus, no reranker: HitRate@5 **0.970–0.985**, MRR@10 **0.806–0.809**, nDCG@5 **0.831–0.840** — up from 0.947/0.796/0.827 the same day pre-fix; 368 → 442 indexed points now include fenced code content (ADR 0034, [reports](../test/evaluation/reports/chunker-fix-512-fullcorpus-20260929-run1.json))  |
| Chunk size 512 vs 2048 runes (fixed chunker, pre-registered) | 2048 failed both ranking bars: MRR@10 **0.788 vs 0.806–0.809**, nDCG@5 **0.818–0.820 vs 0.831–0.840** at HitRate parity; `chunk_size` stays 512 (ADR 0034, [2048 report](../test/evaluation/reports/chunk-2048-fullcorpus-20260929-run1.json))  |
| Reranker re-measured on the EmbeddingGemma default | With rerank: HitRate@5 **0.910**, nDCG@5 **0.767** at 511 ms p50; without: **0.947 / 0.827** at 103 ms — the cross-encoder now degrades every metric at ~5× latency and stays off the measured default path (ADR 0034, [report](../test/evaluation/reports/pre-fix-rerank-gpu-fullcorpus-20260929.json))  |
| Weighted fusion vs native rank fusion | The optional weighted-fusion profile was measured against the default on both corpus states and lost on every metric; the default stays native fusion  |
| Recursive vs sentence-window chunking | Splitting into sentence-sized pieces with wider context windows at answer time lost retrieval recall (HitRate@5 **0.925** vs **0.940**) while ranking held; the recursive chunker stays the default, and any revisit needs a segmenter that truly splits bullet-list markdown into sentences first (ADR 0033, [reports](../test/evaluation/reports/chunk-recursive-goldencorpus-20260927.json))  |
| Answer-quality judge, before the grounding fix | 129/133 queries evaluated: faithfulness **0.485**, answer relevancy **0.780**, context precision/recall **0.615/0.622**; 4 failures ([the generation triage report](../test/evaluation/reports/generation-triage-20260916.json))  |
| Answer-quality judge, after grounding fix + answer-shape recalibration (live reruns) | Pre-fix baseline: faithfulness **0.485**, relevancy **0.780**. Now: all 133 evaluated with zero failures — faithfulness **0.690**, relevancy **0.733**, context precision/recall **0.689/0.736**. Grounding rose sharply once answers stopped padding, and answer shape now matches the question; the residual relevancy gap tracks the queries where Retrieval misses ([report](../test/evaluation/reports/e2e-podman-generation-promptfix-20260927.json))  |
| Generation gate after the ADR 0034 fixes (num_ctx pinned, strict abstention, judge auditability) | First configuration to pass both bars — faithfulness ≥ 0.65 **and** relevancy ≥ 0.75. gemma3:1b: **0.750 / 0.756**; gemma3:4b: **0.881 / 0.803** with generation failures 20 → 3, at ~9× answer latency (4.3 s vs 0.48 s p50). `generator.model` default is now gemma3:4b; gemma3:1b is the low-latency fallback (ADR 0034, [1b](../test/evaluation/reports/generation-gemma1b-fixes-fullcorpus-20260929.json), [4b](../test/evaluation/reports/generation-gemma4b-fullcorpus-20260929.json))  |
| PDF intake | 18/18 successful conversions, p50/p95 **2.62/25.94 s**, peak RSS about **3.28 GiB**  |

The results show that Retrieval is fast without reranking, while reranking and
PDF conversion are the main latency and resource costs. The default models and
fusion policy remain conservative until consent-safe, expert-judged production
data is available.

The ARQMath importer is available for optional public-math research; its
candidate pack and full licensed corpus are not present in this checkout.
They do not block the personal/local technical-notes release. Its finite
scope, direct acceptance evidence and remaining answer failures are recorded
in [LOCAL_V1.md](LOCAL_V1.md).

The schema-v2 evidence above was recorded 2026-09-30 after a reset-and-reindex
(per-chunk source lines change chunk IDs, so a published version rejects
re-staging and a reset is required). The reranker opt-in decision rests on the
ADR 0034 GPU measurement; a CPU re-confirmation arm on the corrected defaults
was attempted and aborted — the BGE v2-M3 sidecar needed ~11.5 s per rerank
call on the local CPU profile, making the full 3-run pack impractical, which
is consistent with keeping the CPU sidecar opt-in only (ADR 0031). A blind
human judge calibration is exported (39 cases,
[test/evaluation/judge-calibration/20260930-phi4-mini/](../test/evaluation/judge-calibration/20260930-phi4-mini/manifest.json))
and pending review; until it completes the judge's calibration status stays
`unreviewed`.

## Conversations and data management

Each conversation is an ordered list of question-and-answer turns. Editing a
turn removes that turn and everything after it, then runs the edited question
against the earlier conversation. This keeps the conversation timeline clear.

Chat history, indexed documents, and cached search results are separate types
of data. Deleting chats does not delete indexed documents. Resetting the
document index does not need to delete conversation history.

## What Nadir is designed for

Nadir is designed for private, document-grounded search on a single machine
or a small local network. It prioritizes understandable results, local data
control, and a simple operating model.

Retrieval and storage can be scaled separately when needed. Horizontal scaling
of live Chat streaming and concurrent Indexing requires a shared event backend
and coordination layer. See the [architecture guide](../AGENTS.md#architecture) for the current single-node
guarantees and the coordination needed for a distributed deployment.
