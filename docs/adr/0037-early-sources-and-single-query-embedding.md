# 0037 — Return sources before the model starts; embed each question once

- **Status:** Accepted
- **Date:** 2026-10-08
- **Deciders:** Chandra

## Context

The October 6 Locust runs (two documents, one user, 1–8 samples per workload)
were too small to establish capacity but showed where time went:

- `POST /api/v1/turns` with `generate: true` took about 5.8 s while retrieval
  took about 0.13 s. `startGeneration` dialed Ollama synchronously, and Ollama
  sends response headers with the first token, so the sources the user could
  already read waited for model load and prompt evaluation.
- A retrieval-only turn took about 200 ms although its retrieval operation took
  about 91 ms. Creating a chat session embedded the session title, and
  persisting a turn embedded the question again. No history code path searches
  by vector.
- A cache hit (280 ms) was slower than the miss that seeded it (190 ms). A miss
  embedded the same question in the cache lookup, in the search, and in the
  detached cache write.
- The cache required `len(results) >= top_k`, but the per-file cap
  (`max_chunks_per_file: 3`) means a one-document corpus can never return five
  results, so it could never hit. The cap also ran after the store ranked
  candidates with no backfill, so a question answered by one long document
  returned fewer than `top_k` chunks.

## Decision

1. **The answer model is dialed after the POST returns.** `startGeneration`
   registers the turn's event stream and starts the supervisor, which calls
   `Generate`. The POST returns the retrieved sources, citations, `turn_id` and
   `stream_url` immediately. **Contract note:** a failure to start generation
   (Ollama unreachable, non-200) used to appear as `generate_error` in the POST
   response with no stream. It is now a `generror` SSE event and the persisted
   turn's `generate_error`. Failures known before the dial still use the POST
   fields: prompt-budget overflow, broker capacity, and a conversation that
   changed during startup. The dashboard, evaluator and Locust clients already
   handled `generror`. A cancel or shutdown during the dial ends the turn like
   any cancelled stream (done event, no provider error). No heartbeat is sent
   while the model loads; SSE response headers follow the first event, and the
   generator's 120 s request timeout still bounds the wait.
2. **One embedding per question.** Retrieval embeds the question's fragments in
   one batch and passes the first vector (the whole question) to the cache
   lookup, the search and the cache write. The cache no longer calls a model.
   Its key text is now the first fragment (trimmed, trailing `.?;` removed)
   rather than the raw question, and the policy version moves to
   `retrieval-v3`.
3. **Cache entries record the request size.** `requested_top_k` is stored with
   the entry. An entry answers a request when it asked for at least `top_k`
   results, so a complete but short result set hits. Records without the field
   read as 0 and vouch only for the results they hold.
4. **History points carry a constant placeholder vector** instead of an
   embedding. The collection schema (dense, cosine, same size) is unchanged, so
   there is no migration; older points keep their embeddings, which nothing
   reads.
5. **The per-file cap backfills.** Each store leg is asked for three times the
   needed candidates before the cap and the final truncation. The reranker's
   candidate count is unchanged. The default cap stays 3; changing it needs the
   measurement in TODO.
6. **Ollama's own timings are recorded** as bounded operations
   (`ollama.generate.load|prefill|decode`, `ollama.embed.load|total`), and
   Locust reports include the per-operation difference between the before and
   after `/debug/metrics` snapshots as `summary.server_operations`.
7. **The evaluator flags a judge that is the answer model** in
   `provenance.judge.is_generator_model` and on stderr. It does not fail the run.

## Consequences

- Sources render about 0.2 s after submit instead of after the first token;
  time to first token is unchanged and is still what `TTFT` measures. The
  `chat_stream` operation now includes model startup; use
  `ollama.generate.*` for the breakdown.
- Existing semantic-cache entries become misses (the cache is already cold
  after every restart because its version includes a per-process epoch).
- Retrieval results can now include chunks from lower ranks where the cap used
  to shorten the list, so retrieval metrics should be re-measured; this change
  can only add candidates and was not measured here.
- The expected latency effects (retrieval-only turn near 110 ms, cache hit
  faster than miss, POST near 0.2 s) are predictions from the October 6
  metrics, to be confirmed by repeating the Locust workloads.
