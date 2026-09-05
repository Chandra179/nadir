# 0002 — Conversational query rewriting before retrieval and generation

- **Status:** Accepted
- **Date:** 2026-09-05
- **Deciders:** Chandra, ZCode session

## Context

`chat.Ask` fed the raw follow-up text straight into retrieval, so a second
turn like "what about the second one?" searched garbage — the embedder and
BM25 leg never see the antecedent. The known fix is Rewrite-Retrieve-Read
(arXiv 2305.14283; LangChain's condense-question pattern): rewrite the
follow-up into a standalone query against recent conversation turns, then
retrieve with it.

Constraints and tradeoffs from the research pass (TODO.md Phase 3):

- The rewrite costs +1 LLM call per follow-up turn (measured ≈0.6–0.9s warm
  on gemma3:1b).
- Rewrites can drift (model rewrites a perfectly good query into something
  else); a 1B model also sometimes decorates output with labels/quotes.
- Rewriting is meaningless on a session's first turn — there are no prior
  turns to resolve references against.
- Any failure (history unreadable, model down) must not break the turn.

## Decision

- New domain package `internal/rewriter`: `Rewriter` interface
  (`Rewrite(ctx, turns []Turn, query string) (string, error)`) in
  `interface.go`, Ollama chat client in `service.go` — same layout as
  `internal/enrichment`. It defines its own `Turn{Query, Answer}` type;
  `chat` maps `history.Turn` onto it, so the rewriter does not import the
  history package.
- `chat.Ask` rewrites **only when** `req.SessionID` is set, history is
  available, and the session has prior turns (last `rewriter.turns`, default
  4). The rewritten query drives **both retrieval and generation** (the
  generator otherwise answers an unresolved pronoun against the retrieved
  context); the **raw query is what gets persisted and displayed**.
- Best-effort by construction: history read failure, empty turn list,
  rewrite error, or the 8s client timeout all fall back to the raw query.
  Temperature 0 plus an explicit "return it unchanged if already standalone"
  instruction keep pass-through queries intact; output cleaning strips
  fences, quotes, and echoed labels.
- Feature flag `rewriter.enabled` (default **on** — it fixes a correctness
  bug on the query path and degrades gracefully; index-time LLM features
  stay default-off because they cost reindexes). Env overrides:
  `REWRITE_ENABLED`, `REWRITE_ADDR`, `REWRITE_MODEL`, `REWRITE_TURNS`.
  Addr/model fall back generator → embedder, mirroring enrichment.
- `chat.History` gained `ListTurns` (consumer-side interface extension);
  `history.Dependencies` already implemented it.

## Consequences

- Follow-up turns retrieve with a self-contained query; multi-turn chat
  stops breaking retrieval.
- Every follow-up pays ~0.6–0.9s extra on gemma3:1b (first turn of a session
  pays nothing). Disable with `REWRITE_ENABLED=false` if that budget matters
  more than correctness.
- Rewrite quality is bounded by the configured model; the fallback keeps a
  bad rewrite from doing worse than the status quo ante.
- Live verification (gemma3:1b): "what about the second one?" → "limits at
  infinity"; "how is it related to the derivative?" → "secant formula
  derivative"; standalone questions pass through unchanged.
