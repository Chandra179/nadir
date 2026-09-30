#!/usr/bin/env python3
"""Build a deterministic sample-corpus regression pack; never a release gate.

The original math golden set is read, not rewritten. New reference answers
describe the committed documents, including conceptual architecture examples,
rather than attesting that those examples describe current external systems.
"""

from __future__ import annotations

import argparse
import copy
import hashlib
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
MATH_IDS = [
    "secant-trig-definition", "tangent-ratio", "sin-45-degrees", "degrees-to-radians-30",
    "secant-method-root-finding", "secant-formula-iteration", "root-method-speed-comparison", "secant-no-derivative",
    "derivative-power-rule", "derivative-chain-rule", "integral-natural-log", "integral-power-rule",
    "vector-definition", "dot-product", "matrix-multiplication-dimensions", "eigenvalues-characteristic-polynomial",
]

# file, id, type, query, exact evidence substring, reference answer.
CASES = [
    ("api-design-guidelines", "api-currency-type", "factoid", "How do the internal API guidelines represent currency without rounding errors?", "Use **Integers** as currency", "Use integers for currency and pair the value with an ISO 4217 currency code."),
    ("api-design-guidelines", "api-empty-vs-missing", "comparison", "What HTTP responses do the guidelines require for an empty search and a missing specific resource?", "HTTP Semantics & Status Codes", "An empty collection search returns 200 OK with []; a missing specific resource returns 404 Not Found."),
    ("api-design-guidelines", "api-long-running", "procedure", "How should an API handle a long running operation without blocking the HTTP request?", "For long running operations", "Return 202 Accepted with a Location header pointing to a status endpoint, or use webhooks."),
    ("api-design-guidelines", "api-coalescing", "procedure", "How does request coalescing handle concurrent identical upstream requests?", "When a request arrives for key", "Check for an in-flight call for that key and subscribe to its result instead of creating another connection."),
    ("business", "business-trust-institutions", "factoid", "Which institutions does the business map associate with trust?", "Money, contracts, reputation, law", "Money, contracts, reputation, and law address trust."),
    ("business", "business-embedded-fintech", "comparison", "How does embedded fintech differ from a standalone digital bank in the business map?", "embedded fintech puts them inside", "A digital bank delivers banking through its own app; embedded fintech puts financial services inside another company's software."),
    ("business", "business-fintech-revenue", "factoid", "How can the embedded fintech providers in the business map combine recurring and volume-based revenue?", "Subscriptions provide", "They combine subscriptions for recurring revenue with a share of transaction fees that grow with volume."),
    ("business", "business-inventory-pricing", "factoid", "What drives subscription tiers for inventory tools in the business map?", "Inventory tools", "Product count, connected sales channels, and monthly orders drive the subscription tiers."),
    ("cache", "redis-execution", "factoid", "How does the Redis guide distinguish command execution from network I/O?", "main execution thread", "Redis mainly executes commands one at a time on its main thread; network I/O and background work can use other threads."),
    ("cache", "redis-slot-formula", "formula", "What is the formula and total slot count used by Redis Cluster?", "slot = CRC16(key) mod 16384", "Redis Cluster uses 16,384 hash slots and slot = CRC16(key) mod 16384."),
    ("cache", "redis-lock-release", "procedure", "Why must a worker compare its token before releasing a Redis lock?", "delete the marker only if the token still matches", "An expired worker must not delete a newer worker's lock; delete only if the token still matches."),
    ("cache", "redis-lua-transaction-limit", "comparison", "Does a Redis Lua script make a later SQL write atomic too?", "not general rollback or a transaction with another service", "No. Redis scripts provide atomic execution in Redis, without general rollback or a transaction with another service."),
    ("change-data-capture", "cdc-polling-deletes", "comparison", "Why can polling-based CDC miss hard deletes while log-based CDC captures them?", "Misses deleted rows", "Polling cannot query a deleted row; log-based CDC records the DELETE event in the transaction log."),
    ("change-data-capture", "cdc-offset-storage", "factoid", "Where does Debezium with Kafka Connect persist its source-log position?", "connect-offsets", "It persists the source-log position in a dedicated durable Kafka topic called connect-offsets."),
    ("change-data-capture", "cdc-duplicates", "procedure", "How can a CDC consumer reject duplicate events after a connector crash?", "incoming_lsn <= last_processed_lsn", "Use idempotent writes or compare LSNs, discarding an incoming LSN less than or equal to the last processed LSN."),
    ("change-data-capture", "cdc-ordering", "procedure", "How does the CDC design preserve the order of updates to one order?", "partition_key = order_id", "Use the order's primary key as the Kafka partition key so all its updates use one ordered partition."),
    ("knowledge-graph", "graph-canonicalization", "procedure", "What happens to graph edges when duplicate entity nodes are merged?", "All incoming and outgoing edges redirect", "Incoming and outgoing edges redirect to a single canonical master node."),
    ("knowledge-graph", "graph-generic-edge-limit", "factoid", "Why do generic RELATED_TO or MENTIONS edges weaken a knowledge graph?", "loses its reasoning power", "They fail to specify the relationship's nature, weakening reasoning and counterfactual simulations."),
    ("knowledge-graph", "graph-ontology-categories", "comparison", "Which examples distinguish structural and causal edges in the graph ontology?", "Structural/Ownership", "SUBSIDIARY_OF, INVESTED_IN, and EMPLOYED_BY are structural; TRIGGERED, SUPPRESSED, and ACCELERATED are causal."),
    ("knowledge-graph", "graph-source-authority", "factoid", "Why should a tweet and an SEC filing not give graph edges identical authority?", "unverified tweet and an official SEC filing", "Their source authority and verification differ, so extracted edges should not receive identical weight."),
    ("os", "os-thread-memory", "comparison", "Which parts of process memory are shared by threads and which part is private?", "each thread has its own private stack", "Threads share code, heap, and global/static data; each has its own stack."),
    ("os", "os-virtual-addresses", "factoid", "Can different processes use the same virtual address for different physical memory?", "same virtual", "Yes. Each process has a virtual address space mapped separately to physical memory."),
    ("os", "os-file-metadata", "factoid", "What file metadata does the operating systems guide list besides the file contents?", "File metadata can include", "File size, owner, permissions, timestamps, data-block locations, and file type."),
    ("os", "os-context-switch-cost", "comparison", "Why can a context switch cost more than the direct switching time?", "caches, TLBs, and CPU", "Caches, TLBs, and CPU pipelines may need to warm up again, adding indirect cost."),
    ("rabbitmq", "rabbit-topology-order", "procedure", "In what order does the RabbitMQ topology manager redeclare cached objects after reconnecting?", "exchanges first, then queues, then bindings", "It redeclares exchanges first, then queues, then bindings."),
    ("rabbitmq", "rabbit-ack-modes", "comparison", "How do RabbitMQ automatic and manual acknowledgements differ when a consumer crashes?", "Acknowledgement Modes", "Automatic acknowledgement considers delivery complete on sending and can lose messages before processing; manual acknowledgement waits for successful processing."),
    ("rabbitmq", "rabbit-retry-cycle", "procedure", "How do RabbitMQ retry queues delay a dead-lettered message before returning it?", "After the TTL expires", "A negative acknowledgement without requeue routes to the DLX and retry queue; after its TTL expires the message is republished to the primary exchange."),
    ("rabbitmq", "rabbit-parking-lot", "factoid", "What happens after the RabbitMQ retry budget is exhausted?", "manual inspection", "The message moves to a parking lot queue for manual inspection."),
    ("system-design-core", "system-backpressure", "procedure", "What should a design do when producers are faster than consumers?", "Backpressure: limit queues", "Limit queues and concurrent work, deciding whether to delay, drop, or reject work."),
    ("system-design-core", "system-capacity-inputs", "factoid", "Which workload estimates belong in the system design capacity checklist?", "estimate average and peak requests", "Estimate average/peak RPS, object size, storage growth, read/write volume, bandwidth, concurrent users, and retention."),
    ("system-design-core", "system-recovery", "procedure", "What does the checklist ask about temporary state and recovery after an outage?", "Temporary state and recovery", "Decide what can be lost from memory and how node, database, or cache outages rebuild state or fall back to durable storage."),
    ("system-design-core", "system-cost-limit", "factoid", "Which cost limits should be identified before selecting a design?", "which cost limit matters most", "Identify whether compute, memory, storage, bandwidth, operations, or third-party usage is the most important limit."),
    ("uber-architecture", "ride-s2-neighbors", "procedure", "How does the conceptual ride matching design narrow the geospatial search for a rider?", "queries that cell and its eight neighbors", "Map the rider to an S2 Cell ID, then query that cell and its eight neighbors."),
    ("uber-architecture", "ride-eta-vs-distance", "comparison", "Does the conceptual ride design match by straight-line distance or ETA?", "not straight-line distance", "It selects a driver by ETA based on real drive times, rather than straight-line distance."),
    ("uber-architecture", "ride-ap-vs-cp", "comparison", "Why does the conceptual ride design choose AP for locations and CP for matching?", "How the AP vs. CP Trade-Off", "Locations tolerate stale or dropped updates for availability; matching requires consistency to prevent double bookings, failing during a partition."),
    ("uber-architecture", "ride-doma-layers", "factoid", "Which five DOMA service layers are listed in the ride architecture document?", "DOMA organizes", "Edge, Presentation, Product, Business, and Infrastructure."),
    ("youtube-architecture", "video-transcode-dag", "procedure", "What does the conceptual video ingestion DAG process in parallel besides video encoding?", "Other DAG nodes", "Other nodes create thumbnails, extract audio, generate captions, and scan audio against Content ID."),
    ("youtube-architecture", "video-abr-manifests", "factoid", "Which manifest formats does the conceptual video design use for adaptive bitrate?", "MPEG-DASH and HLS manifests", "MPEG-DASH .mpd and HLS .m3u8 manifests index encoded chunks."),
    ("youtube-architecture", "video-live-vs-vod", "comparison", "Why does live streaming use a continuous pipeline instead of the conceptual VOD DAG?", "frames must be processed as they arrive", "VOD can take minutes to optimize compression and quality; live favors 1–3 second delay and must process frames as they arrive."),
    ("youtube-architecture", "video-storage-tiers", "factoid", "Where does the conceptual video CDN keep hot, warm, and cold content?", "Hot content", "Hot content uses nearby NVMe/SSD edge servers, warm content uses regional caches, and cold content comes from central blob storage."),
]

UNSUPPORTED = [
    ("api-private-key", "What exact production TLS private key do our internal API services use?", "api-design-guidelines"),
    ("business-real-revenue", "What was StoreHub's exact audited revenue in September 2026?", "business"),
    ("cache-production-address", "What is the IP address of our production Redis primary?", "cache"),
    ("cdc-live-offset", "What is the current committed LSN of our running production CDC connector?", "change-data-capture"),
    ("os-machine-pid", "What is the PID of the process currently using the most memory on this machine?", "os"),
    ("rabbit-queue-depth", "How many messages are waiting in our production RabbitMQ queue right now?", "rabbitmq"),
    ("ride-current-official-scale", "What exact current driver count does Uber officially report for today?", "uber-architecture"),
    ("video-current-official-codec", "What exact percentage of today's YouTube uploads uses AV1 in the actual production system?", "youtube-architecture"),
]


def encoded(value: object) -> bytes:
    return (json.dumps(value, indent=2, ensure_ascii=False) + "\n").encode()


def build(root: Path = ROOT) -> tuple[dict, dict]:
    original = json.loads((root / "test/evaluation/golden.json").read_text())
    math = {query["id"]: query for query in original["queries"]}
    queries = [copy.deepcopy(math[query_id]) for query_id in MATH_IDS]
    for file, case_id, kind, question, evidence, answer in CASES:
        if evidence.casefold() not in (root / f"samples/{file}.md").read_text().casefold():
            raise ValueError(f"{case_id}: evidence no longer matches {file}")
        queries.append({"id": case_id, "query": question, "type": kind,
                        "faithfulness_label": "fully_supported", "expected_answer": answer,
                        "required_claims": [answer], "relevant": [{"file": f"{file}.md", "contains": evidence}]})
    for case_id, question, file in UNSUPPORTED:
        queries.append({"id": f"unsupported-{case_id}", "query": question, "type": "factoid",
                        "faithfulness_label": "unsupported",
                        "expected_answer": "The provided context does not contain this information.",
                        "required_claims": ["State that the provided context cannot answer the question without inventing an answer."],
                        "relevant": [], "distractors": [{"file": f"{file}.md"}], "tags": ["abstention", "no_evidence"]})
    for query in queries:
        query["tags"] = sorted(set(query.get("tags", [])) | {"sample_corpus", "synthetic"})
        query["judgments"] = [{"annotator_id": annotator, "relevant": copy.deepcopy(query["relevant"]),
                                "distractors": copy.deepcopy(query.get("distractors", []))}
                               for annotator in ("synthetic-pass-a", "synthetic-pass-b")]
        query["adjudication"] = {"method": "deterministic copies of synthetic reference labels; not independent human annotation",
                                  "reviewer_ids": ["synthetic-pass-a", "synthetic-pass-b"]}
    files = sorted(path.relative_to(root).as_posix() for path in (root / "samples").glob("*.md"))
    if len(files) != 14:
        raise ValueError(f"expected 14 committed sample documents, got {len(files)}; review coverage before updating")
    digest = hashlib.sha256()
    entries = []
    for relative in files:
        content = (root / relative).read_bytes()
        digest.update(relative.encode() + b"\0" + content + b"\0")
        entries.append({"path": relative, "sha256": hashlib.sha256(content).hexdigest(), "bytes": len(content)})
    manifest = {"files": entries, "content_sha256": digest.hexdigest(),
                "scope": "configured local source bytes; indexed-version equality is not verified"}
    manifest_path = "test/evaluation/representative-corpus.json"
    fixture = {"schema_version": 3, "metadata": {
        "dataset": "synthetic-all-sample-domains-v1",
        "provenance": "64 deterministic engineering regression queries covering all 14 committed sample documents; not representative production traffic. Architecture references describe the sample's conceptual designs, not official current systems.",
        "consent": "not-applicable-no-production-user-data",
        "judgment": "synthetic labels copied to two recorded passes; not independent human judgments",
        "release_gate": False,
        "corpus": {"id": "nadir-all-samples-v1", "documents": files, "document_count": len(files),
                   "representative": True, "manifest_path": manifest_path, "manifest_sha256": hashlib.sha256(encoded(manifest)).hexdigest()},
        "annotators": [{"id": annotator, "role": "synthetic regression labels", "human": False, "independent": False}
                       for annotator in ("synthetic-pass-a", "synthetic-pass-b")],
    }, "queries": queries}
    return fixture, manifest


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true", help="verify generated files without rewriting")
    args = parser.parse_args()
    fixture, manifest = build()
    for name, content in (("representative.json", fixture), ("representative-corpus.json", manifest)):
        path = ROOT / "test/evaluation" / name
        if args.check:
            if not path.exists() or path.read_bytes() != encoded(content):
                raise SystemExit(f"{path} is stale; regenerate and review changes")
        else:
            path.write_bytes(encoded(content))
    print(f"{'verified' if args.check else 'wrote'} {len(fixture['queries'])} queries across 14 documents, including {len(UNSUPPORTED)} abstention cases")


if __name__ == "__main__":
    main()
