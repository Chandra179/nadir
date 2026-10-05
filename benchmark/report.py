"""Versioned run envelopes and per-run artifacts using Locust serializers."""

from __future__ import annotations

import csv
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time
import uuid
from types import SimpleNamespace

import gevent
from locust.html import get_html_report
from locust.stats import PERCENTILES_TO_REPORT, StatsCSV, StatsCSVFileWriter


def now():
    return dt.datetime.now(dt.timezone.utc).isoformat()


def git_metadata():
    try:
        revision = subprocess.check_output(["git", "rev-parse", "HEAD"], text=True,
                                           stderr=subprocess.DEVNULL).strip()
        dirty = bool(subprocess.check_output(["git", "status", "--porcelain"],
                                             stderr=subprocess.DEVNULL))
        return revision, dirty
    except (OSError, subprocess.CalledProcessError):
        return None, None


def validate_report(report):
    """Validate the fixed shared contract, without a generic schema engine."""
    if not isinstance(report, dict) or type(report.get("schema_version")) is not int or report["schema_version"] != 1 or report.get("tool") not in ("evaluator", "locust"):
        raise ValueError("unsupported run report version or tool")
    if not isinstance(report.get("runs"), list) or not report["runs"]:
        raise ValueError("run report requires runs")
    required = {"run_id", "run_type", "status", "started_at", "ended_at", "git_revision",
                "inputs", "provenance", "summary", "results", "errors", "artifacts"}
    identifiers = set()
    for run in report["runs"]:
        if not isinstance(run, dict) or not required <= run.keys() or run["status"] not in ("completed", "failed", "interrupted", "empty"):
            raise ValueError("invalid run record")
        if not isinstance(run["run_id"], str) or not run["run_id"] or run["run_id"] in identifiers:
            raise ValueError("run identifiers must be nonempty and unique")
        identifiers.add(run["run_id"])
        if not isinstance(run["run_type"], str) or not run["run_type"]:
            raise ValueError("run_type must be nonempty")
        for field in ("inputs", "provenance", "summary"):
            if not isinstance(run[field], dict):
                raise ValueError(f"{field} must be an object")
        if run["results"] is not None and not isinstance(run["results"], dict):
            raise ValueError("results must be an object or null")
        if not isinstance(run["errors"], list) or not all(isinstance(x, str) for x in run["errors"]):
            raise ValueError("errors must be an array of strings")
        if not isinstance(run["artifacts"], list):
            raise ValueError("artifacts must be an array")
        for field in ("started_at", "ended_at", "git_revision"):
            if run[field] is not None and not isinstance(run[field], str):
                raise ValueError(f"{field} must be a string or null")


def save(environment):
    path = getattr(environment, "benchmark_report_path", None)
    if path is None:
        return
    validate_report(environment.benchmark_report)
    descriptor, temporary = tempfile.mkstemp(prefix=".report-", dir=path.parent)
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as output:
            json.dump(environment.benchmark_report, output, indent=2, allow_nan=False)
            output.write("\n")
        os.replace(temporary, path)
    finally:
        Path(temporary).unlink(missing_ok=True)


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
