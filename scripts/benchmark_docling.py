#!/usr/bin/env python3
"""Measure the Docling HTTP sidecar with a representative PDF corpus.

The benchmark deliberately uses only the Python standard library so it can be
run from the host without installing another measurement stack. It records
latency and failure behavior for every document/run and optionally samples
the converter's resident memory through a local PID or Docker stats.

Example:
    python scripts/benchmark_docling.py \
        --input-dir ./pdfs/benchmark \
        --endpoint http://127.0.0.1:5003/convert \
        --pid 12345 \
        --json-out test/evaluation/reports/docling.json

For a Compose container, pass the ID returned by:
    docker compose -f deploy/compose/docker-compose.yml ps -q docling
"""

from __future__ import annotations

import argparse
import datetime as dt
import json
import math
import re
import subprocess
import sys
import threading
import time
import urllib.error
import urllib.request
from dataclasses import asdict, dataclass
from pathlib import Path
from typing import Callable


DEFAULT_ENDPOINT = "http://127.0.0.1:5003/convert"
DEFAULT_HEALTH_URL = "http://127.0.0.1:5003/health"
DEFAULT_TIMEOUT_SECONDS = 120.0
DEFAULT_SAMPLE_INTERVAL_SECONDS = 0.25
DEFAULT_MAX_RESPONSE_BYTES = 64 * 1024 * 1024
MEMORY_RE = re.compile(r"^\s*([0-9]+(?:\.[0-9]+)?)\s*([A-Za-z]+)\s*$")
MEMORY_UNITS = {
    "B": 1,
    "KB": 1000,
    "MB": 1000**2,
    "GB": 1000**3,
    "TB": 1000**4,
    "KIB": 1024,
    "MIB": 1024**2,
    "GIB": 1024**3,
    "TIB": 1024**4,
}


@dataclass
class Sample:
    document: str
    run: int
    source_bytes: int
    status: int | None
    success: bool
    timed_out: bool
    latency_ms: float
    response_bytes: int
    error: str | None
    rss_before_bytes: int | None
    rss_after_bytes: int | None
    rss_peak_bytes: int | None


def percentile(values: list[float], fraction: float) -> float | None:
    """Return a deterministic nearest-rank percentile for a non-empty list."""

    if not values:
        return None
    if not 0 < fraction <= 1:
        raise ValueError("percentile fraction must be greater than 0 and at most 1")
    ordered = sorted(values)
    index = min(len(ordered) - 1, max(0, math.ceil(fraction * len(ordered)) - 1))
    return ordered[index]


def parse_memory_bytes(value: str) -> int | None:
    """Parse a Docker/proc-style memory quantity into bytes."""

    match = MEMORY_RE.match(value)
    if not match:
        return None
    unit = match.group(2).upper()
    multiplier = MEMORY_UNITS.get(unit)
    if multiplier is None:
        return None
    return int(float(match.group(1)) * multiplier)


def proc_rss_reader(pid: int) -> Callable[[], int | None]:
    """Return a reader for VmRSS on Linux; missing processes yield None."""

    status_path = Path("/proc") / str(pid) / "status"

    def read() -> int | None:
        try:
            for line in status_path.read_text(encoding="utf-8").splitlines():
                if line.startswith("VmRSS:"):
                    parts = line.split()
                    if len(parts) >= 2:
                        return int(parts[1]) * 1024
        except (FileNotFoundError, PermissionError, ValueError):
            return None
        return None

    return read


def docker_memory_reader(container: str) -> Callable[[], int | None]:
    """Return a reader backed by ``docker stats --no-stream``."""

    def read() -> int | None:
        try:
            result = subprocess.run(
                ["docker", "stats", "--no-stream", "--format", "{{.MemUsage}}", container],
                capture_output=True,
                text=True,
                check=False,
                timeout=5,
            )
        except (FileNotFoundError, subprocess.TimeoutExpired):
            return None
        if result.returncode != 0:
            return None
        usage = result.stdout.strip().splitlines()
        if not usage:
            return None
        # Docker reports "used / limit". Only the used quantity is relevant.
        return parse_memory_bytes(usage[0].split("/", 1)[0].strip())

    return read


class MemorySampler:
    def __init__(self, reader: Callable[[], int | None] | None, interval_seconds: float):
        self._reader = reader
        self._interval_seconds = interval_seconds
        self.before: int | None = None
        self.after: int | None = None
        self.peak: int | None = None
        self._stop = threading.Event()
        self._thread: threading.Thread | None = None

    def __enter__(self) -> "MemorySampler":
        if self._reader is None:
            return self
        self.before = self._reader()
        self.peak = self.before
        self._thread = threading.Thread(target=self._sample, daemon=True)
        self._thread.start()
        return self

    def __exit__(self, exc_type, exc_value, traceback) -> None:
        if self._reader is None:
            return
        self._stop.set()
        if self._thread is not None:
            self._thread.join(timeout=max(1.0, self._interval_seconds * 4))
        self.after = self._reader()
        self._observe(self.after)

    def _sample(self) -> None:
        while not self._stop.wait(self._interval_seconds):
            self._observe(self._reader())

    def _observe(self, value: int | None) -> None:
        if value is not None and (self.peak is None or value > self.peak):
            self.peak = value


def is_timeout_error(error: BaseException) -> bool:
    if isinstance(error, TimeoutError):
        return True
    if isinstance(error, urllib.error.URLError):
        return isinstance(error.reason, TimeoutError) or "timed out" in str(error.reason).lower()
    return "timed out" in str(error).lower()


def read_response(response: object, max_bytes: int) -> int:
    body = bytearray()
    while True:
        chunk = response.read(min(1024 * 1024, max_bytes - len(body) + 1))
        if not chunk:
            break
        body.extend(chunk)
        if len(body) > max_bytes:
            raise ValueError(f"response exceeds max response size of {max_bytes} bytes")
    return len(body)


def convert(
    endpoint: str,
    name: str,
    data: bytes,
    timeout_seconds: float,
    max_response_bytes: int,
    sampler: MemorySampler,
    document: str,
    run: int,
) -> Sample:
    request = urllib.request.Request(
        endpoint,
        data=data,
        headers={
            "Content-Type": "application/pdf",
            "X-Nadir-Filename": name,
        },
        method="POST",
    )
    started = time.perf_counter()
    status: int | None = None
    response_bytes = 0
    success = False
    error: str | None = None
    timed_out = False

    try:
        with urllib.request.urlopen(request, timeout=timeout_seconds) as response:
            status = response.status
            response_bytes = read_response(response, max_response_bytes)
            success = 200 <= status < 300 and response_bytes > 0
            if not success:
                error = f"HTTP {status} with an empty or non-success response"
    except urllib.error.HTTPError as exc:
        status = exc.code
        try:
            error_body = exc.read(1024).decode("utf-8", errors="replace").strip()
        except OSError:
            error_body = ""
        error = f"HTTP {exc.code}: {error_body}".strip()
    except (OSError, ValueError, urllib.error.URLError) as exc:
        timed_out = is_timeout_error(exc)
        error = str(exc)

    latency_ms = (time.perf_counter() - started) * 1000
    return Sample(
        document=document,
        run=run,
        source_bytes=len(data),
        status=status,
        success=success,
        timed_out=timed_out,
        latency_ms=round(latency_ms, 3),
        response_bytes=response_bytes,
        error=error,
        rss_before_bytes=sampler.before,
        rss_after_bytes=sampler.after,
        rss_peak_bytes=sampler.peak,
    )


def health_check(url: str, timeout_seconds: float) -> tuple[int, str]:
    request = urllib.request.Request(url, method="GET")
    with urllib.request.urlopen(request, timeout=timeout_seconds) as response:
        return response.status, response.read(4096).decode("utf-8", errors="replace")


def find_documents(input_dir: Path, max_documents: int | None) -> list[Path]:
    documents = sorted(
        path for path in input_dir.iterdir() if path.is_file() and path.suffix.lower() == ".pdf"
    )
    if max_documents is not None:
        documents = documents[:max_documents]
    return documents


def build_report(
    args: argparse.Namespace,
    documents: list[Path],
    samples: list[Sample],
    memory_source: str,
) -> dict[str, object]:
    successful = [sample for sample in samples if sample.success]
    latencies = [sample.latency_ms for sample in samples]
    peaks = [sample.rss_peak_bytes for sample in samples if sample.rss_peak_bytes is not None]
    return {
        "schema_version": 1,
        "generated_at": dt.datetime.now(dt.timezone.utc).isoformat(),
        "target": {
            "endpoint": args.endpoint,
            "health_url": args.health_url,
            "timeout_seconds": args.timeout,
            "runs": args.runs,
            "warmup_runs": args.warmup_runs,
            "input_dir": str(args.input_dir),
            "documents": [str(path) for path in documents],
        },
        "memory": {
            "source": memory_source,
            "sample_interval_seconds": args.sample_interval,
            "peak_rss_bytes": max(peaks) if peaks else None,
        },
        "summary": {
            "attempts": len(samples),
            "successes": len(successful),
            "failures": len(samples) - len(successful),
            "timeouts": sum(sample.timed_out for sample in samples),
            "success_rate": round(len(successful) / len(samples), 4) if samples else None,
            "latency_ms": {
                "min": min(latencies) if latencies else None,
                "p50": percentile(latencies, 0.50),
                "p95": percentile(latencies, 0.95),
                "max": max(latencies) if latencies else None,
            },
            "response_bytes": {
                "min": min((sample.response_bytes for sample in samples), default=None),
                "max": max((sample.response_bytes for sample in samples), default=None),
            },
        },
        "samples": [asdict(sample) for sample in samples],
    }


def parser() -> argparse.ArgumentParser:
    result = argparse.ArgumentParser(description=__doc__)
    result.add_argument("--input-dir", type=Path, required=True, help="directory containing real PDF documents")
    result.add_argument("--endpoint", default=DEFAULT_ENDPOINT, help=f"Docling conversion URL (default: {DEFAULT_ENDPOINT})")
    result.add_argument("--health-url", default=DEFAULT_HEALTH_URL, help=f"health URL (default: {DEFAULT_HEALTH_URL})")
    result.add_argument("--runs", type=int, default=3, help="measured runs per document (default: 3)")
    result.add_argument("--warmup-runs", type=int, default=1, help="warmup runs for the first document (default: 1)")
    result.add_argument("--timeout", type=float, default=DEFAULT_TIMEOUT_SECONDS, help="per-request timeout in seconds")
    result.add_argument("--max-documents", type=int, help="measure at most this many PDFs")
    result.add_argument("--max-response-bytes", type=int, default=DEFAULT_MAX_RESPONSE_BYTES, help="fail responses larger than this bound")
    memory = result.add_mutually_exclusive_group()
    memory.add_argument("--pid", type=int, help="local Docling process ID; reads /proc/<pid>/status")
    memory.add_argument("--container", help="Docker container ID or name; samples docker stats")
    result.add_argument("--sample-interval", type=float, default=DEFAULT_SAMPLE_INTERVAL_SECONDS, help="memory sampling interval in seconds")
    result.add_argument("--json-out", type=Path, help="write the complete JSON report to this path")
    return result


def validate_args(args: argparse.Namespace) -> None:
    if not args.input_dir.is_dir():
        raise ValueError(f"input directory does not exist or is not a directory: {args.input_dir}")
    if args.runs < 1:
        raise ValueError("--runs must be at least 1")
    if args.warmup_runs < 0:
        raise ValueError("--warmup-runs must not be negative")
    if args.timeout <= 0:
        raise ValueError("--timeout must be greater than 0")
    if args.max_documents is not None and args.max_documents < 1:
        raise ValueError("--max-documents must be at least 1")
    if args.max_response_bytes < 1:
        raise ValueError("--max-response-bytes must be at least 1")
    if args.sample_interval <= 0:
        raise ValueError("--sample-interval must be greater than 0")


def main() -> int:
    args = parser().parse_args()
    try:
        validate_args(args)
        documents = find_documents(args.input_dir, args.max_documents)
        if not documents:
            raise ValueError(f"no PDF documents found in {args.input_dir}")
        health_status, _ = health_check(args.health_url, min(args.timeout, 10.0))
        if health_status < 200 or health_status >= 300:
            raise ValueError(f"Docling health check returned HTTP {health_status}")
    except (OSError, ValueError, urllib.error.URLError) as exc:
        print(f"benchmark_docling: {exc}", file=sys.stderr)
        return 2

    reader: Callable[[], int | None] | None = None
    memory_source = "unavailable"
    if args.pid is not None:
        reader = proc_rss_reader(args.pid)
        memory_source = f"proc:{args.pid}"
    elif args.container:
        reader = docker_memory_reader(args.container)
        memory_source = f"docker:{args.container}"

    first = documents[0]
    for warmup in range(args.warmup_runs):
        data = first.read_bytes()
        with MemorySampler(reader, args.sample_interval) as sampler:
            sample = convert(
                args.endpoint,
                first.name,
                data,
                args.timeout,
                args.max_response_bytes,
                sampler,
                str(first.relative_to(args.input_dir)),
                0,
            )
        if not sample.success:
            print(f"warmup {warmup + 1} failed for {first.name}: {sample.error}", file=sys.stderr)

    samples: list[Sample] = []
    for document in documents:
        data = document.read_bytes()
        relative_name = str(document.relative_to(args.input_dir))
        for run in range(1, args.runs + 1):
            with MemorySampler(reader, args.sample_interval) as sampler:
                sample = convert(
                    args.endpoint,
                    document.name,
                    data,
                    args.timeout,
                    args.max_response_bytes,
                    sampler,
                    relative_name,
                    run,
                )
            samples.append(sample)
            outcome = "ok" if sample.success else "failed"
            print(
                f"{outcome:6} {relative_name} run={run} latency={sample.latency_ms:.1f}ms"
                f" status={sample.status or '-'} response={sample.response_bytes}B"
                f" timeout={sample.timed_out}",
                file=sys.stderr if not sample.success else sys.stdout,
            )

    report = build_report(args, documents, samples, memory_source)
    print(json.dumps(report["summary"], indent=2, sort_keys=True))
    if args.json_out:
        args.json_out.parent.mkdir(parents=True, exist_ok=True)
        args.json_out.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
        print(f"report: {args.json_out}")
    return 0 if all(sample.success for sample in samples) else 1


if __name__ == "__main__":
    raise SystemExit(main())
