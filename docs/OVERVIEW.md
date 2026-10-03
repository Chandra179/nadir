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
| Answer generation | Gemma 3 4B (`gemma3:4b`) | streamed with bounded output; explicitly configured, with no automatic model fallback |
| Follow-up rewriting | Gemma 3 1B (`gemma3:1b`) | optional for unresolved references; selected subjects bypass it; failure retains the original wording |
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
   a follow-up question? ──▶ resolve subject from recent conversation
              │              optionally rewrite the retrieval query
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

Follow-up references use recent conversation and a persisted selected source
section when available. A selected subject bypasses optional LLM rewriting;
other unresolved references may be rewritten for retrieval. Generation keeps
the original question and receives bounded reference context separately. Only an
explicit cancel stops a running answer. Closing the page does not: the answer
finishes in the background and is saved, so reopening shows it from where you
left off. Editing an earlier question trims that branch of the conversation —
the edited question is answered against everything before it, and everything
after it is replaced. The conversation itself is stored, so history survives
a restart; an answer that was still being written is saved up to that point.

### Keeping heavy work orderly

Indexing has a single-writer budget. Destructive operations have a separate
finite budget; document reset is also coordinated with indexing by the
Documents lifecycle. Chat edits/deletions coordinate with their own in-flight
turns and may run while indexing proceeds. Requests that exhaust their queue
timeout fail with a capacity error. These are process-local guarantees.

Ollama's scheduler owns LLM and embedding concurrency. The optional reranker
has its own one-at-a-time client queue. These limits bound work on the current
machine; they do not coordinate multiple API instances.

### What "ready" means

Readiness checks the search index, the embedding model, any enabled reranker,
and installed metadata for each enabled answer/rewrite model at its own
endpoint. A missing model returns an installation hint. Model metadata does
not establish inference capacity or residency; actual turns verify serving
on the current hardware without readiness loading and swapping answer models.

## Current evidence (October 3, 2026)

The finite personal/local engineering checklist passes with the installed
Gemma 3 4B generator, Gemma 3 1B rewriter and EmbeddingGemma embedder. Reranking
and custom fusion remain off. No additional model was downloaded for the
accepted fixes.

| App workflow | Direct answer/source review |
|---|---|
| Daily, 40 saved questions | 30/30 supported, 5/5 declines, 5/5 follow-ups |
| Broader, 64 saved questions | 56/56 supported, 8/8 declines |
| Fragile cases, three repetitions | All 39 turns supported |

These are Codex reviews of simulated questions, not independent human labels
or proof of personal usefulness. The unchanged retrieval packs ran three times:
golden Hit@5 1.0000, MRR@10 0.8504, nDCG@5 0.8640; broader Hit@5 0.9464,
MRR@10 0.8229, nDCG@5 0.8544. Distractor exposure and arbitrary paraphrase
support remain limits. Sequential warm latency does not establish cold-load
performance or concurrent capacity.

See [LOCAL_V1.md](LOCAL_V1.md) for acceptance, measured latency, limitations and
the migrated 15-note corpus launch/rollback. [P1_EVIDENCE.md](P1_EVIDENCE.md)
distinguishes current checks from dated browser/lifecycle evidence.
[The report catalog](../test/evaluation/reports/README.md) indexes accepted
results, saved controls and historical experiments that support architecture
decisions. Older automated-judge scores remain uncalibrated; the
[39-case human-review packet](../test/evaluation/judge-calibration/20260930-phi4-mini/manifest.json)
is still pending. Ten saved owner questions against actual notes are the next
product validation step.

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
