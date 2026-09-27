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
  contextual descriptions or hypothetical questions during indexing.
- **PDF support** — PDFs can be converted to searchable text when conversion
  support is enabled.

## Algorithms

### Chunking

Large documents are divided at headings, paragraphs, and sentence boundaries.
If a section is still too large, it is split into bounded pieces. This gives
the search engine focused passages without losing the document structure.

### Embeddings

An embedding model converts each passage and question into a numerical vector.
Texts with similar meaning produce vectors that are close together, enabling
meaning-based search.

### BM25 keyword search

BM25 scores how well the exact words in a question match each passage. It is
especially useful for names, commands, product terms, formulas, and numbers.

### Reciprocal Rank Fusion

Semantic search and BM25 produce separate ranked lists. Reciprocal Rank Fusion
combines their positions rather than comparing incompatible score values. A
passage that ranks well in either list can therefore contribute to the final
result.

### Reranking

The first search stage retrieves a broader set of candidates quickly. An
optional cross-encoder then reads the question and each leading passage
together and gives them a more precise relevance order.

### Semantic cache

Nadir compares a new question with cached questions using vector similarity.
When the meaning is close enough, it can reuse the previous retrieval result
instead of repeating the full search.

### Grounded generation

The selected passages are placed into a prompt with their source context. The
language model is instructed to answer from that material, which reduces
unsupported claims and makes the result easier to verify.

## Under the hood

Documents flow through one pipeline when they are added; questions flow
through another when they are asked. A small amount of bookkeeping keeps the
heavy work of each from colliding. None of this changes how Nadir is used —
it explains why it stays fast and predictable.

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

Nadir distinguishes merely running from truly ready. It reports ready only
when the search index, the embedding model, and — if enabled — the reranker
have each been checked and are actually working, and startup tooling watches
that signal, so the app never looks healthy while a dependency is quietly
broken.

## Current evidence

These are the latest engineering measurements. The 133-query fixture uses the
sample documents and synthetic user-intent queries, so it is a regression
signal rather than production release evidence.

| Area | Latest result |
|---|---|
| Hybrid Retrieval, no reranker | HitRate@5 **0.805**, MRR@10 **0.657**, p50/p95 **19/23 ms**  |
| BGE reranker on CPU | MRR@10 **0.697**, nDCG@5 **0.709**; rerank p50/p95 **9.4/18.6 s**  |
| BGE reranker on GPU (laptop RTX) | Same quality: MRR@10 **0.697**, nDCG@5 **0.709**; rerank p50/p95 **0.44/0.70 s** (≈21× faster); peak VRAM about **3.7 GiB**  |
| BGE reranker quantized to 8-bit integers on CPU | Quality matched or beat the full-precision reranker (MRR@10 **0.725**, nDCG@5 **0.731**); rerank p50/p95 **5.4/9.7 s** — only about 1.7× faster than full precision, so the portable default stays full precision ([report](../test/evaluation/reports/e2e-podman-rerank-onnx-int8-20260927.json))  |
| EmbeddingGemma experiment | HitRate@5 **0.932**, MRR@10 **0.735**, p50/p95 **98/118 ms**; default remains Nomic pending release-gated evidence  |
| Answer-quality judge, before the grounding fix | 129/133 queries evaluated: faithfulness **0.485**, answer relevancy **0.780**, context precision/recall **0.615/0.622**; 4 failures ([the generation triage report](../test/evaluation/reports/generation-triage-20260916.json))  |
| Answer-quality judge, after grounding fix + answer-shape recalibration (live reruns) | Pre-fix baseline: faithfulness **0.485**, relevancy **0.780**. Now: all 133 evaluated with zero failures — faithfulness **0.690**, relevancy **0.733**, context precision/recall **0.689/0.736**. Grounding rose sharply once answers stopped padding, and answer shape now matches the question; the residual relevancy gap tracks the queries where Retrieval misses ([report](../test/evaluation/reports/e2e-podman-generation-promptfix-20260927.json))  |
| PDF intake | 18/18 successful conversions, p50/p95 **2.62/25.94 s**, peak RSS about **3.28 GiB**  |

The results show that Retrieval is fast without reranking, while reranking and
PDF conversion are the main latency and resource costs. The default models and
fusion policy remain conservative until consent-safe, expert-judged production
data is available.

An ARQMath Task 1 candidate pack is available for the next evidence step. It
selects 40 public math questions from each 2020–2022 edition and records the
source hashes and fixed candidate pools. It is not release evidence yet: the
full licensed corpus, independent human judgments, adjudication, and
privacy/legal approval are still required.

The generation baseline found two answer timeouts, two judge-contract failures,
and a header-only context-evaluation miss. Generation now includes section
headers in context and matching, bounds answer output, and requires a
structured bounded judge response. A post-change live generation measurement
is still pending because the local search-index and model services were unavailable;
the baseline scores above are not post-change results.

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
