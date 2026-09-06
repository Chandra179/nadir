# 0010 — The offline eval harness is removed from the repo (supersedes 0005)

- **Status:** Accepted — supersedes [0005](0005-fresh-control-evalbench-ab.md)
- **Date:** 2026-09-05
- **Deciders:** Chandra, ZCode session

## Context

ADR 0005 specified how retrieval A/Bs should run (`cmd/evalbench` + the
`internal/eval` package, fresh control per session, reports under
`results/`, canned cases in `config/golden/`). In practice the harness was a
standalone CLI: nothing in the running server imported it, the stored reports
were one-off artifacts of a single tuning session, and the golden files were
never consumed. It was a second entrypoint and config surface to maintain for
experiments run a handful of times.

## Decision

Remove `cmd/evalbench`, `internal/eval`, and `config/golden/` from the repo,
and scrub references from docs. The `results/` reports were deleted with an
add-all commit but remain restorable from git history
(`git checkout <sha>~1 -- results/`).

The fresh-control principle in 0005 is sound; it simply has no machinery
implementing it anymore. If in-repo evaluation returns, that ADR (restored
from history) is the design to rebuild against.

## Consequences

- Quality decisions lean on sidecar-side reranker benchmarks (ADR 0003
  evidence) and live manual checks.
- `config/golden/` no longer pretends to be a config concern.
- One binary, one entrypoint: `cmd/server`.
