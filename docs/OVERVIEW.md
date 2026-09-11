---
title: "Nadir Overview"
description: "A simple overview of Nadir's purpose, features, and search algorithms"
tags: [overview, rag, search]
---

# Nadir Overview

## What is Nadir?

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

## Algorithms in simple terms

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

For a larger multi-machine deployment, the search and storage services can be
scaled separately. Live answer streaming also needs a shared event backend so
that users can receive the same conversation from any application instance.
