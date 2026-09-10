---
title: "Nadir"
description: "Nadir RAG Search Engine with Qdrant, LLM"
tags: [system-design, llm, rag]
links:
  github: "https://github.com/Chandra179"
created: 2026-09-08
---

# Nadir Architecture

Nadir is a single-node RAG search engine with conversational retrieval.

## Big Picture


## Server

One Go binary: HTTP API, chat use-case, search and ingest pipelines, and the
composition root that wires everything together. No microservices.

## Dashboard

An htmx + Alpine chat UI: templates in `dashboard/` are embedded via
`go:embed` and parsed once at startup (no markup in Go source); it calls the
same HTTP API as curl and receives answers over plain EventSource (SSE).

## Vector store

All state lives in Qdrant (indexed chunks, semantic cache, chat history —
separate collections): self-hosted, dense + sparse with server-side fusion
in one query, no extra infrastructure.

## LLM service

Using Ollama service for embeddings, answer generation, enrichment, and
query rewriting — fully private and offline-capable.

## Ingest & chunking

Chunking using markdown headings (paragraph/sentence boundaries, hard character split as fallback).

## Embeddings

Chunks are embedded with `nomic-embed-text`, with a document prefix at
ingest and a query prefix at search time, so the model distinguishes
document from search-query representations.

## Retrieval

Using hybrid search (dense + sparse, fused with RRF) each runs dense (semantics) and BM25
sparse (exact terms) legs in parallel, fused server-side with RRF to avoid
calibrating incompatible scores, then merged by best score with a per-file
cap so one document can't crowd out others.

## Re-ranking

Top candidates are re-scored by a swappable cross-encoder (Python); only a small candidate set is
reranked, so latency stays low.

## Semantic cache

Near-repeat questions hit a similarity-thresholded query-level cache in the vector store collection, skipping re-retrieval; it is cleared on ingest and full reset.

## Query rewriting

Follow-up turns are rewritten into standalone search queries over Ollama
(Rewrite-Retrieve-Read, feature-flagged, gated on chat history);
best-effort — a rewrite failure searches the raw query. The rewriter's
endpoint and model are explicit configuration; it does not inherit another
LLM role's settings.

## Answer generation

Chunks are assembled into a citation-constrained prompt with the best chunks
in the middle, fit to a token budget, so answers stay faithful and
attributable to indexed documents. grounded RAG with lost-in-the-middle ordering

## Chat history

Each turn (query, rewritten query, chunks, prompt, answer, errors, timing)
is persisted per session, enabling conversation continuity and a reviewable
trace; sessions are listed in the sidebar and can be deleted individually.
The Settings menu also provides a guarded delete-all action that clears only
the chat-history collection; indexed documents and the semantic cache remain
untouched.
Editing a prior question prunes that turn and the later tail in the same
session, then runs the replacement against the retained prefix.

## Index-time enrichment

Optional ingest-time passes over Ollama: HyPE generates hypothetical user
questions, contextual writes a short situational intro — both one-time per
chunk and off the query path, closing the gap between how documents read
and how users ask.

## Docling

A Python service converts PDFs to Markdown so they can be ingested (the
Python ecosystem isn't vendored into the Go binary). The Go document-intake
Adapter calls the optional sidecar before the normal chunk → embed → replace
indexing pass, preserving the original source path for citations.

## Composition and seams

`internal/server` is the composition root. It constructs each domain Module
once, passes the resulting Adapters through `DependenciesConfig`, and then
builds the HTTP transport. Retrieval owns the caller-facing request, filter,
and result values; the Qdrant store and reranker keep their storage-side
representations behind that seam. Ingest separates per-document planning from
replacement commit, and `internal/qdrantutil` owns only shared Qdrant clients,
dense collection setup, primitive payload codecs, and point-ID decoding.

The chat use-case owns generation supervision and the bounded ordered replay
broker. SSE subscribers are transport concerns: disconnecting a subscriber
does not cancel generation. The broker is intentionally process-local and
single-node; horizontal deployment requires an external ordered event backend
and a subscriber-routing/affinity decision.

## Configuration contract

`config/config.yaml` is decoded with unknown-field rejection, environment
overrides are parsed strictly, and production defaults are applied once by
`config.Config.Validate`. Enabled LLM roles must provide their own endpoint
and model: generator, rewriter, HyPE, and contextual enrichment never inherit
another role's values. Role-specific request timeouts are part of the config
contract. Changing embedding prefixes, dimensions, or enrichment flags
requires a reindex.

## Operational signals

Structured stage logs record duration, outcome, and bounded error labels for
ingest planning/commit, document and query embedding, Retrieval, reranking,
generation, semantic-cache reads/writes, replay gaps, broker rejection, and
Docling conversion. Request logging remains at the HTTP seam and never logs
request or response bodies.

## HTTP surface

| Method | Path | Description |
|--------|------|-------------|
| POST | `/ingest` | Ingest multipart uploads or configured source files |
| POST | `/store/reset` | Drop and recreate the document collection and clear semantic cache |
| GET | `/retrieval` | Render the dashboard |
| POST | `/retrieval/search` | Start one Retrieval/chat turn |
| GET | `/retrieval/turns/:id/events` | Replay/stream turn events over SSE |
| POST | `/retrieval/turns/:id/cancel` | Cancel generation and retain the partial answer |
| GET | `/history/sessions` | List persisted chat sessions |
| GET | `/history/sessions/:id` | Render one persisted session |
| DELETE | `/history/sessions/:id` | Delete one persisted session |
| DELETE | `/history/sessions` | Delete all persisted chat sessions and turns |
| GET | `/healthz` | Liveness check |

## Verification

The default unit scope is `./config ./cmd/... ./internal/...`; the Makefile
provides `test`, `race`, `vet`, `build`, and `check` targets. Adapter tests
exercise HTTP status, malformed response, timeout, cancellation, response
shape, and stream-closure contracts. Qdrant collection/schema and persistence
behaviour are covered by the clearly marked `integration` test suite:
`go test -tags integration ./internal/store`.
