# TODO

Retrieval-accuracy roadmap (audit + research cross-reference, Aug 2026; items
re-audited and research pass re-verified Sep 2026). Each change is measured
against the golden set (HitRate@k / Recall@k / MRR@10 / nDCG@k) before and after.

## Measured results (34-query golden set over `samples/`, top_k=5)

| Stage | HitRate@5 | MRR@10 | nDCG@5 | p50 latency |
|---|---|---|---|---|
| Baseline (MiniLM reranker, un-prefixed index) | 0.824 | 0.689 | 0.742 | 201ms |
| + nomic prefixes + BM25 contextual align | 0.853 | 0.707 | 0.762 | 190ms |
| + bge-reranker-v2-m3 sidecar | **0.882** | **0.824** | **0.857** | 3206ms |
| same, torch-int8 quantized (Sep 2026 A/B) | 0.824 | 0.765 | 0.798 | 3372ms |
| same, no-rerank reference | — | 0.740 | 0.784 | 49ms |
| HyPE enabled on toy corpus | 0.882 | 0.804 | 0.823 | ~flat |

Reports in `tests/eval/reports/`. Rerank CPU latency (~3.2s p50) exceeds the
1–2s budget → quantized/base model swap listed under Phase 3.

## Phase 0 — Eval foundation ✅

- [x] Golden set: `tests/eval/golden.json` (34 queries over `samples/`) with
      committed run reports in `tests/eval/reports/`
- [ ] Grow golden set to 100+ queries as real corpus grows; keep distractor pairs

## Phase 1 — Cheap wins ✅

- [x] Swappable reranker model: `reranker.model` in config.yaml → `RERANKER_MODEL`
      env → sidecar. Default upgraded `ms-marco-MiniLM-L-6-v2` (~60 BEIR nDCG@10,
      can *hurt* vs no rerank per NVIDIA benchmark) → `BAAI/bge-reranker-v2-m3`
      (~71.5 BEIR, MIT). Revert via config; compose memory 1g→3g.
      Caveat: the Go client never pushes `reranker.model` to the sidecar — the
      loaded model is agreed via the shared `RERANKER_MODEL` env/default only.
- [x] nomic task prefixes at call sites: `embedder.query_prefix` ("search_query: ")
      on query fragments, `embedder.document_prefix` ("search_document: ") on
      ingest embeds. Empty string disables (any-model swappable).
- [x] BM25 sparse leg now indexes the same contextual text the dense leg
      embeds (path > header + body), instead of bare chunk text.
- [x] **Requires one reindex** after pulling: drop collection, re-ingest
      (`curl -X DELETE localhost:6333/collections/documents_chunks`).

## Phase 2 — Index-time LLM enrichment ✅ (flags default OFF)

Zero query-time latency; one-time Ollama cost per chunk at ingest. Enabling
after a prior ingest requires a reindex.

- [x] `internal/enrichment` — `Enricher` interface (in `interface.go`, the
      single definition; ingest imports it), Ollama chat client, lenient
      JSON parsing, graceful per-chunk degradation (warn + index without
      enrichment)
- [x] HyPE feature flag `enrichment.hype.enabled` (+ `questions_per_chunk`,
      default 3): hypothetical questions embedded as extra sibling points that
      carry the parent's identity fields → existing Key() dedup collapses them;
      DeleteByFile sweeps them; point IDs get `:hype:<n>` suffix
- [x] Contextual retrieval flag `enrichment.contextual.enabled`: LLM-written
      situational intro prepended before embedding/indexing
- [x] Env overrides `HYPE_ENABLED`, `CONTEXTUAL_ENABLED`
- [x] A/B on toy corpus: neutral/slightly negative (tiny clean corpus, no
      context fragmentation for it to fix; gemma3:1b question noise). Keep OFF
      until a real corpus exists; re-eval there — paper gains (+20pp precision)
      show on fragmented/larger corpora.

## Phase 2.5 — Production hardening ✅

- [x] Bounded, insertion-ordered turn retention: the in-process broker keeps a
      bounded number of turn streams and replay bytes, evicts finished streams
      deterministically, and emits an explicit resync event when a cursor is
      older than the retained window. If all retained streams are active, a new
      generation is rejected instead of growing memory without bound.
- [x] Request and ingest budgets: bound upload bodies, source-file reads,
      query length, sentence fragments, concurrent fragment searches, embedding
      batches, chunks per file, and `top_k`.
- [x] Semantic-cache correctness: filtered searches bypass the unfiltered
      query cache, cache entries carry an embedding/configuration version, and
      cache hits are bounded to the requested result count.
- [x] Startup and schema safety: Qdrant vector schemas are validated rather
      than silently reused, startup failures are returned to `main`, and
      collection setup has a timeout.
- [x] History concurrency and pagination: turn writes/deletes are serialized
      through one bounded seam and history reads use paged Qdrant scans.
- [x] Cleanup and documentation: removed tracked build artifacts, aligned
      source discovery and local-run documentation, and recorded the retention
      decision in ADR-0013.
- [x] Portable Compose deployment: the base stack is CPU-safe with a source
      bind mount, container healthcheck, `.dockerignore`, and an explicit
      NVIDIA GPU override for Linux/Windows WSL2; Apple Silicon skips the AVX2
      quantized bake.

## Phase 2.6 — Architecture deepening ✅

- [x] Centralized explicit LLM endpoint configuration and optional Docling
      configuration in the validated config module; enabled roles no longer
      inherit another role's address or model.
- [x] Retrieval-owned request/result types keep chat and the HTTP transport
      independent from storage-specific chunk and filter types.
- [x] Split the indexing pass into per-file planning and replacement commit
      stages while preserving bounded workers, retries, and cache invalidation.
- [x] Shared Qdrant clients, collection provisioning, payload codecs, and
      point-ID decoding live in `internal/qdrantutil`; document, history, and
      semantic-cache lifecycles remain separate.
- [x] Wired optional PDF document intake through the Docling HTTP Adapter for
      uploads and configured source paths; the original PDF path remains the
      source identity.

## Priority backlog

Work from the top down. P0 protects correctness and keeps the Go test command
reproducible. P1 deepens the Retrieval and Adapter seams. P2 addresses
durability and lifecycle concerns after the behaviour is covered. P3 contains
measured performance and product experiments.

### P0 — Correctness and test foundation

- [x] Keep HyPE and contextual enrichment independently gated. Contextual-only
      indexing must not make HyPE LLM calls or create HyPE sibling points.
- [x] Add ingest regression tests for both flags disabled, HyPE only,
      contextual only, and both enabled.
- [x] Add Retrieval behaviour tests for fragment batching/prefixes,
      per-file caps, semantic-cache filtering, and bounded reranker fallback.
- [x] Add reproducible `make test`, `make race`, `make vet`, `make build`, and
      `make check` targets scoped to Go packages, excluding a local Python
      `venv/` from `go test ./...` discovery.

### P1 — Architecture and operational confidence

- [x] Construct `internal/search` once from `DependenciesConfig`; move the
      reranker, candidate multiplier, and semantic cache into that config and
      remove post-construction `With*` mutators.
- [x] Add HTTP contract tests for the embedder, generator, reranker, rewriter,
      enrichment, and document-intake Adapters: status errors, malformed JSON,
      timeouts, cancellation, response-shape mismatches, and stream closure.
- [x] Add store, cache, Qdrant utility, chunker, middleware, and composition-root
      tests. Use fake Adapters for unit tests and a clearly marked Qdrant
      integration test suite for collection/schema and persistence behaviour.
- [x] Add per-stage observability: ingest, embedding, Retrieval, reranking,
      generation, cache hits/misses, replay gaps, broker rejection, and Docling
      conversion. Record durations, outcomes, and bounded error labels.
- [x] Harden configuration: reject malformed environment values, reject unknown
      YAML fields, centralize production defaults, and keep role-specific
      endpoints explicit. Update stale architecture/configuration documentation.

### P2 — Durability and lifecycle

- [ ] Make Document replacement failure-safe. The current delete-then-upsert
      sequence can temporarily remove a Document when the replacement upsert
      fails. Define a versioned replacement/cleanup protocol and record it in an
      ADR before changing the Store seam.
- [ ] Make detached chat-history persistence drainable during shutdown so a
      process stop cannot silently lose completed Chat turns.
- [ ] Measure PDF document-intake latency, memory, timeout, and failure behaviour
      against real documents in a production-like environment.
- [ ] Add end-to-end HTTP tests for ingest, Retrieval, SSE replay/cancellation,
      history listing/deletion, full reset, and error responses.

### P3 — Measured performance and Retrieval quality

- [~] Finish the reranker benchmark on a machine with enough memory to bake the
      `bge-reranker-v2-m3` ONNX artifact. Compare quality, p50/p95 latency, and
      memory against the current torch-int8 route. Existing measurements remain
      in the committed evaluation reports.
- [ ] Grow the golden set from 34 to 100+ real queries with distractor pairs,
      multi-hop cases, and generation-faithfulness annotations.
- [ ] Add adaptive Retrieval fallback only after confidence, filter-miss, and
      unsupported-answer telemetry exists; measure coverage against noise.
- [ ] Add generation-side evaluation for faithfulness, answer relevancy, and
      context precision/recall using a judge model larger than the model under
      test.
- [ ] Revisit Qdrant quantization and embedder replacement only when corpus size
      or golden-set recall demonstrates a capacity or quality ceiling.
- [ ] Benchmark sentence-window versus recursive chunking before changing the
      default chunker.

### Deliberately deferred

- [ ] Add a shared event backend such as Redis Streams only when multiple Nadir
      instances are deployed. It then also requires subscriber routing and an
      SSE affinity/deployment decision; the current broker remains intentionally
      single-node under ADR-0013.
- [ ] CRAG, speculative RAG, Adaptive-RAG routing, GraphRAG, and multi-query
      expansion remain research candidates, not implementation priorities.

## Rejected (research says skip for this domain)

- HyDE / multi-query expansion — underperforms vanilla hybrid retrieval for
  precise numeric/entity queries (T2-RAGBench 2026); fragment-splitting
  already captures part of the benefit
- GraphRAG / RAPTOR — heavy machinery, unjustified at current corpus size
- Late chunking — efficiency win but sacrifices relevance/completeness vs
  contextual retrieval (arXiv 2504.19754); needs long-context pooling support
