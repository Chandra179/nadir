"""Shared version-1 report contract; standard library only."""

import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile


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


def atomic_json(path, value):
    path = Path(path)
    descriptor, temporary = tempfile.mkstemp(prefix=".report-", dir=path.parent)
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as output:
            json.dump(value, output, indent=2, ensure_ascii=False, allow_nan=False)
            output.write("\n")
        os.replace(temporary, path)
    finally:
        Path(temporary).unlink(missing_ok=True)


def write_report(path, report):
    validate_report(report)
    atomic_json(path, report)


def artifact(path, root=None):
    path = Path(path).resolve()
    return {"path": str(path.relative_to(Path(root).resolve())) if root else str(path),
            "sha256": hashlib.sha256(path.read_bytes()).hexdigest(),
            "bytes": path.stat().st_size, "kind": path.suffix[1:]}
