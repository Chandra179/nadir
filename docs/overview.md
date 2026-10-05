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
updated: 2026-10-04
---

# Nadir: A Private-Document RAG Chat with Hybrid Search

Nadir searches documents, answers questions from retrieved passages, and saves
cited text for review. The current release targets one person working with
local Markdown technical notes.

## How it works

```text
Documents -> chunk + embed -> Qdrant
                               |
                               v
Question + history -------> retrieve
                               |
                               v
                        context + sources
                               |
                               v
                          answer -> browser
```

The React dashboard supports imports, source inspection, follow-ups, edits,
cancellation and saved conversations. Search combines semantic and keyword
retrieval. Similar questions can reuse cached passages, and imports skip
unchanged documents.

Documents, questions and answers stay local when all configured services run
locally. Reranking, contextual enrichment and PDF conversion are off by
default. PDF intake requires the Docling sidecar and is outside the current
release target.

## Algorithms

### Chunking and source versions

The default recursive chunker uses Goldmark's Markdown syntax tree to retain
headings, short bold labels and source locations. It splits on paragraph,
line, sentence and word boundaries, with a Unicode-safe fallback. Chunks
default to 512 runes with 64-rune overlap. Short split sections retain a bounded
full-section window for generation. A sentence-window chunker is experimental.

Each chunk gets a deterministic UUIDv5 from
`filePath:sourceSHA:lineStart:chunkIndex`. Replacements are staged before
activation; old versions are then deactivated and cleaned. Imports skip
unchanged source hashes. Changes to chunking, embedding inputs or enrichment
require a full reindex into a separate collection. Publication coordination
covers one API process.

### Dense and lexical retrieval

An embedding model maps questions and passages to 768-dimensional vectors for
semantic search. The lexical encoder lowercases and tokenizes text, hashes
terms with FNV-1a, and stores term-frequency sparse vectors. Qdrant applies
corpus IDF.

Natural-question encoding drops common English function words but keeps
negation and numbers. Indexed documents and keyword lookups keep all terms.
This is a **BM25-style TF/IDF signal**, without BM25 term saturation or
document-length normalization.

### Rank fusion and reranking

Reciprocal Rank Fusion (RRF) combines dense and lexical result ranks without
comparing raw scores [1](#references). The default uses Qdrant's native fusion
[2](#references). An opt-in local variant sums `weight / (k + rank)` using
one-based ranks and `k=60`, with optional exact-match and heading boosts. Its
scoring differs from Qdrant's and should be measured before use.

An optional cross-encoder reranks leading passages against the question. If it
fails, retrieval keeps the existing order.

### Semantic cache

The cache reuses retrieved passages when a question exceeds a similarity
threshold. Cache compatibility includes the embedding configuration, TTL and
a local invalidation generation. Publishing documents suspends reuse and
invalidates outstanding writes, so delayed work cannot restore stale results.
Filtered searches and evaluations bypass the cache.

### Context and answer generation

The prompt builder selects passages in retrieval order, preserves source
labels and reserves an output budget. Token counts are estimates. The answer
model is instructed to use the selected material. Saved citations retain the
cited text and source version after reindexing.

Literal-comparison and citation rules handle explicit source statements.
Follow-up guards preserve a selected subject and decline conditions documented
only for a competing alternative. These rules cover specific English and
literal cases; they do not verify support for arbitrary answers. Review answers
against the cited evidence.

## Models and configuration

| Role | Default | Behavior |
|---|---|---|
| Embeddings | EmbeddingGemma 300M (`embeddinggemma-300m-q8:latest`) | 768 dimensions; changing the model or task prefixes requires a reindex |
| Answers | Gemma 3 4B (`gemma3:4b`) | Streamed, bounded output; no automatic model fallback |
| Follow-up rewriting | Gemma 3 1B (`gemma3:1b`) | Used for unresolved references; failure retains the original wording |
| Contextual enrichment | Gemma 3 1B (`gemma3:1b`) | Off by default; changing enrichment requires a reindex |
| Reranking | BAAI BGE reranker v2-M3 | Off by default; one request at a time |

Configure models in
[`config.yaml`](https://github.com/Chandra179/nadir/blob/main/internal/bootstrap/configuration/config.yaml)
with environment overrides. Each enabled text-model role has its own Ollama
endpoint and model. Readiness checks the index, embedder, enabled reranker and
answer/rewrite model metadata. Metadata does not prove inference capacity;
actual turns check serving on the current hardware.

## Runtime and hardware profiles

Both local workflows run on one workstation. `./scripts/local.sh` runs the API
and optional reranker on the host, with Qdrant in Podman. Podman Compose runs
the API, Qdrant and optional reranker in containers. Both connect to Ollama on
the host; the dashboard runs separately. See the
[Compose deployment guide](../deploy/compose/README.md) for setup instructions.

```text
Dashboard (browser)
       |
       v
API [host: local.sh | Podman: Compose] ----> Qdrant [Podman]
       |                                      (both workflows)
       +-----------------------------------> Ollama [host]
       +-----------------------------------> Reranker [optional]
                                                host: local.sh
                                                Podman: Compose
                                                CPU default; NVIDIA GPU overlay
```

The reranker is off by default and uses CPU when enabled. NVIDIA GPU reranking
uses an opt-in Compose overlay on Linux or Windows WSL2 with NVIDIA CDI. It
shares GPU memory with Ollama, so measure capacity before enabling it. On
Apple Silicon, keep reranking on CPU; Ollama can use Metal.

Indexing and destructive-operation coordination, along with live generation
events, are local to each API process. Run one API instance; multiple instances
won't coordinate operations or share live events.

## Application architecture

Nadir is a modular monolith with ports and adapters [3](#references). Documents,
Retrieval and Conversation run in one Go process and own separate
responsibilities. Core packages define their interfaces and do not import
HTTP, provider or frontend code.

```text
Browser (React + TypeScript + Vite)
                 |
                 | JSON requests + SSE
                 v
HTTP edge (Go net/http)
                 |
                 v
Core: Documents | Retrieval | Conversation
                 |
                 | calls through core-owned interfaces
                 v
Providers: Qdrant | Ollama | reranker | Docling

Bootstrap wires the API and evaluator from these components.
```

Documents handles intake, indexing and source versions. Retrieval handles
search, fusion, reranking and cache. Conversation handles sessions, history,
generation and event replay. Bootstrap wires the API and evaluator from shared
components.

Goldmark parses Markdown. Go's standard library handles HTTP, cancellation,
Unicode and slice operations. The application defines source identity, context
admission, conversation revisions and evidence matching.

### Retrieval flow

```text
Question
   |
   v
Compatible cache hit? -- yes --> cached passages
   |
   no
   v
Split into sub-questions when applicable
   |
   v
Dense + lexical search -> rank fusion
   |
   v
Limit passages per document -> optional reranking
   |
   v
Final passages -> cache when eligible
```

### Conversation lifecycle

Follow-ups use recent history and, when available, a saved source section. A
selected subject bypasses rewriting; unresolved references may be rewritten
for retrieval. Generation receives the original question and bounded
reference context separately. This follows Rewrite-Retrieve-Read
[4](#references) with a local rewriter.

| Mechanism | Behavior and boundary |
|---|---|
| Supervised generation | Chat runs generation independently of the HTTP request. Closing the page detaches its observer; explicit cancellation saves a partial answer. Provider failure, timeout or shutdown can also stop generation. |
| Replayable event log | Monotonic IDs support replay and live events. Slow observers disconnect; expired cursors receive a resync event. The bounded log is in memory. |
| Revision validation | Editing removes the selected turn and later turns before answering from earlier history. Edits and deletions cancel affected work; a local mutex prevents stale saves from restoring removed turns. |
| Source snapshots | Saved turns retain citation text and source hashes across reindexing. Chats, documents and cached results have separate deletion lifecycles. |

Within one process, revision validation follows Optimistic Offline Lock
[5](#references). SSE event IDs and `Last-Event-ID` support reconnection
[6](#references). Go's `context.AfterFunc` detaches cancelled subscriptions
[7](#references).

Written history survives restarts; live generation and replay logs do not.
Graceful shutdown tries to save partial answers before a timeout. An abrupt
failure can lose the final write.

### Resource limits

Indexing has a single-writer budget; destructive operations use a separate
finite queue. Reset coordinates with indexing, and queue timeouts return
capacity errors. Ollama schedules LLM and embedding work. The optional
reranker has a one-at-a-time client queue. These limits apply to one API
process.

## Evaluation

The evaluator CLI uses the API's Retrieval service, context builder,
literal-comparison path and citation correction. API tests and app review check
browser streaming and full conversation lifecycles separately.

```text
API conversation --------------+
                               |
Fixture -> Evaluator ----------+
                               |
                               v
                       Shared Retrieval
                               |
                 +-------------+-------------+
                 |                           |
                 v                           v
         Retrieval metrics          Shared prompt + citations
                 |                           |
                 |                           v
                 |                 Answer model or literal path
                 |                           |
                 |                           v
                 |                     Explicit judge
                 |                           |
                 +-------------+-------------+
                               |
                               v
                    JSON report + provenance
                               |
                               v
                 Owner review + judge calibration
```

### Retrieval metrics

Evaluation bypasses the cache and requests `max(topK, 10)` results. It matches
chunks to fixture source-path and text annotations. Duplicate annotations
collapse to one evidence identity at the highest grade. Recall and nDCG credit
each identity once, so overlapping chunks do not inflate gain.

| Metric | Definition |
|---|---|
| Hit Rate@k | Fraction of answerable questions with relevant evidence in the first k results |
| Recall@k | Mean fraction of each question's annotated evidence found in the first k |
| MRR@10 | Mean reciprocal rank of the first relevant result through rank ten; misses score zero |
| Graded nDCG@k | Gain `2^grade - 1`, discounted by `log2(rank + 1)` and normalized against the ideal annotated ranking |
| Distractor Hit Rate@k | Fraction of all questions with an annotated non-relevant distractor in the first k |

Ranked metrics follow standard IR evaluation [8](#references); canonical
evidence matching is Nadir's fixture policy. Questions without relevant
annotations are abstention cases and are excluded from answerable denominators.
Retrieval metrics do not measure whether an answer declines them correctly.

Reports retain rankings and latencies for each run. Aggregate quality is the
median of dataset-level metrics. Latency percentiles pool request samples and
use linear interpolation. Reports preserve run order and identify the
representative run used for legacy ranking fields.

### Generation checks and provenance

Reports retain the answer method, answer, admitted context, citation snapshots,
judge response and failure class. The judge requires an explicit endpoint, a
model distinct from the answer model, and a suitability explanation. Both
models use temperature zero. Judge output is bounded and validated against a
JSON score schema; there is no automatic fallback.

The judge scores faithfulness, answer relevancy, context precision and context
recall, plus abstention. These dimensions relate to RAGAs [9](#references), but
Nadir uses its own local judge prompt. The scores are not interchangeable with
RAGAs metrics and need independent human calibration.

Provenance records fixture and configuration hashes, source-byte fingerprints
and observed model metadata. File fingerprints do not prove the index contains
those versions. The 133-question golden and 64-question representative fixtures
are historical synthetic inputs; their source corpus has been removed.
Production gates require consented, independently
reviewed schema-v3 evidence and are outside the personal release target.

## Status and limits

The local engineering checklist passed on October 3, 2026 with installed
defaults and agent-reviewed simulated questions. Usefulness review remains
pending, and automated judge scores remain uncalibrated. The old calibration
packet was retired with its missing source report. See project docs below for
results, latencies and migration/rollback instructions.

The component composition resembles Haystack pipelines [10](#references), but
Nadir uses Go interfaces and functions and does not depend on Haystack or RAGAs
at runtime. Evaluate changes to scoring, fusion, reranking or models against
reproduced failures while preserving source identity, citation order,
cancellation and latency budgets.

## References

1. [Cormack, Clarke and Buettcher: Reciprocal Rank Fusion (SIGIR 2009)](https://plg.uwaterloo.ca/~gvcormac/cormacksigir09-rrf.pdf) - the rank-fusion method.
2. [Qdrant: Hybrid queries](https://qdrant.tech/documentation/search/hybrid-queries/) - native fusion and hybrid search.
3. [Alistair Cockburn: Hexagonal architecture](https://alistair.cockburn.us/hexagonal-architecture) - ports and adapters.
4. [Ma et al.: Query Rewriting for Retrieval-Augmented Large Language Models](https://arxiv.org/abs/2305.14283) - Rewrite-Retrieve-Read.
5. [Martin Fowler: Optimistic Offline Lock](https://martinfowler.com/eaaCatalog/optimisticOfflineLock.html) - validation before committing changes.
6. [WHATWG: Server-sent events](https://html.spec.whatwg.org/multipage/server-sent-events.html#the-last-event-id-header) - event IDs and reconnection.
7. [Go: context.AfterFunc](https://pkg.go.dev/context#AfterFunc) - cancellation callbacks.
8. [Stanford IR textbook: Evaluation of ranked retrieval results](https://nlp.stanford.edu/IR-book/html/htmledition/evaluation-of-ranked-retrieval-results-1.html) - ranked retrieval metrics.
9. [Es et al.: RAGAs, Automated Evaluation of Retrieval Augmented Generation](https://aclanthology.org/2024.eacl-demo.16/) - RAG evaluation dimensions.
10. [Haystack: Pipelines](https://docs.haystack.deepset.ai/docs/pipelines) - a component-pipeline architecture comparison.

### Project documentation

The [documentation directory](https://github.com/Chandra179/nadir/tree/main/docs)
contains the maintained guides and design records.

- [Report catalog](../test/evaluation/reports/README.md) and [TODO](../TODO.md): dated results and remaining usefulness validation.
- Design records: [event log](https://github.com/Chandra179/nadir/blob/main/docs/adr/0006-chat-streams-over-domain-owned-event-log.md), [capability seams](https://github.com/Chandra179/nadir/blob/main/docs/adr/0020-consumer-owned-capability-seams.md), [bounded contexts](https://github.com/Chandra179/nadir/blob/main/docs/adr/0022-bounded-context-layout.md) and [shared runtime](https://github.com/Chandra179/nadir/blob/main/docs/adr/0026-shared-runtime-composition.md).
- Implementation guides: [chunking](https://github.com/Chandra179/nadir/blob/main/internal/core/documents/chunking/README.md), [Chat](https://github.com/Chandra179/nadir/blob/main/internal/core/conversation/chat/README.md) and [evaluation](https://github.com/Chandra179/nadir/blob/main/internal/eval/README.md).
- [Fixtures and review instructions](https://github.com/Chandra179/nadir/blob/main/test/evaluation/README.md) and [report catalog](https://github.com/Chandra179/nadir/blob/main/test/evaluation/reports/README.md): retained evaluation inputs and evidence.
