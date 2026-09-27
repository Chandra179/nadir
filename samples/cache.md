---
title: "Caching & Redis Internals"
description: "Understand how Redis executes commands, how it is deployed and used, and how clustering, cache patterns, locks, and Lua fit together."
seoTitle: "Redis Internals, Cluster, Caching Patterns, and Lua"
seoDescription: "A practical guide to Redis command execution, deployment, data operations, Cluster routing, cache patterns, atomic operations, locks, and Lua scripts."
answerSummary: "Redis processes commands on a main execution thread, stores data in memory, and can scale across nodes with Redis Cluster. Use native commands for single-step updates, Lua for short multi-command operations on one shard, and locks only when work must remain coordinated outside Redis."
aliases: [redis, memcached, lua-scripting, distributed-atomicity, cache-stampede]
tags: [system-design, system-design/caching, redis]
created: 2026-06-13
---

# Redis: Internals, Deployment, Operations, and Patterns

Redis is a separate in-memory data store that applications access through a client library. Understanding how it executes commands helps explain where to run it, how to use it safely, and when to add clustering or server-side scripts.

## How Redis executes commands

Redis keeps its active data in memory. Its command execution is mostly single-threaded: the server handles one command at a time on its main execution thread. Network I/O and background work can use other threads, depending on the Redis version and configuration.

~~~text
Application clients
   │ commands and replies over TCP
   ▼
Redis process
   ├─ network I/O ─────────────── optional I/O threads
   ├─ command execution ───────── main thread
   │                                  │
   │                                  ▼
   │                            data structures in RAM
   └─ persistence work ───────── background work and disk
~~~

Sequential command execution means one command can finish without another client's command changing the same Redis data halfway through it. It also avoids the need for locks around each command's in-memory changes. The tradeoff is that a slow command or long-running script delays other commands on that node.

Redis can also save data using snapshots or an append-only file (AOF). These persistence options are separate from serving commands, and the durability you get depends on the configuration.

## Where Redis runs

An application normally uses a Redis client library to send commands to a Redis server. Running Redis separately also adds a network hop and another service that can fail. Applications need connection timeouts, retry limits, and a plan for what to do when Redis is unavailable.

| Deployment | Why use it | Main tradeoff |
|---|---|---|
| Separate process on the app's machine | Simple for development or a small deployment; traffic can use the local host | Competes for machine memory and CPU, and shares its failure boundary |
| Container | Packages Redis configuration and runtime separately from the app | Needs deliberate memory limits, storage, networking, and restart behavior |
| Managed cloud service | Provider may handle provisioning, monitoring, backups, and failover options | Adds provider cost and network dependency; available guarantees vary by service and plan |
| Dedicated VM or physical host | Gives direct control over resources and configuration | Your team operates upgrades, monitoring, backups, and recovery |

Even when Redis and the app run on one machine or in one container host, they remain separate processes communicating over a socket. A separate Redis service is useful when app instances need shared state or when Redis needs independent memory, scaling, and operations.

## Reading and changing data

Redis stores keys with values. A string key can be read, created, replaced, given an expiration, or deleted:

~~~text
SET user:42:name "Ayu"
GET user:42:name

SET cache:product:123 "..." EX 300
SET signup:token:abc "used" NX
DEL user:42:name
TTL cache:product:123
~~~

For a cache, a common flow is cache-aside: read Redis first, load the source database on a miss, then store the result in Redis with a TTL. On a source-data update, the application commonly invalidates the corresponding cache key so a later read refreshes it.

## Scaling from one node to Redis Cluster

A single Redis node is simplest. When its memory or command capacity is not enough, Redis Cluster distributes keys among primary nodes. Cluster divides the key space into 16,384 hash slots. It calculates a key's slot as:

~~~text
slot = CRC16(key) mod 16384
~~~

Each primary owns a range of slots. A cluster-aware client calculates a key's slot and sends the command to its owner.

~~~text
Application client
  │ calculate slot for key
  ├── slot 0–5,000 ──────► Primary A
  ├── slot 5,001–10,500 ─► Primary B
  └── remaining slots ───► Primary C
                              │
                         assigned keys
~~~

Redis Cluster can also use replicas, which hold copies of a primary's data and can take over if that primary fails. Most multi-key commands, transactions, and scripts require their keys to be in the same slot. Hash tags make related keys share a slot by hashing only the text inside braces:

~~~text
driver:{123}:status
driver:{123}:match
~~~

Both keys above use `{123}` for slot calculation. See the official [Redis Cluster specification](https://redis.io/docs/latest/operate/oss_and_stack/reference/cluster-spec/) for routing and redirection details.

## Making operations safe

A single Redis command is processed without another command interleaving on that node. Commands such as `INCR` and conditional `SET ... NX` can handle common single-key updates directly.

The problem appears when an application performs a read, makes a decision, then sends a separate write. Another client can change the value between those requests:

~~~text
Client A                    Redis                    Client B
   │── GET status ───────────►│
   │◄─ AVAILABLE ─────────────│
                               │◄──────── GET status ──│
                               │───────── AVAILABLE ──►│
   │── SET BUSY ─────────────►│
                               │◄──────── SET BUSY ───│
~~~

Both clients may act on the same AVAILABLE state. The right fix depends on the scope of the work:

- **One command :** use a native Redis command such as `INCR` or `SET ... NX`.
- **Several Redis commands must act together:** use a Redis transaction or a short Lua script. A transaction runs its queued commands without another client's commands interleaving, but it does not roll back earlier commands if a later command fails.
- **One application process needs mutual exclusion:** a process mutex can coordinate its own threads, but does not coordinate other app processes.
- **Several app processes need to coordinate longer work:** a distributed lock can help, but it is a lease with failure and expiry cases, not a transaction across Redis, SQL, or an external API.

A Redis lock is a temporary marker that lets one worker claim a job. 
`SET lock:resource <token> NX PX <milliseconds>` creates the marker only if it does not exist (`NX`) and sets an expiry time (`PX`). Give each attempt a different token so Redis can tell which worker owns the marker.

When the worker finishes, it should delete the marker only if the token still matches. Otherwise, a worker whose lock has expired could accidentally delete a newer worker's lock. The lock can also expire while work is still running, allowing another worker to start the same job. For important changes, make the operation safe to repeat or enforce uniqueness in the database that stores the result. A lock alone cannot prevent duplicate work after it expires. The [Redis distributed-lock guide](https://redis.io/docs/latest/develop/clients/patterns/distributed-locks/) explains the safety assumptions and failure cases.

## Common cache patterns

### Warm the cache before traffic

Cache warmup loads frequently needed values before users request them, for example after a service starts or a cache is replaced. It can reduce misses during a traffic spike. Loading too much at once can overload the source database, so warm only useful data and control the rate.

### Handle a hot key

A hot key, sometimes called the celebrity problem, is requested far more often than other keys. One popular profile, post, or product can concentrate traffic on its key and the Redis node that owns it.

Possible responses include a short-lived in-process cache, replicating or splitting read-heavy data, and caching precomputed results. Each adds a tradeoff: local copies can be stale, and split keys need aggregation. Measure the hot key and its effect before adding complexity.

### Choose when writes update the cache

With a synchronous cache update, the application updates the database and cache as part of the request. Readers see the cache update sooner, but the two writes can disagree if one succeeds and the other fails. Redis and a separate database do not share an automatic transaction.

With an asynchronous update, the application queues work for a background worker to apply to the cache or source. This can shorten request handling, but readers may see old data until the worker catches up. The queue or event path must handle retries and duplicate messages, and the application must decide whether acknowledging the request before the update is durable is acceptable.

**Cache-aside** is another common choice: the application reads through Redis and fills the cache only after a miss. It avoids preloading the entire dataset, but requires a clear TTL and invalidation policy.

## When to use Lua

Lua lets an application send a short program to Redis. The script can read and update Redis data in one server-side execution. No other command runs between the script's Redis operations, so it can protect a check-and-update sequence without separate application round-trips.

Consider two riders trying to claim the same driver. A plain `GET` followed by `SET` can race. A Lua script can check the status and write the match in one execution:

~~~lua
-- KEYS[1]: driver:{123}:status
-- KEYS[2]: driver:{123}:match
-- ARGV[1]: rider ID

local status = redis.call("GET", KEYS[1])

if status ~= "AVAILABLE" then
    return 0
end

redis.call("SET", KEYS[1], "MATCHING")
redis.call("SET", KEYS[2], ARGV[1])
return 1
~~~

The shared `{123}` hash tag keeps both keys in one Redis Cluster slot. The first script claims the driver; a later script sees that the status is no longer AVAILABLE. This protects the Redis update, not a later action in a separate database or service.

| Approach         | Best fit                                                                | Main cost or limit                                                                                       |
| ---------------- | ----------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- |
| Native command   | One atomic update, such as incrementing a counter                       | Cannot express a multi-step condition by itself                                                          |
| Lua script       | Several short Redis reads and writes that must not interleave           | Blocks command execution on that node while it runs; keep it short and use keys from one slot in Cluster |
| Distributed lock | Coordinating a longer workflow across app processes or external systems | More network steps and lease handling; does not make external writes atomic                              |

Use the simplest option that protects the state transition. A script is not inherently faster for every operation: it can save round-trips when it replaces several client-server exchanges, but script execution also uses the Redis command thread. 

Redis scripts provide atomic execution, not general rollback or a transaction with another service. For Redis 7 and later, Redis Functions are also available for server-side logic. See the official [Lua scripting documentation](https://redis.io/docs/latest/develop/programmability/eval-intro/) and [Redis Functions overview](https://redis.io/docs/latest/develop/programmability/functions-intro/).
