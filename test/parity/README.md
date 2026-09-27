# Backend cutover parity evidence

`cmd/api` and `cmd/evaluator` now run the restructured backend. The old binaries, old source, all seven live Qdrant collection snapshots, aliases, and snapshot checksums are kept in the ignored `.local/rollback-2026-09-26/` directory on this host. The live Qdrant data was backed up before switching commands. This directory is operational rollback state and is not committed.

The comparison restored live snapshots to a separate Qdrant v1.19.1 instance on loopback ports 16333/16334, seeded one inactive retired document version, and ran only one backend at a time as a writer. Collection names, the `documents_chunks__active` alias, point IDs, payloads, cache entries, and history records were checked. `capture_api_contract.py` recorded statuses, content types, JSON shapes, SSE event names and IDs, and replay; the [old](fixtures/api-old.json) and [new](fixtures/api-next.json) captures match. The candidate also passed generated-answer, cancellation, replay-gap, failed replacement, reset, source mirroring, filter, cache, PDF adapter, enrichment, and shutdown tests.

The 133-query synthetic fixture gave equal hit rate (0.805), recall (0.788), MRR (0.658), and distractor rate (0.248) without reranking. Candidate nDCG was 0.682 versus 0.679 due to one fifth-rank result. This fixture is a regression check, not release evidence. The evaluator's `--require-release-gate` correctly rejects it.

## Load gate

The load driver counts JSON errors and SSE `generror`, not just HTTP status. It supports a sequential warmup per workload and records warmup failures. All matched runs used 30 requests at concurrency 8 against restored clone data, with the same Ollama and config settings for each pair. Reranking was disabled for these load pairs to avoid sidecar startup variance; the default reranker path was checked separately by readiness, retrieval smoke, and live dashboard E2E.

| Workload | Old p95 | New p95 | Old failures | New failures | Result |
| --- | ---: | ---: | ---: | ---: | --- |
| Warmed chat streams | 67.833 s | 68.229 s | 23 | 23 | Pass (+0.58%) |
| Warmed long retrieval | 0.942 s | 0.914 s | 0 | 0 | Pass (-3.03%) |
| Valid 8 KiB ingestion | 1.885 s | 2.006 s | 0 | 0 | Pass (+6.45%) |
| Cold chat streams, corrected workload | 69.781 s | 72.713 s | 18 | 18 | Pass (+4.20%) |
| Cold long retrieval | 0.867 s | 0.903 s | 0 | 0 | Pass (+4.06%) |
| Cold 8 KiB ingestion | 1.823 s | 1.863 s | 0 | 0 | Pass (+2.25%) |

The first non-warmed full-stack pair failed chat (+67.87% p95, three more semantic failures). A repeated no-warmup pair with the corrected ingestion payload passed all three workloads, as did a warmed chat pair and the warmed full pair. The early chat failure is retained as evidence of live Ollama/model-admission variance. The original 256 KiB ingestion generator caused Ollama status 400 and 30/30 file failures on both backends; `benchmark_load.py` now emits a bounded 8 KiB markdown document by default, and the successful ingestion pair above uses that corrected workload. The synthetic regression and one passing load pair do not establish production capacity. Raw aggregate results and notes are in [measurements.json](fixtures/measurements.json). Apply the comparison script to fresh reports with `python3 test/parity/compare_load.py old.json new.json`.

## HyPE and feature coverage

[HyPE off](fixtures/hype-off.json) and [HyPE on](fixtures/hype-on.json) measured one six-chunk document with live Ollama. Indexing took 125 ms off and 3,710 ms on; point growth was 6 versus 24. Query latency during ingest was sampled once off (240 ms) and three times on (780–1,710 ms), so no p95 claim is made. HyPE remains disabled by default. The current reranker sidecar passed default API readiness and live browser session/history/SSE flow. After command cutover, the normal API also passed live Qdrant readiness, generation through a terminal `done` SSE event, history persistence, and deletion of the test session; live history returned to its 29-point pre-smoke count. The live PDF browser case was skipped because Docling was unavailable; the converter's Go protocol tests passed. Dashboard typecheck, lint, unit, build, mocked E2E, and live session E2E passed. Go unit, integration, race, vet, and build checks passed after cutover.

## Rollback

Stop the new API before running `.local/rollback-2026-09-26/api-old`. The old source and evaluator binary are in the same directory. Verify all snapshot checksums with `sha256sum -c .local/rollback-2026-09-26/snapshots.sha256` before restoring Qdrant; restore only while both APIs are stopped. The Qdrant snapshots include all pre-cutover collections and `aliases.json` records their alias mapping. Never run old and new APIs concurrently as writers against the live collections.
