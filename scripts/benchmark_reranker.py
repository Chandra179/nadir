#!/usr/bin/env python3
"""Benchmark a running Nadir reranker sidecar.

The input dataset contains query/candidate passages and expert relevance
grades. The benchmark measures the sidecar directly, so it can compare model
and backend profiles without changing the API or the Retrieval evaluator.
Quality is calculated from the returned scores; latency and resource samples
are recorded for every measured request.

Dataset format:

    {
      "schema_version": 1,
      "metadata": {"provenance": "consent-safe evaluation corpus"},
      "queries": [
        {
          "id": "q-001",
          "query": "what is the secant formula?",
          "candidates": [
            {"text": "The secant formula is ...", "relevance": 2},
            {"text": "An unrelated passage ...", "relevance": 0}
          ]
        }
      ]
    }

Relevance is an integer grade where zero means non-relevant. A candidate list
must contain at least one positive grade. Use the existing full Retrieval
evaluator for end-to-end quality; this tool isolates reranker quality and
resource cost for model/backend A/B comparisons.

Example:
    python scripts/benchmark_reranker.py \
        --dataset ./reranker-benchmark.json \
        --endpoint http://127.0.0.1:5002/rerank \
        --pid 12345 \
        --runs 3 \
        --json-out test/evaluation/reports/reranker-bge-cpu.json

For a Compose container, pass the ID returned by:
    docker compose -f deploy/compose/docker-compose.yml ps -q reranker
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


DEFAULT_ENDPOINT = "http://127.0.0.1:5002/rerank"
DEFAULT_HEALTH_URL = "http://127.0.0.1:5002/health"
DEFAULT_TIMEOUT_SECONDS = 30.0
DEFAULT_SAMPLE_INTERVAL_SECONDS = 0.25
DEFAULT_MAX_RESPONSE_BYTES = 4 * 1024 * 1024
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


@dataclass(frozen=True)
class Candidate:
    text: str
    relevance: int


@dataclass(frozen=True)
class QueryCase:
    id: str
    query: str
    candidates: tuple[Candidate, ...]


@dataclass
class Sample:
    query_id: str
    run: int
    candidate_count: int
    status: int | None
    success: bool
    timed_out: bool
    latency_ms: float
    response_bytes: int
    scores: list[float] | None
    error: str | None
    rss_before_bytes: int | None
    rss_after_bytes: int | None
    rss_peak_bytes: int | None
    gpu_before_bytes: int | None
    gpu_after_bytes: int | None
    gpu_peak_bytes: int | None


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
    multiplier = MEMORY_UNITS.get(match.group(2).upper())
    if multiplier is None:
        return None
    return int(float(match.group(1)) * multiplier)


def proc_rss_reader(pid: int) -> Callable[[], int | None]:
    """Return a Linux VmRSS reader; missing or inaccessible processes yield None."""

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
        return parse_memory_bytes(usage[0].split("/", 1)[0].strip())

    return read


def nvidia_memory_reader(pid: int) -> Callable[[], int | None]:
    """Return a process-specific CUDA memory reader using ``nvidia-smi``."""

    def read() -> int | None:
        try:
            result = subprocess.run(
                [
                    "nvidia-smi",
                    "--query-compute-apps=pid,used_memory",
                    "--format=csv,noheader,nounits",
                ],
                capture_output=True,
                text=True,
                check=False,
                timeout=5,
            )
        except (FileNotFoundError, subprocess.TimeoutExpired):
            return None
        if result.returncode != 0:
            return None
        for line in result.stdout.splitlines():
            fields = [field.strip() for field in line.split(",", 1)]
            if len(fields) != 2:
                continue
            try:
                if int(fields[0]) == pid:
                    return int(float(fields[1]) * 1024 * 1024)
            except ValueError:
                continue
        return None

    return read


class ResourceSampler:
    """Sample optional process RSS and process-specific GPU memory."""

    def __init__(
        self,
        rss_reader: Callable[[], int | None] | None,
        gpu_reader: Callable[[], int | None] | None,
        interval_seconds: float,
    ):
        self._rss_reader = rss_reader
        self._gpu_reader = gpu_reader
        self._interval_seconds = interval_seconds
        self.rss_before: int | None = None
        self.rss_after: int | None = None
        self.rss_peak: int | None = None
        self.gpu_before: int | None = None
        self.gpu_after: int | None = None
        self.gpu_peak: int | None = None
        self._stop = threading.Event()
        self._thread: threading.Thread | None = None

    def __enter__(self) -> "ResourceSampler":
        if self._rss_reader is None and self._gpu_reader is None:
            return self
        self.rss_before = self._read(self._rss_reader)
        self.rss_peak = self.rss_before
        self.gpu_before = self._read(self._gpu_reader)
        self.gpu_peak = self.gpu_before
        self._thread = threading.Thread(target=self._sample, daemon=True)
        self._thread.start()
        return self

    def __exit__(self, exc_type, exc_value, traceback) -> None:
        if self._thread is None:
            return
        self._stop.set()
        self._thread.join(timeout=max(1.0, self._interval_seconds * 4))
        self.rss_after = self._read(self._rss_reader)
        self.gpu_after = self._read(self._gpu_reader)
        self._observe_rss(self.rss_after)
        self._observe_gpu(self.gpu_after)

    def _sample(self) -> None:
        while not self._stop.wait(self._interval_seconds):
            self._observe_rss(self._read(self._rss_reader))
            self._observe_gpu(self._read(self._gpu_reader))

    @staticmethod
    def _read(reader: Callable[[], int | None] | None) -> int | None:
        return reader() if reader is not None else None

    def _observe_rss(self, value: int | None) -> None:
        if value is not None and (self.rss_peak is None or value > self.rss_peak):
            self.rss_peak = value

    def _observe_gpu(self, value: int | None) -> None:
        if value is not None and (self.gpu_peak is None or value > self.gpu_peak):
            self.gpu_peak = value


def is_timeout_error(error: BaseException) -> bool:
    if isinstance(error, TimeoutError):
        return True
    if isinstance(error, urllib.error.URLError):
        return isinstance(error.reason, TimeoutError) or "timed out" in str(error.reason).lower()
    return "timed out" in str(error).lower()


def read_limited(response: object, max_bytes: int) -> bytes:
    body = response.read(max_bytes + 1)
    if len(body) > max_bytes:
        raise ValueError(f"response exceeds max response size of {max_bytes} bytes")
    return body


def load_dataset(path: Path, max_queries: int | None = None) -> tuple[dict[str, object], list[QueryCase]]:
    """Load and validate a reranker dataset."""

    raw = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(raw, dict):
        raise ValueError("dataset root must be an object")
    raw_queries = raw.get("queries")
    if not isinstance(raw_queries, list) or not raw_queries:
        raise ValueError("dataset must contain a non-empty queries array")
    queries: list[QueryCase] = []
    seen_ids: set[str] = set()
    for index, raw_query in enumerate(raw_queries, start=1):
        if not isinstance(raw_query, dict):
            raise ValueError(f"query #{index} must be an object")
        query_id = str(raw_query.get("id", "")).strip()
        query = str(raw_query.get("query", "")).strip()
        if not query_id or not query:
            raise ValueError(f"query #{index} needs a non-empty id and query")
        if query_id in seen_ids:
            raise ValueError(f"dataset query {query_id!r} is duplicated")
        seen_ids.add(query_id)
        raw_candidates = raw_query.get("candidates")
        if not isinstance(raw_candidates, list) or not raw_candidates:
            raise ValueError(f"query {query_id!r} needs a non-empty candidates array")
        candidates: list[Candidate] = []
        for candidate_index, raw_candidate in enumerate(raw_candidates, start=1):
            if not isinstance(raw_candidate, dict):
                raise ValueError(f"query {query_id!r} candidate #{candidate_index} must be an object")
            text = str(raw_candidate.get("text", "")).strip()
            relevance = raw_candidate.get("relevance")
            if not text or isinstance(relevance, bool) or not isinstance(relevance, int) or relevance < 0:
                raise ValueError(
                    f"query {query_id!r} candidate #{candidate_index} needs text and a non-negative integer relevance"
                )
            candidates.append(Candidate(text=text, relevance=relevance))
        if not any(candidate.relevance > 0 for candidate in candidates):
            raise ValueError(f"query {query_id!r} needs at least one positive relevance grade")
        queries.append(QueryCase(query_id, query, tuple(candidates)))
    if max_queries is not None:
        queries = queries[:max_queries]
    return raw, queries


def health_check(url: str, timeout_seconds: float) -> dict[str, object]:
    request = urllib.request.Request(url, method="GET")
    try:
        with urllib.request.urlopen(request, timeout=timeout_seconds) as response:
            status = response.status
            body = response.read(16 * 1024).decode("utf-8", errors="replace")
    except urllib.error.HTTPError as exc:
        body = exc.read(4096).decode("utf-8", errors="replace").strip()
        raise ValueError(f"health check returned HTTP {exc.code}: {body}") from exc
    if status < 200 or status >= 300:
        raise ValueError(f"health check returned HTTP {status}: {body}")
    try:
        health = json.loads(body)
    except json.JSONDecodeError as exc:
        raise ValueError("health check returned invalid JSON") from exc
    if not isinstance(health, dict):
        raise ValueError("health check response must be a JSON object")
    if health.get("status") not in (None, "ok"):
        raise ValueError(f"health check is not ready: {health.get('status')}")
    return health


def rerank(
    endpoint: str,
    query: QueryCase,
    timeout_seconds: float,
    max_response_bytes: int,
    sampler: ResourceSampler,
    run: int,
) -> Sample:
    payload = json.dumps(
        {"query": query.query, "passages": [candidate.text for candidate in query.candidates]},
        ensure_ascii=False,
    ).encode("utf-8")
    request = urllib.request.Request(
        endpoint,
        data=payload,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    started = time.perf_counter()
    status: int | None = None
    response_bytes = 0
    scores: list[float] | None = None
    success = False
    error: str | None = None
    timed_out = False
    try:
        with urllib.request.urlopen(request, timeout=timeout_seconds) as response:
            status = response.status
            body = read_limited(response, max_response_bytes)
            response_bytes = len(body)
            if status < 200 or status >= 300:
                error = f"HTTP {status}"
            else:
                decoded = json.loads(body)
                raw_scores = decoded.get("scores") if isinstance(decoded, dict) else None
                if not isinstance(raw_scores, list) or len(raw_scores) != len(query.candidates):
                    raise ValueError(
                        f"response scores length does not match passages ({len(query.candidates)})"
                    )
                scores = []
                for score in raw_scores:
                    if isinstance(score, bool) or not isinstance(score, (int, float)) or not math.isfinite(score):
                        raise ValueError("response contains a non-finite or non-numeric score")
                    scores.append(float(score))
                success = True
    except urllib.error.HTTPError as exc:
        status = exc.code
        try:
            response_bytes = len(exc.read(max_response_bytes))
        except OSError:
            response_bytes = 0
        error = f"HTTP {exc.code}"
    except (OSError, ValueError, json.JSONDecodeError, urllib.error.URLError) as exc:
        timed_out = is_timeout_error(exc)
        error = str(exc)
    latency_ms = (time.perf_counter() - started) * 1000
    return Sample(
        query_id=query.id,
        run=run,
        candidate_count=len(query.candidates),
        status=status,
        success=success,
        timed_out=timed_out,
        latency_ms=round(latency_ms, 3),
        response_bytes=response_bytes,
        scores=scores,
        error=error,
        rss_before_bytes=sampler.rss_before,
        rss_after_bytes=sampler.rss_after,
        rss_peak_bytes=sampler.rss_peak,
        gpu_before_bytes=sampler.gpu_before,
        gpu_after_bytes=sampler.gpu_after,
        gpu_peak_bytes=sampler.gpu_peak,
    )


def ranking_metrics(query: QueryCase, scores: list[float], top_k: int) -> dict[str, float | int]:
    """Calculate graded ranking metrics with stable score ties."""

    order = sorted(range(len(scores)), key=lambda index: (-scores[index], index))
    grades = [candidate.relevance for candidate in query.candidates]
    positive = sum(grade > 0 for grade in grades)
    top_order = order[:top_k]
    found = sum(grades[index] > 0 for index in top_order)
    first_rank = 0
    for position, index in enumerate(order, start=1):
        if grades[index] > 0:
            first_rank = position
            break

    dcg = sum(
        (2**grades[index] - 1) / math.log2(position + 2)
        for position, index in enumerate(top_order)
    )
    ideal = sorted(grades, reverse=True)[:top_k]
    idcg = sum((2**grade - 1) / math.log2(position + 2) for position, grade in enumerate(ideal))
    ndcg = dcg / idcg if idcg else 0.0
    return {
        "candidate_count": len(grades),
        "relevant_count": positive,
        "relevant_found_at_k": found,
        "first_relevant_rank": first_rank,
        "hit_rate_at_k": 1 if found else 0,
        "recall_at_k": found / positive if positive else 0.0,
        "mrr_at_10": 1 / first_rank if 0 < first_rank <= 10 else 0.0,
        "ndcg_at_k": ndcg,
    }


def mean(values: list[float]) -> float | None:
    return sum(values) / len(values) if values else None


def build_report(
    args: argparse.Namespace,
    dataset_metadata: dict[str, object],
    queries: list[QueryCase],
    samples: list[Sample],
    health: dict[str, object],
    health_latency_ms: float,
    resource_sources: dict[str, str],
) -> dict[str, object]:
    final: dict[str, Sample] = {}
    for sample in samples:
        final[sample.query_id] = sample
    query_results: list[dict[str, object]] = []
    for query in queries:
        sample = final.get(query.id)
        result: dict[str, object] = {
            "id": query.id,
            "query": query.query,
            "candidate_count": len(query.candidates),
            "success": bool(sample and sample.success),
            "latency_ms": sample.latency_ms if sample else None,
        }
        if sample and sample.success and sample.scores is not None:
            result.update(ranking_metrics(query, sample.scores, args.top_k))
        else:
            result.update(
                {
                    "relevant_count": sum(candidate.relevance > 0 for candidate in query.candidates),
                    "relevant_found_at_k": None,
                    "first_relevant_rank": None,
                    "hit_rate_at_k": None,
                    "recall_at_k": None,
                    "mrr_at_10": None,
                    "ndcg_at_k": None,
                }
            )
        query_results.append(result)

    evaluated = [result for result in query_results if result["success"]]
    successful_samples = [sample for sample in samples if sample.success]
    successful_latencies = [sample.latency_ms for sample in successful_samples]
    elapsed_seconds = sum(successful_latencies) / 1000
    attempted_passages = sum(sample.candidate_count for sample in samples)
    successful_passages = sum(sample.candidate_count for sample in successful_samples)
    peaks_rss = [sample.rss_peak_bytes for sample in samples if sample.rss_peak_bytes is not None]
    peaks_gpu = [sample.gpu_peak_bytes for sample in samples if sample.gpu_peak_bytes is not None]

    quality_keys = ("hit_rate_at_k", "recall_at_k", "mrr_at_10", "ndcg_at_k")
    quality = {
        key: mean([float(result[key]) for result in evaluated if result[key] is not None])
        for key in quality_keys
    }
    return {
        "schema_version": 1,
        "generated_at": dt.datetime.now(dt.timezone.utc).isoformat(),
        "target": {
            "endpoint": args.endpoint,
            "health_url": args.health_url,
            "health": health,
            "health_probe_latency_ms": round(health_latency_ms, 3),
            "timeout_seconds": args.timeout,
            "runs": args.runs,
            "warmup_runs": args.warmup_runs,
            "top_k": args.top_k,
            "dataset": str(args.dataset),
            "dataset_metadata": dataset_metadata,
        },
        "resources": {
            "rss_source": resource_sources["rss"],
            "gpu_source": resource_sources["gpu"],
            "sample_interval_seconds": args.sample_interval,
            "peak_rss_bytes": max(peaks_rss) if peaks_rss else None,
            "peak_gpu_bytes": max(peaks_gpu) if peaks_gpu else None,
        },
        "summary": {
            "queries": len(queries),
            "evaluated_queries": len(evaluated),
            "attempts": len(samples),
            "successes": len(successful_samples),
            "failures": len(samples) - len(successful_samples),
            "timeouts": sum(sample.timed_out for sample in samples),
            "success_rate": round(len(successful_samples) / len(samples), 4) if samples else None,
            "latency_ms": {
                "min": min(successful_latencies) if successful_latencies else None,
                "p50": percentile(successful_latencies, 0.50),
                "p95": percentile(successful_latencies, 0.95),
                "max": max(successful_latencies) if successful_latencies else None,
            },
            "throughput": {
                "successful_queries_per_second": round(len(successful_samples) / elapsed_seconds, 4)
                if elapsed_seconds
                else None,
                "successful_passages_per_second": round(successful_passages / elapsed_seconds, 4)
                if elapsed_seconds
                else None,
                "attempted_passages": attempted_passages,
            },
            "quality": quality,
        },
        "queries": query_results,
        "samples": [asdict(sample) for sample in samples],
        "notes": [
            "Quality uses the final measured run for each query and only positive relevance grades as relevant.",
            "Latency and throughput are sequential request measurements; use the load benchmark for concurrency saturation.",
            "RSS and GPU samples are optional and may miss short-lived peaks because they are sampled at a fixed interval.",
            "The health probe measures readiness latency, not process startup time; measure container/process startup separately.",
        ],
    }


def parser() -> argparse.ArgumentParser:
    result = argparse.ArgumentParser(description=__doc__)
    result.add_argument("--dataset", type=Path, required=True, help="JSON dataset with query candidates and relevance grades")
    result.add_argument("--endpoint", default=DEFAULT_ENDPOINT, help=f"reranker URL (default: {DEFAULT_ENDPOINT})")
    result.add_argument("--health-url", default=DEFAULT_HEALTH_URL, help=f"health URL (default: {DEFAULT_HEALTH_URL})")
    result.add_argument("--runs", type=int, default=3, help="measured runs per query (default: 3)")
    result.add_argument("--warmup-runs", type=int, default=1, help="warmup runs using the first query (default: 1)")
    result.add_argument("--timeout", type=float, default=DEFAULT_TIMEOUT_SECONDS, help="per-request timeout in seconds")
    result.add_argument("--max-queries", type=int, help="measure at most this many queries")
    result.add_argument("--top-k", type=int, default=5, help="ranking cutoff for quality metrics (default: 5)")
    result.add_argument("--max-response-bytes", type=int, default=DEFAULT_MAX_RESPONSE_BYTES, help="reject larger responses")
    memory = result.add_mutually_exclusive_group()
    memory.add_argument("--pid", type=int, help="local sidecar PID; samples Linux /proc RSS")
    memory.add_argument("--container", help="Docker container ID or name; samples docker stats RSS")
    result.add_argument("--gpu-pid", type=int, help="CUDA process PID; samples process VRAM with nvidia-smi")
    result.add_argument("--sample-interval", type=float, default=DEFAULT_SAMPLE_INTERVAL_SECONDS, help="resource sampling interval in seconds")
    result.add_argument("--json-out", type=Path, help="write the complete JSON report to this path")
    return result


def validate_args(args: argparse.Namespace) -> None:
    if not args.dataset.is_file():
        raise ValueError(f"dataset does not exist or is not a file: {args.dataset}")
    if args.runs < 1:
        raise ValueError("--runs must be at least 1")
    if args.warmup_runs < 0:
        raise ValueError("--warmup-runs must not be negative")
    if args.timeout <= 0:
        raise ValueError("--timeout must be greater than 0")
    if args.max_queries is not None and args.max_queries < 1:
        raise ValueError("--max-queries must be at least 1")
    if args.top_k < 1:
        raise ValueError("--top-k must be at least 1")
    if args.max_response_bytes < 1:
        raise ValueError("--max-response-bytes must be at least 1")
    if args.sample_interval <= 0:
        raise ValueError("--sample-interval must be greater than 0")


def main() -> int:
    args = parser().parse_args()
    try:
        validate_args(args)
        dataset, queries = load_dataset(args.dataset, args.max_queries)
        health_started = time.perf_counter()
        health = health_check(args.health_url, min(args.timeout, 10.0))
        health_latency_ms = (time.perf_counter() - health_started) * 1000
    except (OSError, ValueError, json.JSONDecodeError, urllib.error.URLError) as exc:
        print(f"benchmark_reranker: {exc}", file=sys.stderr)
        return 2

    rss_reader: Callable[[], int | None] | None = None
    rss_source = "unavailable"
    if args.pid is not None:
        rss_reader = proc_rss_reader(args.pid)
        rss_source = f"proc:{args.pid}"
    elif args.container:
        rss_reader = docker_memory_reader(args.container)
        rss_source = f"docker:{args.container}"
    gpu_reader: Callable[[], int | None] | None = None
    gpu_source = "unavailable"
    if args.gpu_pid is not None:
        gpu_reader = nvidia_memory_reader(args.gpu_pid)
        gpu_source = f"nvidia-smi:pid:{args.gpu_pid}"

    first = queries[0]
    for warmup in range(args.warmup_runs):
        with ResourceSampler(rss_reader, gpu_reader, args.sample_interval) as sampler:
            sample = rerank(args.endpoint, first, args.timeout, args.max_response_bytes, sampler, 0)
        if not sample.success:
            print(f"warmup {warmup + 1} failed for {first.id}: {sample.error}", file=sys.stderr)

    samples: list[Sample] = []
    for query in queries:
        for run in range(1, args.runs + 1):
            with ResourceSampler(rss_reader, gpu_reader, args.sample_interval) as sampler:
                sample = rerank(args.endpoint, query, args.timeout, args.max_response_bytes, sampler, run)
            samples.append(sample)
            outcome = "ok" if sample.success else "failed"
            print(
                f"{outcome:6} {query.id} run={run} latency={sample.latency_ms:.1f}ms"
                f" status={sample.status or '-'} candidates={sample.candidate_count}"
                f" timeout={sample.timed_out}",
                file=sys.stderr if not sample.success else sys.stdout,
            )

    report = build_report(
        args,
        dataset.get("metadata", {}) if isinstance(dataset.get("metadata", {}), dict) else {},
        queries,
        samples,
        health,
        health_latency_ms,
        {"rss": rss_source, "gpu": gpu_source},
    )
    print(json.dumps(report["summary"], indent=2, sort_keys=True))
    if args.json_out:
        args.json_out.parent.mkdir(parents=True, exist_ok=True)
        args.json_out.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
        print(f"report: {args.json_out}")
    return 0 if all(sample.success for sample in samples) else 1


if __name__ == "__main__":
    raise SystemExit(main())
