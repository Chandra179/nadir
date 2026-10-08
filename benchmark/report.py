"""Versioned run envelopes and per-run artifacts using Locust serializers."""

from __future__ import annotations

import csv
import hashlib
from pathlib import Path
import time
import uuid
from types import SimpleNamespace

import gevent
from locust.html import get_html_report
from locust.stats import PERCENTILES_TO_REPORT, StatsCSV, StatsCSVFileWriter


from eval.report import now, git_metadata, validate_report, write_report


def save(environment):
    path = getattr(environment, "benchmark_report_path", None)
    if path is not None:
        write_report(path, environment.benchmark_report)


def begin(environment, inputs, provenance):
    environment.benchmark_started = time.perf_counter()
    revision, dirty = git_metadata()
    provenance["producer"] = {"entrypoint": "python -m benchmark", "working_tree_dirty": dirty}
    run = {"run_id": uuid.uuid4().hex, "run_type": inputs["workload"], "status": "interrupted",
           "started_at": now(), "ended_at": None, "git_revision": revision,
           "inputs": inputs, "provenance": provenance, "summary": {}, "results": None,
           "errors": [], "artifacts": []}
    environment.benchmark_run = run
    environment.benchmark_csv_writer = None
    directory = getattr(environment.parsed_options, "report_dir", "")
    if directory:
        root = Path(directory)
        root.mkdir(parents=True, exist_ok=True)
        if not hasattr(environment, "benchmark_report"):
            environment.benchmark_report = {"schema_version": 1, "tool": "locust", "runs": []}
            environment.benchmark_report_path = root / "report.json"
        environment.benchmark_report["runs"].append(run)
        save(environment)
        folder = root / "runs" / run["run_id"]
        folder.mkdir(parents=True, exist_ok=False)
        environment.benchmark_artifact_dir = folder
        writer = StatsCSVFileWriter(environment, PERCENTILES_TO_REPORT, str(folder / "locust"),
                                    full_history=getattr(environment.parsed_options, "csv_full_history", False))
        environment.benchmark_csv_writer = writer
        environment.benchmark_csv_greenlet = gevent.spawn(writer.stats_writer)
    return run


def snapshot_artifacts(environment):
    writer = getattr(environment, "benchmark_csv_writer", None)
    if writer is None:
        return []
    # The pinned native serializer records history periodically. Stop its writer,
    # retain the history file, and serialize final samples before the next UI run.
    environment.benchmark_csv_greenlet.kill(block=True)
    if writer.stats_history_csv_filehandle.tell() == 0:
        writer.stats_history_csv_writer.writerow(writer.stats_history_csv_columns)
    writer._stats_history_data_rows(writer.stats_history_csv_writer, time.time())
    writer.stats_history_flush()
    writer.close_files()
    environment.benchmark_csv_writer = None
    folder = environment.benchmark_artifact_dir
    final = StatsCSV(environment, PERCENTILES_TO_REPORT)
    for suffix, serialize in [("stats", final.requests_csv), ("failures", final.failures_csv),
                              ("exceptions", final.exceptions_csv)]:
        with (folder / f"locust_{suffix}.csv").open("w", newline="") as output:
            serialize(csv.writer(output))
    (folder / "locust.html").write_text(get_html_report(environment, show_download_link=False), encoding="utf-8")
    root = environment.benchmark_report_path.parent
    return [{"path": str(path.relative_to(root)), "kind": path.suffix[1:],
             "sha256": hashlib.sha256(path.read_bytes()).hexdigest(), "bytes": path.stat().st_size}
            for path in sorted(folder.iterdir()) if path.is_file()]


def operation_deltas(before, after):
    """Per-operation work done between two /debug/metrics snapshots.

    Each row gives the sample count, total and mean milliseconds for an
    operation/outcome pair, which separates server stages (retrieval, model
    load, prompt evaluation, decode) that a client-side latency lumps together.
    Maxima in the snapshots are process-lifetime values, so they are carried
    only as informational ``lifetime_duration_ms_max``. Unavailable snapshots
    or a server restart between them yield no rows rather than wrong ones.
    """
    def index(snapshot):
        operations = snapshot.get("operations") if isinstance(snapshot, dict) else None
        if not isinstance(operations, list):
            return None
        return {(item["operation"], item["outcome"]): item for item in operations
                if isinstance(item, dict) and "operation" in item and "outcome" in item}

    first, last = index(before), index(after)
    if first is None or last is None:
        return []
    rows = []
    for (operation, outcome), current in sorted(last.items()):
        previous = first.get((operation, outcome), {})
        count = current.get("count", 0) - previous.get("count", 0)
        if count <= 0:
            continue
        total = current.get("duration_ms_sum", 0) - previous.get("duration_ms_sum", 0)
        rows.append({"operation": operation, "outcome": outcome, "count": count,
                     "duration_ms_sum": round(total, 3), "duration_ms_avg": round(total / count, 3),
                     "lifetime_duration_ms_max": current.get("duration_ms_max")})
    return rows


def startup_failure(directory, arguments, message, *, empty=False):
    revision, dirty = git_metadata()
    timestamp = now()
    run = {"run_id": uuid.uuid4().hex, "run_type": "startup", "status": "empty" if empty else "failed",
           "started_at": timestamp, "ended_at": timestamp, "git_revision": revision,
           "inputs": {"arguments": arguments}, "provenance": {"producer": {
               "entrypoint": "python -m benchmark", "working_tree_dirty": dirty}},
           "summary": {}, "results": None, "errors": [message], "artifacts": []}
    save(SimpleNamespace(benchmark_report_path=directory / "report.json",
                         benchmark_report={"schema_version": 1, "tool": "locust", "runs": [run]}))
