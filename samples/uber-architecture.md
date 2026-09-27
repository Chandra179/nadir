---
title: "Uber Architecture"
created: 2026-08-08
author: "Koala"
authorProfile: "/about/introduction#author-koala"
tags: [uber, architecture, backend, software-design]
description: "A conceptual ride-hailing architecture covering geospatial indexing, dispatch, storage, streaming workflows, and service boundaries."
seoTitle: "Ride-Matching System Design with Geospatial Search"
seoDescription: "A conceptual ride-hailing architecture covering geospatial indexing, low-latency dispatch, storage, streaming workflows, and service boundaries."
answerSummary: "This conceptual design shows how a ride-hailing platform can match supply to demand with geospatial indexing, low-latency dispatch, and separate storage paths."
---

# Ride-Matching System Design: Geospatial Search and Dispatch

This conceptual ride-hailing design matches driver supply with rider demand using geospatial indexing, low-latency dispatch, and storage paths separated by access pattern.

It uses Uber's publicly discussed terminology as a reference point, but the diagrams and capacity choices below are an engineering model rather than an official description of Uber's current implementation.

The sections below cover the main parts.

## Geospatial Indexing (Google S2)

Uber's location system partitions the Earth with Google's S2 geometry library.
This avoids calculating precise spherical distances for every user in real time.

**Cell mapping:** S2 projects the Earth onto a cube and divides it into
hierarchical cells with unique 64-bit IDs. Level 13 (~0.5 km²) or Level 14
(~0.1 km²) cells can support neighborhood dispatching.

**Searching:** The backend maps a rider's coordinate to an S2 Cell ID, then
queries that cell and its eight neighbors instead of the whole database.

## The Core Ride Loop Architecture

```text
Driver app -- location --> Supply service --\
                                             DISCO --> ETA / routing
Rider app  -- request  --> Demand service --/
```

**Step A: Supply Service (tracking drivers)**
Active driver apps send location pings about every 4 seconds over WebSockets or
HTTP. The pings go to Kafka and update an in-memory spatial index such as Redis.

**Step B: Demand Service (rider request)**
The Demand Service receives the rider's coordinates, destination, and vehicle
tier, such as UberX or XL.

**Step C: Matching Engine (DISCO)**
DISCO receives the request, queries nearby driver candidates, and sends them to
the ETA Engine. The ETA Engine uses road networks, traffic, and routing data to
estimate drive times. DISCO selects a driver and sends the offer to the phone.

## Data Architecture & Storage

Uber processes petabytes of data daily and uses different storage by access
pattern:

- **In-memory cache (Redis):** Current driver locations, sessions, and active ride status.
- **Transactional storage (Schemaless / MySQL):** Trip details, user profiles, and order records.
- **Real-time analytics (Apache Pinot and Flink):** Dynamic pricing, fraud detection, and driver incentives.
- **Data warehouse (Hadoop/HDFS and Parquet):** Historical trips for ML training, ETA prediction, and analytics.

## Microservice Organization: DOMA

DOMA organizes Uber's microservices into five layers:

1. **Edge Layer:** The API Gateways exposing public endpoints to mobile apps.
2. **Presentation Layer:** App-specific logic for iOS, Android, or Web interfaces.
3. **Product Layer:** Core business logic specific to a product (e.g., Rides, Eats, Freight).
4. **Business Layer:** Shared capabilities used across all products (e.g., Payments, Passports/Identity, Billing).
5. **Infrastructure Layer:** Low-level operations like database management, networking, and deployment frameworks.

## Core Design Drivers: Ratio, CQRS & CAP Trade-Offs

The `1:10` driver-to-rider ratio and the AP/CP split guide the database, write
protocol, and partition choices.

### How the `1:10` Driver-to-Rider Ratio Shapes the System

The ratio creates an asymmetric Read/Write profile:

$$\text{Writes} = 250,000 \text{ pings/sec (Drivers sending updates)}$$
$$\text{Reads} = 50,000\text{--}100,000 \text{ queries/sec (Riders opening maps, searching, polling)}$$

Drivers write far more often than riders query because they send updates every 4
seconds. This leads to three design choices:

**1. Ingestion protocol (gRPC over HTTP/2 vs. REST):** REST over HTTP/1.1
would create too many connection handshakes at 250,000 writes/sec. Long-lived
gRPC streams over HTTP/2 let up to 1,000,000 drivers keep persistent sockets
open and send small Protobuf payloads.

**2. CQRS:** Driver updates and rider searches use separate paths. Writes go
from drivers through Kafka to Redis primaries; reads use Redis replicas. Rider
searches do not block location writes.

### How the AP vs. CP Trade-Off Shapes the System

The platform uses different CAP trade-offs for different sub-domains:

| Engine | Requirement | Trade-off Choice | Storage Engine | Flow |
|--------|-------------|----------------|----------------|------|
| Location Tracking Engine | High Availability & Sub-second Latency | AP Eventual Consistency | Redis Spatial Cluster | → feeds into → |
| Matching & Trip State Engine | Zero Double-Bookings, Financial Integrity | CP Strong Consistency | Distributed RDBMS (CockroachDB / Postgres) | |

**AP engine (location streaming):** A stale driver location does not affect a
completed payment. Redis and Kafka favor speed over strict ACID guarantees. A
failed location ping is dropped; the next ping arrives about 4 seconds later.

**CP engine (matching and dispatch):** Double-booking a driver loses trust and
money, so consistency takes priority. Matching uses distributed locks, atomic
Lua scripts, and relational ACID transactions. During a network partition, the
request fails and the rider tries again instead of risking a double booking.

### Summary Matrix

| Metric / Constraint | Design Decision Driven By It |
|---------------------|------------------------------|
| `1:10` Asymmetric Scale | Separated Read/Write pipelines (CQRS) and used persistent gRPC streams instead of REST |
| AP (Location Tracking) | Redis in-memory storage, dropped-packet tolerance, 2-second eventual consistency |
| CP (Trip Matching) | Pessimistic/Optimistic distributed locking, transactional SQL state updates, hard consistency guarantees |

## Production System Design: Driver Tracking & Matching

### Requirements & Scale Expectations

**Functional:**
- Location tracking: Drivers send GPS updates every 4 seconds.
- Nearby driver lookup: Riders see available drivers on a map in real time.
- Ride request & matching: Select optimal driver based on ETA (not straight-line distance).
- Offer acceptance: Assigned driver has 15 seconds to accept or decline.

**Non-functional:**
- Low latency: Location ingestion < 50ms; matching decision < 1 second.
- High throughput: Handle 1,000,000+ active drivers sending pings continuously.
- Consistency: Assign each driver to at most one rider at a time.
- High availability: 99.99% uptime without a single point of failure.

### Pipeline Architecture

```text
Driver app -- WebSocket --> gateway --> Kafka --> tracking --> Redis
Rider app  -- HTTPS/gRPC -> gateway --> DISCO --> Redis (nearby drivers)
                                             \--> ETA engine
```

**Write path (driver ingestion):**
1. Driver app streams GPS pings every ~4 seconds over WebSocket.
2. The API Gateway terminates TLS and routes pings to Kafka.
3. Kafka buffers the stream and shields downstream services from spikes.
4. Location Tracking Service consumes pings, calculates the S2 Cell ID, and updates Redis (driver state + spatial index).

**Read path (rider match):**
1. Rider sends an HTTPS POST to `/v1/trips/request` via the API Gateway.
2. DISCO handles the request synchronously: it directly queries Redis for nearby driver candidates.
3. DISCO calls the ETA Engine via gRPC with candidate coordinates for real drive times.
4. DISCO selects the optimal driver, acquires a lock, and pushes a notification.

### Protocol Differences: Driver vs. Rider

| Aspect | Driver App | Rider App |
|--------|-----------|-----------|
| Protocol | WebSocket (long-lived, continuous) | HTTPS / gRPC (request-response) |
| Why | Streams location every 4s; needs persistent connection | Requesting a ride is a single action/command |
| Post-match | Stays on WebSocket for dispatch offers | Switches to WebSocket after match (to see driver moving) |

A rider's request does not go through Kafka. Kafka is an asynchronous event log,
not a query engine; DISCO queries Redis directly.

### ETA vs. Real-Time Analytics: Decoupled

Both are separate microservices with different roles:

**A. ETA Engine (synchronous, inline during match):**
- DISCO queries Redis for candidates (e.g., 10 available drivers in the S2 cell).
- DISCO calls the ETA Service via gRPC with those 10 coordinates + rider pickup.
- ETA returns drive times (Driver A: 3 min, Driver B: 5 min). DISCO picks the best match.

**B. Real-time analytics / surge pricing (asynchronous):**
- Analytics is outside the matching request-response loop.
- Flink and Pinot consume raw location and search pings directly from Kafka in the background.
- Stream 1: Driver location updates → calculate available supply per H3 cell.
- Stream 2: Rider app opens/searches → calculate demand per H3 cell.
- Flink computes the surge multiplier (e.g., 1.4x) and writes it to a cache. DISCO reads the cached rate without waiting for analytics.

| Action | Protocol / Tech | Sync or Async? |
|--------|----------------|----------------|
| Driver Ingestion | WebSocket → API Gateway → Kafka → Redis | Async (event-driven) |
| Rider Search / Match | HTTPS → DISCO → Direct Redis Query | Sync (sub-second RPC) |
| ETA Calculation | DISCO → ETA Engine (gRPC) | Sync (inline during match) |
| Surge / Analytics | Kafka Stream → Flink → Surge Cache | Async (out-of-band) |

### Dispatch Flow: Four-Phase Sequence

DISCO follows a multi-stage workflow:

**Phase 1: Pre-request and fare estimate**
When a rider enters a destination, before tapping "Confirm":
- The Ride Service calls the ETA Engine and Pricing Engine.
- The client receives route ETAs and estimated fares, including surge multipliers.
- No driver is assigned or contacted yet.

**Phase 2: Candidate ranking**
Once the rider taps "Confirm Ride":
1. Fetch candidates: DISCO maps the pickup to an S2/H3 cell and queries Redis for available drivers in that cell and its surrounding cells.
2. Batch routing and ranking: DISCO sends 10–20 drivers to the ETA Engine, which estimates road distance and drive time using traffic data.
3. Score candidates: DISCO considers ETA, driver rating, acceptance probability, and vehicle type.

**Phase 3: Lock and dispatch offer**
The selected driver has not accepted yet.
1. Acquire atomic lock: DISCO uses Redis (`SETNX lock:driver_123 ride_999 EX 15`) to reserve the top-ranked driver for 15 seconds.
2. Send the offer: If the lock succeeds, the notification service sends an offer to the Driver App.
3. Driver decision:
   - If accepted: The lock becomes an active trip record in the primary database.
   - If declined or timed out: The lock expires and DISCO tries Candidate #2.

**Phase 4: Match confirmation**
After the driver accepts:
- The system updates the ride state to MATCHED.
- The Notification Service sends the driver's details and ETA to the Rider App.
- The Rider App switches to the live vehicle map.

```text
Rider  → DISCO: request ride
DISCO  → Redis: find nearby drivers
DISCO  → ETA: rank candidates
ETA    → DISCO: return ETAs
DISCO  → Redis: lock top driver
DISCO  → Driver: send offer
Driver → DISCO: accept
DISCO  → Redis: save match
DISCO  → Rider: push driver details
```

### Data Model

**Location Update Payload:**
```json
{
  "driver_id": "drv_98765",
  "lat": 37.774929,
  "lng": -122.419416,
  "bearing": 180.5,
  "status": "AVAILABLE",
  "timestamp": 1770556443
}
```

**Redis Structures:**
- **Driver State (Hash):** `driver:state:{driver_id}` → `{ status, lat, lng, s2_cell_id, last_ping }`
- **Spatial Index (Sorted Set):** `s2:cell:{s2_cell_id}` → `{driver_id}` (only AVAILABLE drivers)

### Concurrency & Lock Management

DISCO uses atomic state transitions and Redis locks so two riders cannot match
the same driver at once.

#### The Core Problem: Race Conditions

If two riders request a ride at the same time, two DISCO instances could choose
Driver X and send offers unless the match is protected.

#### Basic Redis Atomic Lock (SETNX)

DISCO uses Redis `SETNX` (set if not exists) with a time-to-live:

```
SET driver:lock:drv_98765 "trip_id:ride_111" NX EX 15
```

- `NX`: Set only if the key does not exist.
- `EX 15`: Expire after 15 seconds.

#### Edge Cases & State Machines

**Case A: Driver Accepts**
If the driver accepts within 15 seconds, DISCO updates the status in Redis:
```
HSET driver:state:drv_98765 "status" "EN_ROUTE_TO_PICKUP"
```
The lock is deleted or expires, and the driver leaves the AVAILABLE spatial index.

**Case B: Driver Declines or Times Out**
- If declined: DISCO deletes the lock with `DEL driver:lock:drv_98765`.
- If timed out: Redis expires the key after 15 seconds. A background timer then tries Candidate #2.

**Case C: Lock Deletion Safety (Lua Script)**
If Thread A's lock expires and Thread B acquires the key, Thread A's raw `DEL`
could remove Thread B's lock. DISCO checks the trip ID in an atomic Lua script:

```lua
if redis.call("get", KEYS[1]) == ARGV[1] then
    return redis.call("del", KEYS[1])
else
    return 0
end
```

#### Redlock and distributed state

SETNX works on one Redis node. In a multi-region cluster, a primary could crash
before replicating the lock.

**Redlock:** DISCO writes to five independent Redis masters. It grants a lock
when at least three confirm `SETNX` within the timeout.

**Database check:** Before writing the trip record to Schemaless, DISCO runs a
conditional update:

```sql
UPDATE drivers
SET status = 'ON_TRIP', current_trip_id = 'ride_111'
WHERE driver_id = 'drv_98765' AND status = 'AVAILABLE';
```

If `affected_rows == 0`, another request updated the driver first, so the
transaction aborts.

| Scenario | Solution |
|----------|---------|
| Simultaneous match requests | Atomic SETNX lock prevents double-assignment |
| Driver unresponsive (15s) | Redis TTL auto-releases lock for next driver |
| Delayed execution | Lua script validates trip_id before releasing lock |
| Redis master failover | Redlock multi-node consensus or DB conditional update |

### Redis Infrastructure & Network Topology

#### App Instances vs. Redis Instances

App instances (Node.js, Go, or Java services such as DISCO) are stateless and
scale with CPU and memory. Redis instances hold driver locations and locks. On
separate machines, an app crash does not remove Redis state.

#### Redis Cluster Node Topology

In production, each Redis master or replica runs on its own VM. Putting three
masters on one VM would make one hardware failure affect the whole cluster.

#### Communication Protocols

**App to Redis (RESP):** The app uses TCP and RESP (Redis Serialization
Protocol). A connection pool keeps persistent sockets to the Redis nodes. The
client hashes each key and sends the command to its master.

**Between Redis nodes (gossip):** Nodes use a separate cluster bus to exchange
state and detect failures. If a master stops responding, replicas vote and one
is promoted.

#### Why Redis is Still Fast on a Different Machine

| Operation | Typical Time |
|-----------|-------------|
| Disk I/O (Database Read) | 5 ms to 20 ms |
| Redis Remote RAM Read + Network Latency | 0.5 ms to 1.5 ms |
| CPU Memory Read (Internal) | 100 nanoseconds |

Four factors help keep it fast:
- **Low data-center latency:** LAN between VMs is 0.2–0.8 ms.
- **In-memory speed:** Redis reads from RAM rather than disk.
- **TCP connection pooling:** Reuses sockets instead of handshaking per request.
- **Pipelining:** Bundles multiple commands into one TCP packet.

#### If a region loses its Redis nodes

**Scenario A: Cross-Region Failover (Active-Active)**
- The API Gateway detects the failure and shifts traffic to Region B.
- Region B runs its own independent Redis Cluster and App Instances.
- Recent location data might be lost, but driver apps reconnect and send a fresh
  ping within 4 seconds.

**Scenario B: Circuit Breaker Fallback**
- If cross-region routing is unavailable, the app stops sending requests to Redis
  to avoid timeouts.
- The App falls back to querying the persistent database (Cassandra, DynamoDB, or Schemaless) directly.
- Latency increases, but the feature can remain available.

### Scaling & Resiliency

| Bottleneck | Mitigation |
|-----------|-----------|
| Hotspot S2 cells (airport, stadium) | Shard Redis sets by `s2_cell_id + hash_slot`; use local memory caching for ultra-popular zones |
| Gateway socket exhaustion | Use Netty/epoll non-blocking I/O to hold 100k+ open WebSocket connections per instance |
| Driver connection drop | Background worker marks driver OFFLINE and removes from S2 index if no ping in > 12 seconds |

## Supporting Architecture Layers

### Data Mesh & Machine Learning Platform (Michelangelo)

Matching also uses real-time ML predictions:

- **Michelangelo:** Uber's ML platform for feature stores, model training, and inference.
- **Dynamic pricing (Surge):** Flink compares rider demand with available drivers per H3 cell. Michelangelo uses the result to update pricing multipliers.
- **DeepETA:** Neural networks estimate trip times from weather, traffic history, and road-level details.

### High Availability & Multi-Region Resiliency

Uber uses multi-region deployment to reduce downtime.

- **Active-active datacenters:** If a region fails, traffic can fail over to another region.
- **Stateful failover:** In-flight trip state is replicated across regions so navigation and fare tracking can continue.

### Payment Processing & Financial Settlement

Payments across currencies, methods, and tax jurisdictions require a separate domain:

- **Double-Entry Ledger:** Ensures financial consistency: a dollar charged to a rider must strictly balance across Uber's fee, driver payout, and local tax.
- **Payout Pipelines:** Real-time risk screening before pushing money to driver bank accounts or debit cards globally.

### Safety & Telematics Processing

Driver phones send gyroscope, accelerometer, and GPS data to Uber:

- Real-time anomaly detection flags sudden stops, crashes, or erratic driving.
- Safety features like crash detection trigger immediate customer support outreach via automated workflows.

### Open-Source Ecosystem Originated by Uber

Uber also built tools used in this architecture:

- **H3:** A hexagonal spatial index used alongside Google's S2 for grids and aggregation.
- **Jaeger:** A distributed tracing tool for following requests across services.
- **Cadence / Temporal:** Workflow engines for long-running operations such as cancellations, refunds, and onboarding.

| Challenge | Architectural Solution |
|-----------|----------------------|
| Geospatial Queries | Google S2 cells for spatial indexing and rapid lookups |
| High Write Ingestion | Apache Kafka for streaming millions of driver GPS pings/sec |
| Matching Speed | In-memory DISCO matching engine coupled with a custom ETA engine |
| High Availability | Active-active multi-datacenter deployment (if one region fails, another takes over instantly) |

## Historical Data Storage & Database Scaling

Historical trip storage differs from live driver tracking. Live tracking needs low
latency and temporary memory; historical data needs durable, scalable storage
and high write throughput.

Uber built **Schemaless**, a distributed datastore on MySQL, with a Hadoop/Data
Lake tier for long-term analysis.

### Schemaless: The Core Storage Engine

When a trip completes, it moves from temporary memory state to a permanent
record. At very large table sizes, indexes, migrations, and cross-node joins can
slow a relational database.

Schemaless is an append-only, key-value datastore built over clusters of MySQL instances:

```text
App services: Ride / Billing / Receipt
              |
              v
Schemaless worker: route, shard, datastore logic
           /          |          \
          v           v           v
      MySQL 1      MySQL 2      MySQL 3
```

**Key Design Principles:**

- **Append-only rows:** Trip details are not updated in place. A fare adjustment adds a new version.
- **No database-level indexes or joins:** MySQL stores data; indexing and relational logic stay in the application.
- **Cell entities:** Data is stored as JSON blobs called "cells" identified by three fields:
  - **Row key:** The `trip_uuid`.
  - **Column name:** Domain data such as `driver_info` or `fare_breakdown`.
  - **Ref key:** An increasing version number.

### Horizontal Scaling Strategies

**A. Dynamic sharding by trip_uuid:**
Schemaless maps each `trip_uuid` to a virtual shard with consistent hashing.
Virtual shards can move to new MySQL nodes as capacity changes.

**B. Functional sharding (domain isolation):**
Trip data is separated by domain so high-volume work does not affect billing:
- **Trip Datastore:** Core trip metadata (coordinates, timestamps, state history).
- **Payment Datastore:** Isolated cluster for strict ACID compliance and financial audit trails.
- **Driver Partner Datastore:** Earnings, payouts, and tax documentation.

### Tiered Storage: Hot, Warm, and Cold

Keeping decades of history in high-speed transactional storage is expensive, so
Uber uses tiers:

- **Hot (Schemaless / NVMe SSDs):** Active and recent trips (0–30 days) for fast API reads.
- **Warm (Cassandra / HBase):** Older trips for occasional point lookups.
- **Cold (Hadoop HDFS, Parquet, Apache Iceberg):** CDC events are written to columnar files for trends, model retraining, and fraud analysis.

### Multi-Region Data Replication

Uber uses active-active replication across regions:

- **Asynchronous multi-master replication:** Each region is primary for its local shards and replicates writes through Kafka.
- **Conflict resolution:** Append-only rows let concurrent writes add versions instead of overwriting each other. Reads resolve versions by timestamp.

| Need | Solution |
|------|---------|
| High Write Throughput | Schemaless: Append-only architecture over MySQL nodes |
| Horizontal Scalability | Consistent hashing by trip_uuid across virtual shards |
| Cost-Effective Retention | Data Tiering: Hot (Schemaless) → Warm (Cassandra) → Cold (Hadoop/Iceberg) |
| Analytical Querying | Kafka CDC pipelines streaming into a Parquet-based Data Lake |

### Change Data Capture (CDC): operational to analytical bridge

CDC connects the operational database to Kafka, analytics, and the data lake.
It streams mutations from the binlog instead of polling the live database with
heavy SQL queries.

#### The CDC Pipeline Architecture

```text
Schemaless/MySQL → binlog → StorageTapper → Kafka
                                             ├─→ Flink / Pinot
                                             └─→ Hudi/Marmaray → HDFS/S3
```

#### Step-by-Step Data Journey

**Step A: capture binlog events (StorageTapper)**

When a driver completes a trip, MySQL writes the mutation to its binary log.
StorageTapper reads the binlog like a secondary replica without locking tables or
running SQL queries.

**Step B: schema enforcement and serialization (Apache Avro)**

StorageTapper turns the raw binlog data into a shared event format:

- It maps table columns to Apache Avro using the Schema Service.
- The event contains:
  - **Operation Type:** INSERT, UPDATE, DELETE
  - **Metadata:** Database name, table name, commit timestamp, log position offset
  - **Payload:** before_image (old row values) and after_image (new row values)

**Step C: stream to Apache Kafka**

StorageTapper publishes events to Kafka topics such as `schemaless.trip_events`.
Kafka provides:

- **Decoupling:** Producers do not need to know the consumers.
- **Replayability:** Consumers can rewind offsets and reprocess events.
- **Fan-out:** One event can feed analytics, audit logs, and cold storage.

#### Ingesting CDC Streams into the Data Lake (Hadoop/Hudi)

HDFS is designed for large immutable files, while CDC produces many small,
out-of-order updates. Apache Hudi combines these updates into queryable files.

**How Hudi Handles CDC Incremental Writes:**

- **Upserts via Record Key Indexing:** Hudi maintains a record key index (e.g., indexed by trip_uuid). When a CDC record arrives in Kafka for an existing trip, Hudi knows which Parquet data file on HDFS contains that trip.
- **Copy-on-Write (COW) vs. Merge-on-Read (MOR):**
  - **Merge-on-Read (MOR):** Incoming CDC updates are appended to fast, lightweight Delta Logs (Avro format).
  - **Compaction:** A background compaction job periodically merges the Delta Logs with the historical base files (Parquet format), creating a fresh, highly compressed columnar snapshot for analytical queries.

#### Key Engineering Challenges & Solutions

| CDC Challenge | How Uber Solved It |
|---------------|--------------------|
| Schema Evolution | Upstream table schemas change over time (e.g., adding new columns). Uber uses an Avro Schema Registry. If a breaking schema change occurs, StorageTapper flags it and prevents invalid data from corrupting the Data Lake. |
| Data Ordering & Deduplication | Distributed Kafka topics can sometimes deliver events out-of-order or duplicate them. Every CDC event contains the source database transaction timestamp and sequence ID. Hudi uses these fields to apply changes in exact chronological order. |
| Cross-Region Replication | Uber built uReplicator (an optimized alternative to Kafka MirrorMaker) to mirror CDC Kafka topics between geographically distant datacenters without losing offsets or introducing lag. |

#### Summary checklist

1. Schemaless / MySQL accepts the trip write and appends it to the binlog.
2. StorageTapper reads the binlog and converts it to Avro CDC events.
3. Events land in Apache Kafka within seconds.
4. Apache Hudi / Marmaray consumes CDC messages and upserts Parquet files on HDFS.
5. Data engineers and ML models query the Parquet data with Presto, Hive, or Spark.

## Durable execution and financial ledger

Uber uses Durable Execution (Cadence/Temporal) for multi-step processes and
double-entry accounting (Gulfstream) for financial accuracy.

### Distributed Workflows: Cadence / Temporal

When a trip is canceled, several services must charge a fee, notify the driver,
update availability, issue credits, and adjust matching. HTTP calls or queues can
lose state if a service fails midway, causing duplicate charges or unfinished
transactions.

Cadence, now continued in the open-source community as Temporal, provides
durable execution for this flow.

**Workflows vs. activities:**

Cadence splits the code into two concepts:

- **Workflows (state logic):** Deterministic code that defines order and waits for signals. It does not call APIs, read the clock, or generate random values directly.
- **Activities (side effects):** Non-deterministic work such as charging a card, sending an SMS, or calling an API. Activities can fail, time out, and retry independently.

**Replay-based recovery:**

Cadence uses event sourcing rather than memory snapshots:

```text
Workflow started
      ↓
Activity scheduled: ChargeFee
      ↓
Activity completed: success
```

After each Activity, Cadence stores an event in the history database. If a worker
dies, a new worker replays the Workflow. It sees the completed event, skips the
payment call, and resumes with the stored result.

**Saga pattern and compensation:**

Without 2-Phase Commit, Cadence uses the Saga pattern. If a late step fails,
compensation runs in reverse:

```go
func CancellationWorkflow(ctx workflow.Context, tripID string) error {
    var saga CompensationSaga

    err := workflow.ExecuteActivity(ctx, ReserveDriverPayout, tripID).Get(ctx, nil)
    if err != nil { return err }
    saga.AddCompensation(ReleaseDriverPayout, tripID)

    err = workflow.ExecuteActivity(ctx, ChargeRiderFee, tripID).Get(ctx, nil)
    if err != nil {
        saga.Compensate(ctx)
        return err
    }
    return nil
}
```

### Financial ledger and double-entry bookkeeping (Gulfstream)

Money movement needs an audit trail. Updating one balance field would not provide
that, so Gulfstream uses double-entry bookkeeping.

**Rule:** Money is neither created nor destroyed.

Every monetary movement is an immutable transaction where:

$$\sum \text{Debits} = \sum \text{Credits}$$

Every balance is the sum of its ledger entries.

**Example: $20 fare with a $5 promo code**

For a $20 ride with a $5 promo code, Uber keeps a $3 fee and the driver earns
$17. Gulfstream writes four entries in one balanced transaction:

| Account | Entry Type | Amount |
|---------|-----------|--------|
| Rider:Account | Debit (Asset reduction/Payment) | $15.00 |
| Uber:MarketingPromo | Debit (Expense/Subsidy) | $5.00 |
| Driver:Account | Credit (Liability/Owed to driver) | $17.00 |
| Uber:Revenue | Credit (Revenue retained) | $3.00 |

$$\text{Total Debits } (\$15 + \$5 = \$20) \equiv \text{Total Credits } (\$17 + \$3 = \$20)$$

**Account scaling (batching and concurrency):**

A ledger can receive concurrent writes to shared accounts. Row locks can become a
bottleneck.

Uber groups updates with a 250 ms User Account Batch Processing Engine:

```text
Incoming ledger requests
          ↓
Batch creator (Redis)
          ↓
Batch process service
          ↓
User account store
          ↓
Async audit service (UAC)
```

- **Batching:** Updates to one account are grouped into 250 ms windows.
- **Single read/write:** The engine reads an account once, applies the updates in memory, and writes the result once.
- **Optimistic locking:** The update checks the account version and retries on conflict.
- **Asynchronous audit logging:** Kafka moves the User Account Changelog (UAC) off the request path.

### Architectural Comparison

| Need | Distributed Workflow (Cadence) | Financial Ledger (Gulfstream) |
|------|-------------------------------|------------------------------|
| Primary Goal | Orchestrate long-running, multi-step business logic without dropping state | Guarantee mathematical correctness and auditability of funds |
| Failure Recovery | Replay-based recovery from event history logs; Saga compensation rollbacks | Atomic batch updates; double-entry balance constraints ($\sum \text{Debits} = \sum \text{Credits}$) |
| Consistency Model | Eventual consistency across microservices via orchestrator tasks | Strict serializability and ACID compliance at the account entry level |
| Throughput Strategy | Decoupled background task queues and priority-based scheduling | 250ms time-window batching with optimistic locking in Redis |

### Workflow Design Hierarchy: Steps, Flows, and Journeys

Choosing boundaries for Steps (Activities), Flows (Child/Parent Workflows), and
Journeys (Entities) affects event-history limits and maintainability. Small
boundaries can create too many events; large ones become hard to recover or test.

**Tier 2: Step (Activity)** A unit that performs I/O, non-deterministic work,
retriable work, or heavy computation. Keep pure validation, mapping, and math in
the Workflow.

**Tier 3: Flow (Child / Sub-Workflow)** A bounded business sequence. Use one for
reusable work, a separate failure domain, or a history that should be isolated.

**Tier 4: Journey (Entity / Long-Running Workflow)** Models a long-lived entity
such as a Driver or Vehicle. Use Signals for state changes and `ContinueAsNew` to
truncate history before the event limit.

**Decision Matrix:**

| Question | Step (Activity)? | Flow (Sub-Workflow)? | Journey (Entity)? |
|----------|:---:|:---:|:---:|
| Calls an external API or DB? | YES | No | No |
| Should be retried independently? | YES | No | No |
| Represents an entire business task? | No | YES | No |
| Will generate thousands of events? | No | YES (isolates history) | YES (uses ContinueAsNew) |
| Listens for signals over months? | No | No | YES |

**Example: driver onboarding**

```go
// TIER 4: JOURNEY (Entity Workflow - Driver Lifetime)
func DriverJourneyWorkflow(ctx workflow.Context, driverID string) error {
    state := InitialDriverState()

    for {
        var signal DriverSignal
        workflow.GetSignalChannel(ctx, "driver-events").Receive(ctx, &signal)

        switch signal.Type {
        case "SUBMIT_DOCUMENTS":
            // TIER 3: FLOW (Child Workflow)
            err := workflow.ExecuteChildWorkflow(ctx, DocumentVerificationFlow, driverID).Get(ctx, nil)
            if err == nil { state.IsVerified = true }

        case "RETIRE_DRIVER":
            return nil
        }

        if workflow.GetInfo(ctx).HistoryLength > 20000 {
            return workflow.NewContinueAsNewError(ctx, DriverJourneyWorkflow, driverID, state)
        }
    }
}

// TIER 3: FLOW (Sub-Workflow)
func DocumentVerificationFlow(ctx workflow.Context, driverID string) error {
    err := workflow.ExecuteActivity(ctx, CallBackgroundCheckAPI, driverID).Get(ctx, nil)
    if err != nil {
        _ = workflow.ExecuteActivity(ctx, SendRejectionEmail, driverID).Get(ctx, nil)
        return err
    }
    return nil
}
```

### Concrete Example: Uber Eats Order Fulfillment

The same four tiers can coordinate an Uber Eats order across its roughly
45-minute lifecycle.

**OrderFulfillmentJourney:**

```text
Customer order
      ↓
Payment & authorization
      ↓
Restaurant preparation
      ↓
Courier dispatch & pickup
      ↓
Delivery & hand-off
```

Signals update the stage asynchronously: restaurant acceptance starts
preparation, courier arrival updates pickup, and delivery confirmation closes
the flow.

**Flow A: Payment Authorization** Runs before notifying the restaurant. If
payment fails, the restaurant is not notified.

**Flow B: Restaurant Fulfillment** Waits for `AcceptOrder(prepTimeMinutes)`,
`RejectOrder(reason)`, or a five-minute timeout.

**Flow C: Courier Dispatch & Matching** Uses a timer so the courier arrives as
the food finishes cooking:

$$\text{Dispatch Delay} = \text{Target Pickup Time} - \text{Estimated Driver Transit Time}$$

**Tier 2 Activities:**

| Activity | Failure & Retry Policy |
|----------|----------------------|
| AuthorizePayment | Retry 3x on network failure; fail immediately on card decline |
| SendOrderToRestaurantPOS | Exponential backoff for 3 min; fallback to IVR phone call |
| AssignCourierLock | Short retry (15s SETNX timeout per candidate) |
| CapturePayment | Retry indefinitely (durable finance step) |
| SendPushNotification | Fire-and-forget; low-priority retry |

**Production Go Implementation:**

```go
func OrderFulfillmentJourney(ctx workflow.Context, orderID string) error {
    var saga CompensationSaga

    // Phase 1: Payment Authorization
    var paymentAuth PaymentAuthResult
    err := workflow.ExecuteChildWorkflow(ctx, PaymentAuthorizationFlow, orderID).Get(ctx, &paymentAuth)
    if err != nil {
        return err
    }
    saga.AddCompensation(VoidPaymentAuthorization, paymentAuth.AuthCode)

    // Phase 2: Restaurant Fulfillment
    var prepResult RestaurantPrepResult
    err = workflow.ExecuteChildWorkflow(ctx, RestaurantFulfillmentFlow, orderID).Get(ctx, &prepResult)
    if err != nil {
        saga.Compensate(ctx)
        return err
    }

    // Phase 3: Timed Courier Dispatch
    dispatchDelay := prepResult.EstimatedReadyTime.Sub(workflow.Now(ctx)) - EstimatedCourierTransitTime
    if dispatchDelay > 0 {
        workflow.Sleep(ctx, dispatchDelay)
    }

    var courierResult CourierMatchResult
    err = workflow.ExecuteChildWorkflow(ctx, CourierDispatchFlow, orderID, prepResult.RestaurantLocation).Get(ctx, &courierResult)
    if err != nil {
        workflow.ExecuteActivity(ctx, CancelRestaurantOrder, orderID)
        saga.Compensate(ctx)
        return err
    }

    // Phase 4: Delivery & Payment Capture
    var deliverySignal DeliveryConfirmationSignal
    workflow.GetSignalChannel(ctx, "delivery-channel").Receive(ctx, &deliverySignal)

    if deliverySignal.Status == "DELIVERED" {
        return workflow.ExecuteActivity(ctx, CapturePayment, orderID, paymentAuth.AuthCode).Get(ctx, nil)
    }
    saga.Compensate(ctx)
    return fmt.Errorf("delivery failed")
}
```

**Key takeaways:**
- **Failure isolation:** A restaurant rejection voids the card hold without dispatching a courier.
- **Durable timers:** `workflow.Sleep` survives server restarts.
- **Independent scaling:** Payment, POS, and courier workers scale separately.

## Edge Infrastructure, Identity & Rate Limiting

The edge serves mobile clients, web apps, and partners before requests reach
DOMA services. Its layers handle routing, identity, and rate limiting.

### Edge Architecture Topology

Uber's edge uses two gateway tiers: public threat filtering and internal business routing.

**Tier 1: Anycast & Public Edge (Cloudflare WAF)**

- Anycast routing sends traffic to a nearby global PoP.
- DDoS and L7 inspection blocks network and HTTP floods before they enter Uber's datacenters.
- TLS termination near the user supports long-lived TCP/gRPC connections to the origin.

**Tier 2: Core API Gateway (Envoy Proxy)**

Inside the network, the Envoy gateway handles four functions:
- **Protocol translation:** Converts external REST/JSON or HTTP/2 gRPC to internal gRPC, Thrift, or Protobuf.
- **Path and tenant routing:** Sends `/v1/trips` and `/v1/eats` to the right service cluster using request metadata and deployment flags.
- **Edge authentication:** Converts public bearer tokens into internal identity objects.
- **Resiliency:** Applies timeouts, backoff retries, and circuit breakers.

### Security, OAuth2 & Identity Engineering

The identity pipeline uses external OAuth2 tokens for clients and internal
Passports for microservices.

**External authentication (OAuth2):** On login, the Identity Service issues a
short-lived access token and a refresh token. Mobile clients send the access
token in the `Authorization` header.

**Identity Passport pattern:** The Edge Gateway exchanges the public token for a
signed Passport containing validated user metadata. Services can verify it
locally instead of calling the Identity Service.

```json
{
  "user_id": "usr_9921_sf",
  "user_type": "DRIVER",
  "device_id": "dev_iphone_8832",
  "authenticated_at": 1770562800,
  "scopes": ["trips:read", "location:write"]
}
```

The Passport is HMAC-signed with a key shared across the internal mesh. Services
verify the signature locally.

**Service-to-service security (SPIFFE/SPIRE and mTLS):** Each workload receives a
cryptographic identity. SPIRE issues short-lived X.509 certificates, and sidecar
proxies enforce which services may communicate.

### Distributed Rate Limiting (Radix Engine)

Rate limiting runs at multiple tiers against credential stuffing, API abuse, and
runaway clients. Radix uses Redis Cluster as a sliding-window store.

```text
Incoming request → Edge gateway → Redis counter
                                      ├─ under limit → Microservices
                                      └─ exceeded   → HTTP 429
```

**Sliding window counter:** Radix uses atomic Lua scripts with `INCRBY` and
`EXPIRE` over time buckets instead of a fixed window:

$$\text{Current Weight} = \text{Count}_{\text{current}} + \text{Count}_{\text{previous}} \times \left(1 - \frac{\text{Time elapsed in current window}}{\text{Window duration}}\right)$$

**Token bucket:** Used for bursty endpoints such as driver location pings. It
allows bursts up to a capacity, then applies the refill rate.

**Multi-Dimensional Rate Limit Keys:**

| Target Scope | Key Definition | Purpose |
|-------------|----------------|---------|
| IP-Based (Global) | `ip:{client_ip}:all` | Blocks botnets and global scraping |
| Authentication | `ip:{client_ip}:endpoint:/v1/login` | Brute-force prevention (max 5 attempts/min) |
| Per-User Endpoint | `user:{user_id}:endpoint:/v1/payment` | Prevents duplicate credit card charge attempts |
| Partner API | `client_id:{partner_app}:all` | Enforces tier-based B2B developer API limits |

### Architecture Summary

| Security Tier | Core Technology | Operational Benefit |
|---------------|-----------------|---------------------|
| Public Perimeter | Cloudflare Anycast + Envoy Edge Gateway | Anycast routing, L3/L4 DDoS scrubbing, TLS termination |
| Public Auth | OAuth2 (Short-lived Access + Refresh Tokens) | Standardized secure authentication for mobile & web |
| Internal Auth | Passport Pattern (Token-to-Passport Swap) | Eliminates auth-service bottlenecks; local HMAC verification |
| Service Identity | SPIFFE/SPIRE + Mutual TLS (mTLS) | Zero-trust service mesh preventing lateral movement |
| Rate Limiting | Radix Engine (Redis Sliding Window Counters) | Multi-dimensional protection against DDoS, brute-force, and API abuse |
