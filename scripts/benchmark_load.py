#!/usr/bin/env python3
"""Measure bounded live API workloads with p50/p95/p99 and saturation evidence.

The benchmark intentionally uses only Python's standard library. It exercises
the same HTTP seams a browser or upload client uses:

  python scripts/benchmark_load.py --mode all --requests 30 --concurrency 8 \
      --json-out test/evaluation/reports/load-$(date +%Y%m%d).json

The API must be running. The report includes request percentiles, failures,
throughput, and the delta from /debug/metrics. It does not claim distributed
capacity: the admission gauges and counters describe one API process.
"""

from __future__ import annotations

import argparse
import concurrent.futures
import datetime as dt
import json
import math
import mimetypes
import time
import urllib.error
import urllib.request
import uuid
from dataclasses import dataclass, asdict
from typing import Any, Callable


@dataclass
class Sample:
    index: int
    status: int | None
    success: bool
    latency_ms: float
    error: str | None


def percentile(values: list[float], fraction: float) -> float | None:
    """Return a deterministic nearest-rank percentile."""

    if not values:
        return None
    ordered = sorted(values)
    rank = max(1, math.ceil(fraction * len(ordered)))
    return round(ordered[rank - 1], 3)


def request_json(url: str, payload: dict[str, Any], timeout: float) -> tuple[int, bytes]:
    body = json.dumps(payload).encode("utf-8")
    request = urllib.request.Request(
        url,
        data=body,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(request, timeout=timeout) as response:
        return response.status, response.read()


def read_stream(url: str, timeout: float) -> int:
    request = urllib.request.Request(url, headers={"Accept": "text/event-stream"})
    with urllib.request.urlopen(request, timeout=timeout) as response:
        data = response.read()
    return response.status


def multipart_body(field: str, filename: str, content: bytes) -> tuple[str, bytes]:
    boundary = "nadir-load-" + uuid.uuid4().hex
    header = (
        f"--{boundary}\r\n"
        f'Content-Disposition: form-data; name="{field}"; filename="{filename}"\r\n'
        f"Content-Type: {mimetypes.guess_type(filename)[0] or 'text/markdown'}\r\n\r\n"
    ).encode("utf-8")
    body = header + content + f"\r\n--{boundary}--\r\n".encode("utf-8")
    return f"multipart/form-data; boundary={boundary}", body


def request_upload(url: str, index: int, content: bytes, timeout: float) -> tuple[int, bytes]:
    content_type, body = multipart_body("files", f"load-benchmark-{index}.md", content)
    request = urllib.request.Request(
        url,
        data=body,
        headers={"Content-Type": content_type},
        method="POST",
    )
    with urllib.request.urlopen(request, timeout=timeout) as response:
        return response.status, response.read()


def fetch_metrics(base_url: str, timeout: float) -> dict[str, Any] | None:
    request = urllib.request.Request(base_url.rstrip("/") + "/debug/metrics")
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            return json.loads(response.read().decode("utf-8"))
    except (OSError, ValueError, urllib.error.HTTPError):
        return None


def metric_map(snapshot: dict[str, Any] | None) -> dict[tuple[str, str], dict[str, Any]]:
    if not snapshot:
        return {}
    return {
        (item["operation"], item["outcome"]): item
        for item in snapshot.get("operations", [])
        if "operation" in item and "outcome" in item
    }


def metric_delta(before: dict[str, Any] | None, after: dict[str, Any] | None) -> dict[str, Any]:
    before_map = metric_map(before)
    after_map = metric_map(after)
    keys = sorted(set(before_map) | set(after_map))
    operations = []
    for key in keys:
        old = before_map.get(key, {})
        new = after_map.get(key, {})
        operations.append(
            {
                "operation": key[0],
                "outcome": key[1],
                "count": new.get("count", 0) - old.get("count", 0),
                "duration_ms_sum": round(new.get("duration_ms_sum", 0) - old.get("duration_ms_sum", 0), 3),
            }
        )
    gauges = (after or {}).get("gauges", {})
    saturation = {
        name: value
        for name, value in gauges.items()
        if name.startswith("admission.")
    }
    return {"operations": operations, "admission_gauges": saturation}


def summarize(samples: list[Sample], started: float, metrics: dict[str, Any]) -> dict[str, Any]:
    latencies = [sample.latency_ms for sample in samples]
    successes = sum(sample.success for sample in samples)
    elapsed = max(0.001, time.monotonic() - started)
    return {
        "requests": len(samples),
        "successes": successes,
        "failures": len(samples) - successes,
        "latency_ms": {
            "p50": percentile(latencies, 0.50),
            "p95": percentile(latencies, 0.95),
            "p99": percentile(latencies, 0.99),
            "max": max(latencies) if latencies else None,
        },
        "throughput_rps": round(successes / elapsed, 3),
        "metrics": metrics,
        "samples": [asdict(sample) for sample in samples],
    }


def run_workload(
    name: str,
    requests: int,
    concurrency: int,
    timeout: float,
    operation: Callable[[int], tuple[int, bytes]],
    before: dict[str, Any] | None,
    base_url: str,
) -> dict[str, Any]:
    started = time.monotonic()

    def one(index: int) -> Sample:
        request_started = time.monotonic()
        try:
            status, _ = operation(index)
            return Sample(index, status, 200 <= status < 300, (time.monotonic() - request_started) * 1000, None)
        except (OSError, ValueError, urllib.error.HTTPError) as exc:
            status = getattr(exc, "code", None)
            return Sample(index, status, False, (time.monotonic() - request_started) * 1000, str(exc))

    with concurrent.futures.ThreadPoolExecutor(max_workers=concurrency) as executor:
        samples = list(executor.map(one, range(requests)))
    after = fetch_metrics(base_url, timeout)
    return {
        "name": name,
        "concurrency": concurrency,
        **summarize(samples, started, metric_delta(before, after)),
    }


def workload_operations(base_url: str, timeout: float, large_bytes: int) -> dict[str, Callable[[int], tuple[int, bytes]]]:
    turns_url = base_url.rstrip("/") + "/api/v1/turns"
    documents_url = base_url.rstrip("/") + "/api/v1/documents"
    long_query = "; ".join(
        [
            "explain the definition and formula",
            "give the assumptions and edge cases",
            "compare the computational steps",
            "show a worked example",
            "state the result precisely",
        ]
    )
    document = ("# Load benchmark document\n\n" + ("This is a representative long indexing paragraph. " * 4000)).encode("utf-8")
    if len(document) < large_bytes:
        remaining = large_bytes - len(document)
        document += b"\n" + b"detail " * math.ceil(remaining / len(b"detail "))

    def chat(index: int) -> tuple[int, bytes]:
        status, body = request_json(turns_url, {"query": f"load chat {index}: {long_query}", "generate": True, "top_k": 5}, timeout)
        response = json.loads(body.decode("utf-8"))
        turn_id = response.get("turn_id")
        if turn_id:
            read_stream(base_url.rstrip("/") + "/api/v1/turns/" + turn_id + "/events", timeout)
        return status, body

    def retrieval(index: int) -> tuple[int, bytes]:
        return request_json(turns_url, {"query": f"load retrieval {index}: {long_query}", "generate": False, "top_k": 5}, timeout)

    def ingest(index: int) -> tuple[int, bytes]:
        return request_upload(documents_url, index, document + f"\ncase={index}\n".encode("utf-8"), timeout)

    return {"chat_streams": chat, "long_retrieval": retrieval, "large_ingestion": ingest}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base-url", default="http://127.0.0.1:8100")
    parser.add_argument("--mode", choices=["chat", "retrieval", "ingest", "all"], default="all")
    parser.add_argument("--requests", type=int, default=20)
    parser.add_argument("--concurrency", type=int, default=4)
    parser.add_argument("--timeout", type=float, default=120.0)
    parser.add_argument("--large-bytes", type=int, default=256 * 1024)
    parser.add_argument("--json-out")
    args = parser.parse_args()
    if args.requests <= 0 or args.concurrency <= 0:
        parser.error("--requests and --concurrency must be greater than zero")

    health_request = urllib.request.Request(args.base_url.rstrip("/") + "/api/v1/health")
    try:
        with urllib.request.urlopen(health_request, timeout=args.timeout) as response:
            if response.status != 200:
                raise RuntimeError(f"health returned {response.status}")
    except OSError as exc:
        parser.error(f"API health check failed: {exc}")

    operations = workload_operations(args.base_url, args.timeout, args.large_bytes)
    selected = {"chat": "chat_streams", "retrieval": "long_retrieval", "ingest": "large_ingestion"}
    names = list(operations) if args.mode == "all" else [selected[args.mode]]
    report: dict[str, Any] = {
        "schema_version": 1,
        "measured_at": dt.datetime.now(dt.timezone.utc).isoformat(),
        "base_url": args.base_url,
        "process_scope": "single API process; admission is not distributed",
        "workloads": [],
    }
    for name in names:
        before = fetch_metrics(args.base_url, args.timeout)
        report["workloads"].append(
            run_workload(name, args.requests, args.concurrency, args.timeout, operations[name], before, args.base_url)
        )
    encoded = json.dumps(report, indent=2, sort_keys=True) + "\n"
    if args.json_out:
        with open(args.json_out, "w", encoding="utf-8") as output:
            output.write(encoded)
    print(encoded, end="")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
