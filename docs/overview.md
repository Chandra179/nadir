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

Nadir is a document search and question-answering app. It retrieves passages
from your documents, uses them to answer questions, and saves the cited text
for later review. Its current release target is a Markdown technical-notes
companion for one person on their own machine.

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

The React dashboard supports imports, source inspection, follow-up questions,
edits, cancellation and saved conversations. Search combines meaning-based
and keyword retrieval. Similar questions can reuse cached passages; unchanged
documents are skipped on later imports.

Documents, questions and answers can stay local when all configured services
run locally. Reranking, contextual enrichment and PDF conversion are optional
and off by default. PDFs require the Docling sidecar and are outside the
current Markdown release target.

## Algorithms

### Chunking and source versions

The default recursive chunker uses Goldmark's Markdown syntax tree to retain
headings, short bold labels and source locations. It splits at paragraph,
line, sentence and word boundaries, with a Unicode-safe fallback. Defaults
are 512 runes per chunk and 64-rune overlap. Short split sections retain a
bounded full-section window for generation; a sentence-window chunker is
available as an experiment.

Each chunk receives a deterministic UUIDv5 over
`filePath:sourceSHA:lineStart:chunkIndex`. Replacements are staged before
activation; old versions are then deactivated and cleaned. Unchanged source
hashes are skipped, so changes to chunking, embedding inputs or enrichment
require a full reindex into a separate collection. Publication coordination
covers one API process.

### Dense and lexical retrieval

An embedding model converts questions and passages into 768-dimensional
vectors for meaning-based search. The lexical encoder lowercases and tokenizes
text, hashes terms with FNV-1a, and stores term-frequency sparse vectors.
Qdrant applies corpus IDF server-side.

Natural-question encoding drops common English function words while retaining
negation and numbers. Indexed documents and explicit keyword lookups retain
all terms. This is a **BM25-style TF/IDF signal**: the encoder does not implement
full BM25 term saturation or document-length normalization.

### Rank fusion and reranking

Reciprocal Rank Fusion (RRF) combines positions in the dense and lexical lists
without comparing their raw scores [1](#references). The default path uses
Qdrant's native fusion [2](#references). An opt-in local variant sums
`weight / (k + rank)` with one-based ranks and configured `k=60`, plus optional
exact-match and heading boosts. Its scoring semantics differ from the native
path and require a measured comparison before enabling.

An optional cross-encoder reads each leading passage together with the
question to reorder candidates. If reranking fails, retrieval keeps its
existing order.

### Semantic cache

The cache reuses retrieved passages for questions above a vector-similarity
threshold. Compatibility includes embedding configuration, TTL and a local
invalidation generation. Document publication suspends reuse and invalidates
outstanding writes, preventing delayed work from restoring an old corpus
result. Filtered searches and evaluations bypass the cache.

### Context and answer generation

The prompt builder admits passages in retrieval order, preserves complete
source labels and reserves an output budget. Token counts are estimates.
The answer model is instructed to use the admitted material; saved citations
retain that text and its source version after reindexing.

Narrow literal-comparison and citation-attribution rules handle explicit source
statements. Follow-up guards preserve a selected subject and decline conditions
documented only for a competing alternative. These rules cover specific
English and literal cases; they do not establish semantic support for arbitrary
answers. Answer quality still needs review against the cited evidence.

## Models and configuration

| Role | Default | Behavior |
|---|---|---|
| Embeddings | EmbeddingGemma 300M (`embeddinggemma-300m-q8:latest`) | 768 dimensions; changing the model or task prefixes requires a reindex |
| Answers | Gemma 3 4B (`gemma3:4b`) | Streamed, bounded output; no automatic model fallback |
| Follow-up rewriting | Gemma 3 1B (`gemma3:1b`) | Used for unresolved references; failure retains the original wording |
| Contextual enrichment | Gemma 3 1B (`gemma3:1b`) | Off by default; changing enrichment requires a reindex |
| Reranking | BAAI BGE reranker v2-M3 | Off by default; one request at a time |

Models are configured in
[`config.yaml`](https://github.com/Chandra179/nadir/blob/main/internal/bootstrap/configuration/config.yaml)
with environment overrides. Each enabled text-model role declares its own
Ollama endpoint and model. Readiness checks the index, embedder, any enabled
reranker and installed answer/rewrite-model metadata. Metadata does not prove
inference capacity; actual turns verify serving on the current hardware.

## Application architecture

Nadir is a modular monolith with ports and adapters [3](#references). Documents,
Retrieval and Conversation own separate responsibilities inside one Go
process. Core packages define their required interfaces and must not import
HTTP, provider or frontend implementations.

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

Documents owns intake, indexing and source versions. Retrieval owns search,
fusion, reranking and cache. Conversation owns sessions, history mutations,
generation and event replay. Bootstrap is the composition root shared by
`cmd/api` and `cmd/evaluator`.

Markdown parsing uses Goldmark; HTTP, cancellation, Unicode handling and slice
operations use Go's standard library. Source identity, context admission,
conversation revisions and evidence matching remain application policies.

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

Follow-ups use recent history and a persisted selected source section when
available. A selected subject bypasses LLM rewriting; unresolved references
may be rewritten for retrieval. Generation receives the original question
and bounded reference context separately. This resembles Rewrite-Retrieve-Read
[4](#references), using an installed local rewriter rather than the paper's
trained rewriting pipeline.

| Mechanism | Behavior and boundary |
|---|---|
| Supervised generation | Chat owns generation independently of the starting HTTP request. Closing the page detaches its observer; explicit cancellation saves a partial answer. Provider failure, timeout and shutdown can also stop work. |
| Replayable event log | Monotonic event IDs support replay followed by live events. Slow observers disconnect; an expired cursor receives a resync event. The log is bounded and lives in memory. |
| Revision validation | Editing removes the selected turn and all later turns before answering against earlier history. Edits and deletions cancel affected work; revision checks under a local mutex prevent stale saves from recreating removed turns. |
| Source snapshots | Saved turns retain their admitted citation text and source hash independently of the current index. Chats, documents and cached retrieval results have separate deletion lifecycles. |

Revision validation applies the principle behind Optimistic Offline Lock
[5](#references) within one process. The HTTP edge uses SSE event IDs and
`Last-Event-ID` for reconnection [6](#references); Go's `context.AfterFunc`
detaches cancelled subscriptions [7](#references).

Successfully written history survives restarts. Live generation and replay
logs do not. Graceful shutdown attempts to save partial answers within a
timeout; an abrupt failure can lose the final write.

### Resource limits

Indexing uses a single-writer budget; destructive operations have a separate
finite queue. Document reset also coordinates with indexing. Queue timeouts
return capacity errors. Ollama schedules LLM and embedding work; the optional
reranker has a one-at-a-time client queue. These limits coordinate one API
process, not multiple instances.

## Evaluation

The evaluator CLI uses the API's Retrieval service and the production context
builder, literal-comparison path and citation correction. Browser streaming
and full conversation lifecycles are checked separately by API tests and app
review.

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

Evaluation bypasses the cache and requests `max(topK, 10)` results. Chunks are
matched to fixture source-path and text annotations. Duplicate annotations
collapse to one evidence identity with its highest grade; recall and nDCG
credit each identity once, so overlapping chunks cannot inflate gain.

| Metric | Definition |
|---|---|
| Hit Rate@k | Fraction of answerable questions with relevant evidence in the first k results |
| Recall@k | Mean fraction of each question's annotated evidence found in the first k |
| MRR@10 | Mean reciprocal rank of the first relevant result through rank ten; misses score zero |
| Graded nDCG@k | Gain `2^grade - 1`, discounted by `log2(rank + 1)` and normalized against the ideal annotated ranking |
| Distractor Hit Rate@k | Fraction of all questions with an annotated non-relevant distractor in the first k |

The ranked-metric definitions follow standard IR evaluation [8](#references);
canonical evidence matching is Nadir's fixture policy. Questions without
relevant annotations are separate abstention cases, excluded from answerable
denominators. Retrieval metrics alone do not measure whether an answer
correctly declines them.

Repeated runs retain their concrete rankings and latencies. Aggregate quality
is the median of dataset-level run metrics; latency percentiles pool actual
request samples and use linear interpolation. Reports preserve run order and
identify the representative run used for legacy ranking fields.

### Generation checks and provenance

Reports retain the answer method, complete answer, admitted context, citation
snapshots, judge response and failure class. The judge requires an explicit
endpoint, a model distinct from the answer model, and a suitability explanation.
Both models use temperature zero during evaluation. Judge output is bounded
and validated against a JSON score schema, with no automatic fallback.

The judge scores faithfulness, answer relevancy, context precision and context
recall, with separate abstention scoring. These dimensions relate to RAGAs
[9](#references), but Nadir uses a direct local judge prompt rather than that
framework's full metric procedures. The scores are not interchangeable, and
independent human calibration is required before calling them calibrated.

Provenance records fixture/configuration hashes, configured source-byte
fingerprints and observed model metadata. File fingerprints do not prove the
remote index contains those versions. The 133-question golden and 64-question
representative fixtures are synthetic regressions. Consented production gates
require independently reviewed schema-v3 evidence and are outside the personal
release target.

## Status and limits

The finite local engineering checklist passed on October 3, 2026 with the
installed defaults and agent-reviewed simulated questions. Owner usefulness
review and independent judge calibration remain pending. Maintained results,
latencies and migration/rollback instructions are in the project documentation
below.

The component composition is comparable to Haystack pipelines [10](#references);
Nadir uses Go interfaces and functions and has no Haystack or RAGAs runtime
dependency. Changes to scoring, fusion, reranking or models require a measured
gain on reproduced failures without breaking source identity, citation order,
cancellation or latency budgets.

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

- [Local acceptance](https://github.com/Chandra179/nadir/blob/main/docs/local-v1.md), [P1 evidence](https://github.com/Chandra179/nadir/blob/main/docs/p1-evidence.md) and [owner review](https://github.com/Chandra179/nadir/blob/main/docs/owner-review.md): measured results and remaining validation.
- [RAG failure research](https://github.com/Chandra179/nadir/blob/main/docs/rag-failure-research.md): observed failures and evaluated approaches.
- Design records: [event log](https://github.com/Chandra179/nadir/blob/main/docs/adr/0006-chat-streams-over-domain-owned-event-log.md), [capability seams](https://github.com/Chandra179/nadir/blob/main/docs/adr/0020-consumer-owned-capability-seams.md), [bounded contexts](https://github.com/Chandra179/nadir/blob/main/docs/adr/0022-bounded-context-layout.md) and [shared runtime](https://github.com/Chandra179/nadir/blob/main/docs/adr/0026-shared-runtime-composition.md).
- Implementation guides: [chunking](https://github.com/Chandra179/nadir/blob/main/internal/core/documents/chunking/README.md), [Chat](https://github.com/Chandra179/nadir/blob/main/internal/core/conversation/chat/README.md) and [evaluation](https://github.com/Chandra179/nadir/blob/main/internal/eval/README.md).
- [Fixtures and review instructions](https://github.com/Chandra179/nadir/blob/main/test/evaluation/README.md) and [report catalog](https://github.com/Chandra179/nadir/blob/main/test/evaluation/reports/README.md): retained evaluation inputs and evidence.
