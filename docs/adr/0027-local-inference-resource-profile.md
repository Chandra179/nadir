# 0027 — Explicit local inference resource profile

- **Status:** Accepted
- **Date:** 2026-09-13
- **Deciders:** Chandra, Codex architecture review

## Context

The API process can call Ollama for embedding, rewriting, enrichment, and
streaming generation. Retrieval and indexing can also call the reranker
sidecar. The previous per-request and per-Adapter limits did not express the
combined local hardware budget: concurrent users could load several models or
overlap GPU work even when each individual request was bounded.

Automatic device selection also made the local runtime's hardware behavior
implicit. A laptop deployment needs a portable default and a finite admission
policy before model quality or throughput is tuned.

## Decision

The shipped `inference.profile: local` is conservative and explicit:

- one process-local Ollama Gate is shared by embedding, rewriting, enrichment,
  and generation;
- queued Ollama work has a finite wait and every Ollama request sends a finite
  `keep_alive` value;
- reranker calls use a separate bounded Gate and the sidecar also rejects work
  above its configured concurrency;
- the reranker defaults to CPU with the portable `torch` backend; and
- `RERANKER_DEVICE=auto` is not accepted by the local profile. CUDA requires
  the explicit `RERANKER_DEVICE=cuda` and `RERANKER_BACKEND=torch` settings, and
  the sidecar reports not-ready when CUDA is unavailable instead of falling
  back silently.

`inference.profile: custom` permits an operator to choose different limits or
automatic device selection after measuring the hardware. These controls are
process-local and do not coordinate multiple API instances.

This decision supersedes the default and silent-device-fallback aspects of
ADR-0008. The historical device-selection decision remains retained for its
model/backend compatibility rules.

## Consequences

Local model memory and concurrency are predictable, and a streaming generation
holds its Ollama slot until the stream really finishes. Contention produces a
bounded wait or an explicit capacity failure instead of unbounded latency.
The conservative profile may reduce throughput on a large machine, so custom
limits and distributed model-serving pools remain measurement-driven future
work. A shared Ollama daemon's global model cache still requires daemon-level
operator settings; request-level `keep_alive` is the application guarantee.
