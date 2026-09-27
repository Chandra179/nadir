---
title: "System Design Core"
description: "A layered system design checklist for defining requirements, choosing basic building blocks, protecting correctness, coordinating work, scaling traffic, and handling failures."
tags: [system-design]
created: 2026-09-05
---

# System Design Core

- Check if the operation is CPU/Processor or GPU (matrix, neural network, etc..) heavy
- Choose strategy for in-memory (read, write, eviciton, invalidation, stale data)
- Analyze network latency inbound/outbound and bandwith
- When choosing a data structure or storage, check the internal architecture whether its suitable for our problems or not, i.e, for faster key value access data we can use (in-memory) redis store

---

- Clarrify the requirements and scope, also the effort it takes for development, whether its worth doing or no based on impact of the  output
- estimate average and peak requests per second, object size, storage growth, read and write volume, bandwidth, concurrent users, and retention time.
- identify which cost limit matters most: compute, memory, storage, bandwidth, operations, or third-party usage.

---

- strong, causal, or eventual consistency 
- use atomic operations, mutexes, database row locks, version checks, etc.. for critical operation
- CAP tradeoff
- Temporary state and recovery: decide what may be lost from memory and how a node, database, or cache outage rebuilds state or falls back to durable storage.
- Source of truth

---

- Push and pull event
- at-least-once vs exactly-once delivery
- Backpressure: limit queues and concurrent work when producers are faster
than consumers. Decide whether to delay, drop, or reject work.
- Reservation or hold with expiry: protect scarce inventory
- Transactional outbox
- Change Data Capture (CDC)

---

- Single-flight pattern
- Routing and load distribution for servers, databases, partitions, or regions
- Partitioning and sharding
- Celebrity problem
- Autoscaling
- Fail open or fail closed
- CDN