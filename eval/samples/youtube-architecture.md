---
title: "YouTube Architecture"
description: "A conceptual video platform architecture covering ingestion, DAG transcoding, adaptive bitrate, live streaming, and global delivery."
seoTitle: "YouTube-Style Video Architecture"
seoDescription: "A conceptual design for video uploads, DAG transcoding, adaptive bitrate packaging, live streaming, storage, CDN delivery, and recommendations."
answerSummary: "This conceptual architecture explains how a video platform can turn uploads into multiple renditions and deliver them efficiently through asynchronous processing and edge caching."
tags: [system-design, distributed-systems]
created: 2026-09-05
---
# YouTube-Style Video Ingestion and Delivery Architecture

This conceptual video-platform architecture turns uploads into multiple renditions
through asynchronous processing, then uses manifests, storage tiers, and edge
caches to deliver the right stream for each viewer.

It uses YouTube as a reference model; the implementation details below are an
engineering explanation, not an official description of YouTube's current systems.

## Video Ingestion & DAG Transcoding

When a creator uploads a video, YouTube processes it through an asynchronous
Directed Acyclic Graph (DAG) instead of one large conversion job:

- **Chunking**: The upload service splits the video into 2–5 second GOP (Group of Pictures) chunks for parallel processing.
- **Transcoding DAG**: Workers encode chunks into AV1, VP9, and H.264/AVC at multiple resolutions and frame rates.
- **Parallel workflows**: Other DAG nodes create thumbnails, extract audio, generate captions, and scan audio against Content ID.
- **Adaptive bitrate (ABR) packaging**: Encoded chunks are indexed in MPEG-DASH and HLS manifests (`.mpd` or `.m3u8`). The client selects quality based on network bandwidth and device performance.

## Storage Tier: Blobs vs. Relational Data

YouTube separates video media from transactional metadata:

| Storage Layer | Technology Used | Functional Role |
| --- | --- | --- |
| Blob Storage | Google Cloud Storage / Colossus | Stores original source videos and generated encoded chunk segments. |
| Metadata Storage | Vitess (MySQL sharding framework) | Stores structured data including user accounts, video channel maps, video titles, and privacy configurations. |
| NoSQL / Key-Value | Bigtable / Spanner | Stores high-volume time-series data such as watch history, user preferences, and analytics traces. |
| Distributed Caching | Memcached / Redis | Caches hot metadata, channel info, and session states to protect database backends from read spikes. |

### Handling High-Volume Writes (View Counter Problem)

For viral videos, view writes are buffered in an event stream, aggregated by
workers, and committed to the database in batches.

## Global Content Delivery (CDN & Edge Strategy)

Video is delivered through Google Global Cache (GGC), with edge servers inside
ISP networks to reduce buffering.

- **Hot content**: Cached on NVMe/SSD edge servers near the user.
- **Warm content**: Kept in regional Google data-center caches.
- **Cold content**: Read from central blob storage over Google's backbone.

## Recommendation & Search Engine

YouTube uses a two-stage Deep Neural Network (DNN) to build personalized feeds:

- **Candidate generation**: The model uses user history, search tokens, and other signals to reduce millions of videos to a few hundred candidates.
- **Ranking**: The model scores candidates with signals such as click-through rate, watch time, and satisfaction, then orders the feed.

## Live Streaming Engine (YouTube Live)

Live streaming cannot use the standard VOD DAG because encoding must happen in
real time:

```text
Broadcaster
     │ RTMP/WebRTC
     v
Live ingress → Transcoder → LL-DASH → CDN → Viewer (1–3 s)
                   │
                   └─→ DVR store
```

- **Ingress protocols**: Broadcasters push raw streams using RTMP, WebRTC, SRT, or DASH-PUT.
- **Low-Latency DASH (LL-DASH) and CMAF**: Transcoders send sub-second chunks to the edge as they are generated.
- **DVR engine**: Writes live segments to Colossus so viewers can rewind.

### VOD vs. Live: Architecture Comparison

**Video-on-demand** favors compression and image quality; processing can take
minutes. **Live streaming** favors a 1–3 second broadcaster-to-viewer delay, so
frames must be processed as they arrive.

| Architectural Dimension | VOD Pipeline | Live Pipeline |
| --- | --- | --- |
| Primary Goal | Reduce bandwidth and storage. | Reduce broadcast-to-viewer delay. |
| Ingestion Protocol | Resumable HTTP POST / gRPC chunk uploads. | Push protocols (RTMP, WebRTC, SRT, or DASH-Ingest). |
| Processing Engine | Asynchronous DAG across thousands of nodes. | Continuous stream pipeline (real-time workers with small memory buffers). |
| Encoding Passes | Multi-pass encoding optimizes bitrate across the video. | Single-pass hardware encoding with no lookahead. |
| Codecs Used | Compute-intensive (AV1, VP9, H.264). | Fast codecs with hardware acceleration. |
| Delivery Protocols | Standard MPEG-DASH / HLS (2-6 s static chunks). | LL-DASH / CMAF w/ HTTP Chunked Transfer (sub-second sub-chunks). |
| Caching Strategy | Static files aggressively cached on CDN edges (high hit ratio). | Live ring buffer in memory; consumed as it is generated. |
| Storage Engine | Permanent Blob Storage (Google Colossus). | Temporary volatile RAM/SSD ring buffer + async dump to Colossus for DVR. |

### Three Core Architectural Divergences

**1. Ingestion & Transcoding: DAG vs. Pipeline.**
- **VOD (batch)**: The complete file is split into GOP chunks, encoded in parallel, and assembled.
- **Live (stream)**: Bytes arrive continuously. A live transcoder keeps a small frame buffer, encodes frames on the fly, and sends them to the distribution network.

**2. CDN Mechanics: Pull vs. Push/Chunked Streaming.**
- **VOD (pull)**: The player requests a 4-second chunk (`chunk_10.m4s`); the edge fetches it from origin on a cache miss.
- **Live (push / chunked transfer)**: A 2-second chunk is split into ~100 ms sub-chunks and streamed to the CDN while it is being recorded.

```text
VOD:  full 4 s chunks → Origin → CDN edge → Viewer
Live: 100 ms chunks    → CDN edge → Viewer
```

**3. State & Storage: Static Blobs vs. the "DVR Loop".**
- **VOD**: Video files are immutable blobs in Google Colossus.
- **Live**: A circular ring buffer drops the oldest segments as new video arrives. A DVR worker writes rewind data to permanent blob storage.

### Where the Architectures Merge (Live → VOD)

When a live broadcast ends, a post-live task stitches the DVR chunks in
Colossus, creates a permanent VideoID, and sends the recording to the VOD DAG.
Multi-pass encoding then produces AV1 files, chapters, and captions.

## Content ID and copyright matching

Before or during publication, audio and video are checked against Content ID:

- **Digital fingerprinting**: Audio becomes spectral fingerprints; visual frames become perceptual hashes.
- **Reference search**: Fingerprints are compared with a database of copyrighted assets.
- **Policy router**: A match triggers **Block**, **Track Analytics**, or **Claim Revenue**.

## Monetization and ad insertion (SSAI vs. CSAI)

Targeted ads require real-time ad decisions:

- **Server-side ad insertion (SSAI)**: The ad server inserts personalized segments into the manifest at GOP boundaries.
- **Client-side ad insertion (CSAI)**: The player requests ad tags, pauses the main video, fetches the ad, and resumes playback.

## Real-time analytics and data pipeline

YouTube processes petabytes of telemetry per hour, including watch time,
impressions, click-through rates, and drop-off rates:

- **Stream processing**: Google Cloud Dataflow, Apache Flink, or MillWheel.
- **Anti-fraud and bot detection**: Filters fake views, scrapers, and click farms before public counters or billing.
- **Aggregated storage**: Writes cleaned metrics to Bigtable and ClickHouse/Spanner for Creator Studio.

## Comments, Community & Notifications

- **Fan-out notifications**: A publish-subscribe bus sends upload notifications to subscribers in batches.
- **Comment graph**: Vitess stores comments as a tree; write buffers absorb spikes on busy channels.

### Live Chat at Scale

Chat for millions of viewers creates a high fan-out load. With 2M viewers and
5,000 messages/second, pushing every message would require about 10 billion
deliveries per second. The design uses decoupled ingestion, server-side
sampling, tiered fan-out, and adaptive polling.

```text
User message → API gateway → Moderation → Kafka/Pub/Sub
                                               ↓
                                         Batch sampler
                                               ↓
                                    Edge cache + continuation token
                                               ↓
                                           Viewer app
```

**Write path (ingestion & moderation):**
- **Rate limiting**: The API gateway enforces per-user token buckets, adjusted by channel "Slow Mode".
- **Synchronous moderation**: ML classifiers scan for blocked terms, links, and toxic content.
- **Super Chat validation**: Paid messages are validated and marked with priority and pin duration.
- **Event ingest**: Validated messages enter a Kafka/Pub/Sub topic partitioned by `LiveStreamID`.

**Server-side sampling and throttling**
People read only about 3–5 messages/s. Sending 5,000 messages/s wastes
bandwidth and overloads the browser. The server ranks messages in a time window
and limits the output:

$$\text{Priority Score} = f(\text{SuperChat Value}, \text{Subscriber Status}, \text{Moderator Badge}, \text{User Engagement})$$

- **Low-volume stream** (<5 msgs/s): all messages pass through.
- **Viral stream** (>1,000 msgs/s): keep Super Chats and moderator messages, then sample regular messages to about 3–5 msgs/s; drop the rest server-side.

**Read path: adaptive HTTP batch polling.**
Unlike 1:1 messaging apps that use persistent WebSockets, live chat uses an
adaptive `get_live_chat` batch-polling endpoint. This avoids keeping millions of
stateful sockets for one stream:

```json
{
  "actions": [ ],
  "continuationToken": "EiQxMj...",
  "pollIntervalMs": 1500
}
```

The server returns sampled messages with a `continuationToken` and
`pollIntervalMs`. The edge raises the interval when volume drops and lowers it
when chat spikes.

**Tiered memory caching:** Edge servers do not read chat from the primary DB:

| Storage Tier | Technology | Purpose |
| --- | --- | --- |
| L1 Edge Cache | In-memory Edge Ring Buffer | Holds the last 10-30 s of sampled chat batches on CDN/Edge nodes. |
| L2 Aggregation Cache | Distributed Redis / Memcached | Sliding time-window state per `LiveStreamID`; serves L1 misses. |
| L3 Persistent Store | Bigtable / Spanner | Asynchronously writes full (unsampled) chat logs for Live Chat Replay on VODs. |

Most poll requests are served from L1/L2, avoiding primary database reads.

**Client-side virtualization (browser & app):**
- **DOM virtualization**: The client keeps a bounded buffer and drops the oldest items as new ones arrive.
- **Animation queueing**: Batches enter a JavaScript queue and appear every 200–300 ms to keep the chat readable.

## Security, DRM & Asset Protection

- **Widevine DRM**: Premium content (e.g., YouTube Movies/Rentals) uses AES-128 encryption via Encrypted Media Extensions (EME). Encrypted segments sit on the CDN; the browser must fetch a hardware-backed decryption key from Google's License Server.
- **Signed CDN URLs**: CDN media URLs carry short-lived cryptographic tokens bound to the user's IP address and session ID to prevent unauthorized hotlinking or scraping.

## Deep Dives

### What is a GOP chunk, and does it have an ID?

A video is a sequence of image frames. Compression does not store a full image
for every frame:

- **I-frame (keyframe)**: A complete standalone image.
- **P/B-frames (delta frames)**: Store only changes since earlier frames.

A Group of Pictures (GOP) is a self-contained group that starts with an I-frame.
Video is split at GOP boundaries, usually every 2–5 seconds, so a chunk starts
with a complete frame.

**Do they have unique IDs?** Yes. A deterministic name and timestamp identify
each chunk, and the manifest lists the chunks in playback order.

### Why use multiple codecs?

Resolution is the screen size (e.g., $1920 \times 1080$). A codec compresses
pixels into bytes. YouTube uses multiple codecs for compatibility and bandwidth:

- **H.264 (AVC)**: A widely supported standard with larger files.
- **VP9**: Developed by Google and more efficient than H.264 at similar quality.
- **AV1**: An open codec with high compression and higher encoding cost.

YouTube serves AV1 or VP9 to compatible devices and falls back to H.264 when
needed.

### Why use a DAG and workflow engine?

A DAG is a workflow of steps (nodes) connected by one-way paths (edges), with no
cycles. One upload creates tasks that can run in parallel:

```text
Upload raw video
├─→ split into chunks → encode 1080p / 720p / 360p → manifest
├─→ extract audio → speech-to-text captions
└─→ sample frames → Content ID scan
```

**Why DAG is crucial:**
- **Parallel execution**: Independent tasks such as speech-to-text and encoding run at the same time.
- **Fault tolerance**: If chunk #42 fails, only that task is retried.

**The DAG is not the entry point.** A separate workflow engine tracks state and
assigns tasks to workers.

| Feature | Entry Point (API Gateway) | Workflow Engine (Temporal / DAG engine) |
| --- | --- | --- |
| Primary Role | Accepts traffic, validates auth, ingests file. | Orchestrates multi-step dependencies, maintains cross-server state. |
| Lifespan | Short-lived (seconds); ends once upload is stored. | Long-lived (minutes-hours); lives until all encoding completes. |
| State Handling | Stateless. | Stateful: tracks which chunks succeeded, failed, or are running. |

**At the entry point:**
1. The creator hits "Upload"; the gateway streams raw bytes into Google Cloud Storage (Colossus).
2. It assigns a `VideoID` and creates a row in Vitess with status `PROCESSING`.
3. It fires a trigger: `Start Workflow "ProcessVideo" for VideoID: 9x2A_kL`.
4. The connection closes.

**What the workflow engine adds:** durable orchestration, runtime expansion,
heartbeat retries, and dependency ordering:
- **Dynamic fan-out/fan-in**: A long video can expand into many parallel chunk tasks, then wait for them before generating the manifest.
- **Durable retries**: If a worker fails, only its chunk is re-queued.
- **Dependency management**: Thumbnails can start from one chunk, while speech-to-text waits for all audio chunks.

**Mapping to Temporal concepts** (if you rebuilt ingest with Temporal today):

| YouTube DAG Concept | Temporal Equivalent | Responsibility |
| --- | --- | --- |
| Ingestion Pipeline | Workflow | Defines task order, timeouts, and retry policies. |
| Chunk Transcode Task | Activity | The CPU/GPU-heavy function that encodes one chunk. |
| Transcoding Servers | Workers | Clusters polling Temporal task queues for jobs. |
| DAG Orchestrator | Temporal Server | Persists workflow/DAG state, timers, and retries. |

### Why separate audio and video, and what is ABR packaging?

**Why separate audio and video?** If bandwidth drops, the player can switch video
quality while keeping one continuous audio track. A combined file would make
that switch harder.

**What is ABR packaging?** It does not merge chunks into one video. It creates a
manifest (`.mpd` for MPEG-DASH or `.m3u8` for HLS) that indexes the encoded
audio and video chunks. The manifest contains:

- The web addresses (URLs) of every audio chunk.
- The web addresses of every video chunk broken down by resolution and codec.
- The exact timestamp alignment for every chunk.

**Playback:**
1. The client downloads the manifest.
2. It selects audio and video chunks based on network speed.
3. The media player synchronizes the streams and renders them together.
