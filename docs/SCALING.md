---
title: "Nadir Scaling and Concurrency"
description: "Current concurrency guarantees, scale-out limits, bottlenecks, and the path to distributed operation"
tags: [architecture, scaling, concurrency, operations]
---

# Nadir Scaling and Concurrency

## Executive summary

Nadir is currently a single-node modular monolith. One Go process owns the
HTTP transport, Chat turn lifecycle, in-process event log, and process-local
mutation coordination. Qdrant stores the Document corpus, semantic cache,
and persisted Chat history; Ollama and the optional reranker and Docling
processes provide external computation.

The safest part to scale first is read-heavy Retrieval: multiple application
instances can share Qdrant and the model backends when Chat streaming,
Indexing, and destructive operations are kept on one writer or given explicit
coordination. Running multiple unmodified Nadir instances as active-active
writers is not safe.

Horizontal scale becomes a design change when any of these are required:

- a Chat stream must survive routing to a different application instance;
- two instances may append, edit, or delete the same Session;
- more than one instance may run an Indexing pass against the same source
  identity;
- a reset must be issued safely from any instance; or
- one application instance is no longer enough for the measured Retrieval,
  generation, or Indexing workload.

The current recommendation is to finish single-node correctness and operating
evidence first. Add distributed infrastructure only when measurements or
availability requirements justify its operational cost.

## Current topology

```text
Browser
  |
  v
React dashboard (Vite locally or an external static host)
  |
  v
one Nadir API process
   ├── Document intake + Indexing pass
   ├── Retrieval: embed → dense + BM25 → RRF → optional rerank → cache
   ├── Chat turn lifecycle + generation supervisor
   ├── in-process bounded Chat event log
   └── process-local Session mutation revisions
          |
          ├── Qdrant
          │   ├── Document corpus
          │   ├── semantic cache
          │   └── Chat history
          ├── Ollama: embeddings, rewriting, enrichment, generation
          ├── reranker sidecar
          └── optional Docling sidecar for PDF Document intake
```

The Compose deployment supplies a portable CPU topology. A GPU override is
available for the reranker on Linux or Windows WSL2 with NVIDIA support.
Apple Silicon uses the CPU reranker while Ollama may use host Metal
acceleration. These deployment choices affect latency and capacity, but do not
change the consistency model.

## Domain invariants

These are the properties that must remain true as implementations change.

- A Document is identified by its source identity and content hash.
- An Indexing pass should publish one complete active version of a Document;
  a failed replacement must not remove the previous active version.
- Retrieval must never use inactive Document versions.
- A Session is an ordered sequence of Chat turns.
- Editing a Chat turn removes that turn and its later tail before the
  replacement is appended.
- Deleting a Session or all Sessions must not be undone by older asynchronous
  generation work.
- A cache result must not outlive the Document corpus version it describes.
- A Chat subscriber may reconnect and replay events in order, or receive an
  explicit resynchronization signal when the retained window is too old.

The current implementation satisfies several of these invariants within one
process. It does not yet provide the fencing, shared ordering, or durable
event ownership needed to satisfy all of them across multiple processes.

## Concurrency model today

| Area | Current guarantee | Limitation |
|---|---|---|
| HTTP requests | Handled concurrently by the Go HTTP server | No global admission control or rate limit |
| Retrieval fragments | Up to the configured fragment limit per request, with bounded parallel fragment searches | The limit is per process and per request; many clients multiply the load |
| Embeddings | Ingest batches requests and uses bounded file workers | Search and ingest share the external embedder without a global budget |
| Reranking | A per-process semaphore bounds concurrent sidecar calls | Multiple application instances multiply sidecar pressure |
| Document replacement | New versions are staged inactive, then activated and old versions cleaned | Concurrent replacements for one source identity are not ordered or fenced |
| Indexing pass | One process serializes complete passes and coordinates them with reset | The gate is process-local; distributed workers still need leases/fencing |
| Chat event log | Bounded, insertion-ordered, replayable in-process streams | Turn IDs and event cursors exist only on the owning process |
| Session mutations | One process-local revision registry serializes destructive Chat mutations | Another process has a different revision registry and can append stale data |
| History writes | One process-local mutex protects sequence allocation | The mutex does not protect writes made by another process and serializes all sessions together |
| Cache writes | Cache entries carry a process-local invalidation generation and are checked at read time | A shared generation is still needed across application instances |
| Shutdown | HTTP connections, active generations, and detached history writes drain within a bounded shutdown budget | A timeout can still leave an external history write unfinished; the lifecycle logs this and durable retry state is still open |
| Full reset | Builds a new collection generation and switches a stable alias before cleanup | Cleanup can leave temporary retired collections; alias/lifecycle state is still single-node |

The Interface at each of these seams is useful only when its ordering and
failure guarantees are explicit. A second Adapter is not enough by itself:
distributed correctness also needs shared state, fencing, and recovery rules.

## What is safe to scale now

### Retrieval readers

Read-heavy Retrieval is the closest part to being horizontally scalable. Each
request carries its query and filters, and the durable Document corpus and
semantic cache are externalized to Qdrant. Multiple instances can therefore
serve independent Retrieval requests if they use the same:

- embedding model, dimensions, and query/document prefixes;
- collection and schema version;
- reranker model and backend behavior; and
- configuration limits and prompt policy.

This is not a complete active-active deployment. The following must remain
single-writer or coordinated:

- `/api/v1/documents` for a shared source identity;
- full Document reset;
- Chat generation start and SSE subscription routing;
- Session edit, append, and delete operations; and
- any cache invalidation protocol that assumes no in-flight stale write.

### External computation

Ollama, reranking, and Docling are already Adapters behind HTTP seams, so they
can eventually be moved to separate hosts or pools. The tradeoff is that the
application must then add connection pooling, per-backend concurrency limits,
health-aware routing, model-version verification, and capacity monitoring.

## What is not distributed-safe

### Chat streaming

The event broker retains streams in process memory. A POST that starts a
generation on instance A followed by an SSE request routed to instance B will
normally produce an unknown-turn response on B. Sticky sessions can hide this
problem, but they do not provide failover: if A stops, B cannot replay A's
events.

The broker is intentionally bounded. Its current defaults retain at most 64
turn streams, up to 4096 events and 1 MiB of replay text per turn, with a
10-minute finished-turn retention window. Active streams consume retention
slots; if all slots are active, new generations are rejected. This is a good
single-node safety limit, not a distributed event system.

### Session ordering and destructive mutations

History sequence allocation reads a Session turn count and then writes a turn
and updates the counter. The process-local history mutex makes this coherent
inside one process, but it is not a compare-and-swap operation. Two instances
can read the same turn count and create turns with duplicate sequence values.

The Chat mutation registry has the same scope. Session and global revisions,
active generation cancellation, and stale append rejection are process-local.
An edit or delete on instance A cannot invalidate an already-running append on
instance B.

For distributed operation, the authoritative Session mutation must use a
shared conditional write or fencing token. Sequence assignment, edit epoch,
delete epoch, and append acceptance must be decided by one shared authority.

### Indexing ownership

The Indexing pass is bounded within one invocation, but there is no process-wide
or distributed lease for a source identity. Two passes can both observe an
old SHA, embed the same source, and interleave activation and cleanup. Because
the cleanup filter is based on source identity and hash rather than a
monotonic ingest generation, completion order can determine which version
remains active instead of source freshness.

Deterministic point IDs make retries less likely to create duplicates, but
idempotence is not ordering. Distributed Indexing needs a shared source
manifest, a per-source lease or fencing token, and a commit rule that rejects
stale plans.

The current source model also reads local paths. Multiple instances need the
same source bytes and stable source identities; host-specific paths or
different bind mounts can otherwise create duplicate Documents or inconsistent
SHA observations. Removed source files are not reconciled by a normal ingest
sweep today, so a shared manifest must define deletion semantics as well.

## Important single-node consistency gaps

These issues should be addressed before claiming production-grade scale-out.

### Overlapping Indexing passes

Concurrent `/api/v1/documents` requests are serialized within one process, and reset is
coordinated with the complete Indexing pass. Distributed workers can still
race because the gate is not shared. Add per-source leases and a monotonic
fencing generation before allowing distributed Indexing.

### Semantic-cache invalidation race

Retrieval writes cache entries asynchronously with a detached context. The
cache now attaches a process-local invalidation generation to each entry and
validates it at read time, so a write from before an Indexing pass or reset
cannot become valid again within that process. A cache clear remains useful
for space reclamation; the generation must be shared by all application
instances later.

### Recoverability of full reset

Reset now stages a replacement collection, validates the full dense and BM25
schema, atomically switches the active alias, and cleans up the old collection
only after successful publication. Failed cleanup is retryable, and the
process-local lifecycle gate defines behavior when reset and Indexing overlap.

### Detached Chat persistence

Normal turn persistence and generation supervision are intentionally detached
from the HTTP request. The process now stops new Chat admission on shutdown,
cancels active generations, waits for generation supervisors, and drains
detached history writes within the configured shutdown budget. If the external
history store remains unavailable past that budget, the lifecycle logs the
failure; durable retry state is still required before claiming recoverability
across process loss.

### Liveness and readiness

`/api/v1/health` is a cheap liveness endpoint and does not call external
services. `/api/v1/ready` is the dependency gate: it checks Qdrant, performs a
real embedding request and dimension check against the configured Ollama model,
and checks the enabled reranker sidecar's loaded model and runtime. It returns
HTTP 503 with per-dependency diagnostics until all required checks pass. A
deployment should route traffic only to ready instances and use liveness for
restart decisions.

### Global resource admission

Most limits are local to one request or one process. Without a global budget,
replicas or a burst of clients can overload Qdrant, Ollama, or the reranker.
Production operation needs explicit policies for request rate, concurrent
Retrieval, generation slots, embedding work, Indexing jobs, and per-backend
timeouts. Rejection or queueing is preferable to unbounded latency growth.

### Destructive-operation security

The current HTTP surface includes Document reset, Document intake, and delete
all Chat operations. A deployment exposed beyond a trusted local network needs
authentication, authorization, CSRF protection where browser cookies are used,
audit logging, and rate limits before those routes are reachable by untrusted
clients. Multi-user isolation and tenant-specific source identities are not
implemented by the current domain model.

## Main bottlenecks

The current limits prevent individual requests from being unbounded, but some
limits multiply across stages:

| Workload | Current bound or behavior | Likely bottleneck |
|---|---|---|
| Long Retrieval query | Up to 16 fragments, up to 8 fragment searches in parallel | Qdrant calls and embedder capacity |
| Maximum Retrieval | `top_k` capped at 50 | Candidate fan-out and response memory |
| Reranked Retrieval | Candidate multiplier fetches more than the requested results | CPU reranker latency; current measured p50 is about 3.2 seconds on the toy benchmark |
| Hybrid Qdrant leg | Each fragment fetches dense and sparse candidates before RRF | Qdrant CPU, RAM, and segment/index pressure |
| Indexing pass | 8 file workers, embedding batches of 64 | Ollama throughput and host memory |
| Large Document | Up to 10,000 chunks per file | Planning memory, embedding cost, and replacement payload size |
| HyPE enrichment | Multiple LLM questions per chunk when enabled | LLM calls and temporary point count |
| Chat generation | Up to 64 retained streams per process | Ollama throughput, per-turn memory, and SSE connections |
| History append | All writes serialized by one process-local mutex | Slow Qdrant or embedding calls block unrelated Sessions |
| PDF intake | Conversion is synchronous in an Indexing worker | Docling CPU/memory and timeout behavior |

The effective load of one hostile or simply large query is greater than its
HTTP request count suggests. Candidate multiplication, fragment parallelism,
reranking, and concurrent clients should be measured together. Raising one
limit without measuring downstream saturation usually increases tail latency
instead of throughput.

## Target distributed architecture

```text
Clients
   |
Load balancer / API gateway
   |
Nadir application replicas
   ├── stateless Retrieval readers
   ├── Chat command and SSE adapters
   ├── controlled Indexing workers
   └── readiness, metrics, traces, structured logs
        |
        ├── shared Chat event backend
        ├── shared Session authority with conditional mutation/fencing
        ├── shared Qdrant cluster for Document Retrieval and cache
        ├── shared source manifest/object storage + Indexing queue
        └── model-serving pools: embedder, generator, reranker, Docling
```

The target is not “put every current process behind a load balancer.” Each
stateful concern needs an owner and a recovery protocol:

1. The API replicas accept requests and can serve independent Retrieval.
2. A Chat command creates a globally addressable turn and publishes ordered
   events to a shared event log.
3. Any SSE adapter can subscribe by turn ID and replay from a global cursor.
4. Session mutations use a shared epoch or fencing token; stale generation
   work is rejected at persistence time.
5. An Indexing coordinator leases source identities and commits only the newest
   fenced plan.
6. Qdrant provides durable vector storage, while backups and cluster health
   are managed as an explicit operational concern.

## Shared Chat event backend requirements

The current ADR leaves the implementation open and names Redis Streams as a
pragmatic candidate. That choice should not be made until scale-out begins.
Whatever Adapter is selected must provide:

- a globally unique turn ID;
- ordered events within one turn;
- replay from a client cursor;
- bounded retention and explicit expiration;
- fan-out to multiple SSE subscribers rather than competing consumption;
- a terminal event that remains replayable long enough for reconnects;
- behavior for a node failure during generation;
- at-least-once delivery with client/server deduplication by event ID; and
- observability for lag, dropped subscribers, retention gaps, and active turns.

Sticky sessions are an acceptable transitional deployment for read scale, but
they should be documented as an availability compromise. A shared event log
provides failover and routing freedom at the cost of another highly available
dependency, event retention management, and more complex ownership semantics.

Qdrant is not the preferred event log. Its vector storage and scroll/query
operations do not provide the low-latency ordered fan-out semantics required
for token streaming.

## Shared Session authority

Qdrant can continue to store vectorized Chat history for retrieval or browsing,
but distributed ordering should not depend on a read-then-write turn counter
protected only by a Go mutex. A future Session authority should atomically
decide:

- the next sequence number;
- the current Session mutation epoch;
- whether an append belongs to the current epoch;
- whether an edit may prune a given tail; and
- whether a delete invalidates all older work.

A relational store is often a better authority for these transactional
metadata operations, while Qdrant remains useful for vector search over
history. Keeping these responsibilities separate improves Module depth and
locality, but adds an infrastructure dependency and a migration path.

## Distributed Indexing

Before adding Indexing workers, define a shared source model:

- source bytes live in a common readable location or object store;
- a manifest records source identity, content hash, ingest generation, and
  deletion state;
- a queue schedules changed sources;
- a lease or fencing token grants one worker ownership of a source;
- plans are idempotent and commits reject stale generations;
- replacement publication is atomic from Retrieval's perspective; and
- failed cleanup and interrupted jobs are visible and retryable.

For a large corpus, a full-corpus rebuild should use a separate collection
generation and an atomic alias switch rather than deleting the live collection
first. That protocol also makes schema or embedding migrations safer. It
requires temporary storage for two generations and an explicit policy for
cache invalidation and in-flight Retrieval.

## Operations and observability needed for scale

Before operating multiple instances, retain the existing `/api/v1/health`
liveness and `/api/v1/ready` dependency-readiness checks in the deployment
contract, and add:

- protected or disabled profiling endpoints;
- request, Session, Chat turn, Indexing job, and reset operation IDs in logs;
- metrics for Retrieval latency, fragment fan-out, cache hit/stale rates,
  reranker fallback, generation slots, SSE lag, replay gaps, Indexing queue
  depth, source failures, and reset phase outcomes;
- traces across HTTP, Qdrant, Ollama, reranker, Docling, and shared backends;
- model, embedding, schema, and corpus-generation labels for result analysis;
- Qdrant backups, restore drills, disk/segment monitoring, and capacity
  alarms; and
- runbooks for dependency outage, stale cleanup, failed reset, stuck leases,
  event-log retention, and model mismatch.

Logs alone are sufficient for local debugging but make distributed diagnosis
slow and speculative. Observability should expose the state transitions that
matter to the domain, not only generic HTTP request counts.

## Tradeoffs

| Choice | Benefit | Cost or risk |
|---|---|---|
| Keep one process | Lowest operational complexity and strong locality for Chat mutations | One-node failure, limited concurrency, no active-active Chat |
| Sticky sessions | Smallest change for initial read scale | No failover for in-memory streams; routing assumptions leak into deployment |
| Shared event backend | Any replica can serve SSE and replay after routing changes | New durable dependency, retention policy, lag handling, and failure modes |
| Qdrant for all persistence | Few dependencies and existing vector retrieval | Weak fit for transactional Session ordering and conditional mutations |
| Relational Session authority plus Qdrant vectors | Strong ordering and simple conditional writes | More infrastructure and two data lifecycles to reconcile |
| Synchronous Indexing endpoint | Simple user experience and immediate result | Large Documents occupy HTTP/workers and are hard to retry or distribute |
| Queue-based Indexing | Backpressure, retries, leases, and horizontal workers | Eventual consistency, job visibility, and queue operations |
| Best-effort semantic cache | Lower latency and resilience when cache is unavailable | Requires a shared corpus generation to avoid stale data across replicas |
| CPU reranker | Portable Linux, Windows, and macOS deployment | High p50 latency and lower concurrency |
| GPU reranker pool | Lower latency at sufficient batch/concurrency levels | Hardware scheduling, vendor coupling, and higher deployment cost |

## Recommended roadmap

### Before any horizontal deployment

1. Add drainable Chat persistence and generation shutdown.
2. Expand HTTP smoke tests with dependency-backed failure cases, readiness
   checks, protected profiling, and basic
   admission/rate limits for expensive and destructive operations.
3. Expand Retrieval and generation evaluation on real Documents before tuning
   capacity or model defaults.

### First scale step: read-heavy Retrieval

Run multiple API replicas only after they share identical configuration and
model/schema versions. Route Retrieval reads through a load balancer, keep
Indexing and reset single-writer, and use sticky Chat routing if Chat is
required during this transitional phase. Measure p50/p95/p99 latency,
Qdrant saturation, embedder throughput, reranker queueing, and cache behavior.

### Second scale step: distributed Chat

Introduce the shared event-log Adapter, global turn routing, replay/fan-out
tests, and a failure policy for a generation owner disappearing. Then replace
process-local Session revisions with shared epochs or fencing tokens.

### Third scale step: distributed Indexing

Introduce shared source storage and manifest, job leases, generation-aware
Document commits, deletion reconciliation, and collection-generation rebuilds.
Only then allow multiple Indexing workers or active-active reset commands.

### Final scale step: high availability

Add Qdrant replication and restore drills, model-serving pools, dependency-aware
readiness, autoscaling based on measured saturation, centralized observability,
and authenticated multi-user isolation if the deployment is no longer a
trusted local application.

## Decision triggers

Scale-out work is justified by evidence such as:

- sustained p95 Retrieval or generation latency above the product budget;
- concurrent Chat streams regularly exhausting the bounded broker;
- Indexing passes exceeding the acceptable maintenance window;
- model-serving CPU/GPU saturation with spare application capacity;
- a required availability target higher than one application/Qdrant node; or
- multiple independent users or teams needing shared access and isolation.

Do not add a shared event backend, queue, or database only because the code
could theoretically use one. Each new Adapter reduces some local bottleneck
while adding failure modes, operational ownership, and cross-system recovery
work. The smallest design that satisfies a measured requirement is preferred.

## Current architectural decision

Nadir remains intentionally single-node for Chat event retention, Session
mutation coordination, full reset, and source Indexing ownership. The bounded
in-process broker and process-local locks are valid for that deployment model.
When a distributed requirement is accepted, record the chosen event backend,
Session authority, Indexing ownership, and failure semantics in separate ADRs
before changing the corresponding Seams.
