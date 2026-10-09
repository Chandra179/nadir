# Scaling

What limits Nadir today, and what to change at each step. Numbers come from
the [evaluation log](evaluation-log.md) and the October 8 benchmark. Anything
not measured is marked **not measured**.

## Measured limits

Test machine: 12 CPU threads, 15 GB RAM, one 6 GB laptop GPU. Corpus: 13
documents. Load: 1, 2 and 4 simultaneous users.

| Area | Result | Limit |
|---|---|---|
| Search | 0.10 to 0.13 s, scales linearly to 4 users | Not a bottleneck |
| Chat | 8 to 11 s; 4× users gave 2.75× throughput | One language model on one GPU |
| Answer grading | A small local grader mis-scored correct answers | GPU memory |
| Larger corpus, more than 4 users, cold start | **Not measured** | Unknown |

## What more hardware does and does not fix

- More or faster GPUs raise **throughput** (more users at once). They only
  shorten a **single** answer if each GPU is faster than the current one.
- A bigger model improves answers and gives a more trustworthy grader. It also
  slows each answer unless the GPU is larger too.
- Quality ideas such as a refusal rule based on search scores (see the
  sufficient-context paper) or a reranker do not need new hardware. The current
  quality gaps are measurement and design, not resources.
- Methods that add model calls per question (corrective RAG, self-checking)
  add latency on any hardware.

## Steps, in order

| Step | Change | Do it when | Unblocks |
|---|---|---|---|
| 1 | Same machine: shorter prompt, keep the model loaded, try the reranker, add a refusal rule | Now | Latency and trust, no new hardware |
| 2 | Larger GPU or model | Answer quality or grading is the limit | Better answers, reliable grader |
| 3 | Several model servers behind a queue | Chat queueing at your real user count | Chat throughput |
| 4 | Qdrant sharding and replicas | Corpus or search rate outgrows one node. Rerun the golden set at 500 to 1,000 documents first | Corpus size |
| 5 | Several API replicas | Step 3 is not enough | Needs the change below |

## Before running more than one API process

Chat state lives in each process: the event log per turn (`broker`), the
connected subscribers, and the edit/delete version tokens (`historyMutations`)
in `internal/core/conversation/chat`. With two replicas, a browser that
reconnects, cancels or replays a turn can reach the wrong one. Either route
requests by turn id, or move the event log and version tokens to a shared
store. Sessions, history and the semantic cache are already in Qdrant, so they
are shared. See ADR [0006](adr/0006-chat-streams-over-domain-owned-event-log.md)
and [0017](adr/0017-chat-history-mutation-ownership.md).

## Before scaling

Run the golden set and the benchmark on a larger corpus, and on real questions.
Scaling a pipeline whose answer quality is not yet measured hides problems
instead of fixing them.
