#!/usr/bin/env python3
"""Run comparable reranker model/backend profiles and collect one report.

The direct benchmark measures a ready sidecar. This runner owns the process
lifecycle so startup latency, startup RSS, model-load failures, and the direct
benchmark results are recorded with the same profile metadata.

Profiles are intentionally explicit and sequential. A failed profile is
recorded and does not prevent the remaining profiles from running. This is
important on heterogeneous machines where CUDA or a large model may be
unavailable.
"""

from __future__ import annotations

import argparse
import datetime as dt
import json
import os
import signal
import subprocess
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
DEFAULT_ENDPOINT = "http://127.0.0.1:5002/rerank"
DEFAULT_HEALTH = "http://127.0.0.1:5002/health"
DEFAULT_STARTUP_TIMEOUT = 600.0

PROFILES: dict[str, dict[str, str]] = {
    "bge-m3-cpu": {
        "model": "BAAI/bge-reranker-v2-m3",
        "backend": "torch",
        "device": "cpu",
    },
    "bge-m3-gpu": {
        "model": "BAAI/bge-reranker-v2-m3",
        "backend": "torch",
        "device": "cuda",
    },
    "gte-multilingual-base-cpu": {
        "model": "Alibaba-NLP/gte-multilingual-reranker-base",
        "backend": "torch",
        "device": "cpu",
    },
    "minilm-l6-cpu": {
        "model": "cross-encoder/ms-marco-MiniLM-L-6-v2",
        "backend": "torch",
        "device": "cpu",
    },
    "minilm-l6-onnx-cpu": {
        "model": "cross-encoder/ms-marco-MiniLM-L-6-v2",
        "backend": "onnx",
        "device": "cpu",
    },
    "minilm-l6-int8-onnx-cpu": {
        "model": "cross-encoder/ms-marco-MiniLM-L-6-v2",
        "backend": "onnx",
        "device": "cpu",
        "quantized": "true",
    },
}


def proc_rss(pid: int) -> int | None:
    try:
        for line in (Path("/proc") / str(pid) / "status").read_text(encoding="utf-8").splitlines():
            if line.startswith("VmRSS:"):
                return int(line.split()[1]) * 1024
    except (FileNotFoundError, PermissionError, ValueError):
        return None
    return None


def resolve_cached_model(model: str) -> str | None:
    """Resolve a locally cached Hugging Face snapshot without network access."""

    try:
        from huggingface_hub import snapshot_download

        return snapshot_download(model, local_files_only=True)
    except Exception:
        return None


def health(url: str, timeout: float) -> tuple[int | None, dict[str, object] | None, str | None]:
    request = urllib.request.Request(url, method="GET")
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            body = response.read(64 * 1024).decode("utf-8", errors="replace")
            return response.status, json.loads(body), None
    except urllib.error.HTTPError as exc:
        try:
            body = exc.read(64 * 1024).decode("utf-8", errors="replace")
        except OSError:
            body = ""
        try:
            payload = json.loads(body)
        except json.JSONDecodeError:
            payload = None
        return exc.code, payload, body or str(exc)
    except (OSError, json.JSONDecodeError) as exc:
        return None, None, str(exc)


def wait_ready(url: str, process: subprocess.Popen[bytes], timeout: float) -> dict[str, object]:
    started = time.perf_counter()
    last_error: str | None = None
    while time.perf_counter() - started < timeout:
        if process.poll() is not None:
            raise RuntimeError(f"sidecar exited with code {process.returncode}: {last_error or 'see log'}")
        status, payload, error = health(url, 2.0)
        if status == 200 and isinstance(payload, dict) and payload.get("status") == "ok":
            return {
                "startup_ms": round((time.perf_counter() - started) * 1000, 3),
                "health": payload,
            }
        if (
            status == 503
            and isinstance(payload, dict)
            and payload.get("error")
            and payload.get("error") != "model is not loaded"
        ):
            raise RuntimeError(f"sidecar model failed to load: {payload['error']}")
        last_error = error or (json.dumps(payload) if payload else f"HTTP {status}")
        time.sleep(0.25)
    raise TimeoutError(f"sidecar readiness timed out after {timeout}s: {last_error or 'no response'}")


def run_profile(args: argparse.Namespace, name: str, settings: dict[str, str], output: Path) -> dict[str, object]:
    resolved_model = resolve_cached_model(settings["model"])
    model_for_process = (
        resolved_model if settings.get("quantized") == "true" else (resolved_model or settings["model"])
    )
    quantized_dir = args.quantized_dir or os.environ.get("RERANKER_QUANTIZED_DIR")
    if settings.get("quantized") == "true" and not quantized_dir:
        return {
            "name": name,
            "requested_model": settings["model"],
            "requested_backend": settings["backend"],
            "requested_device": settings["device"],
            "resolved_model": resolved_model,
            "status": "unavailable",
            "error": "--quantized-dir or RERANKER_QUANTIZED_DIR is required",
        }
    env = os.environ.copy()
    env.update(
        {
            "RERANKER_MODEL": model_for_process,
            "RERANKER_BACKEND": settings["backend"],
            "RERANKER_DEVICE": settings["device"],
            "RERANKER_PORT": str(args.port),
            "RERANKER_MAX_CONCURRENT": "1",
            "HF_HUB_OFFLINE": "1",
            "TRANSFORMERS_OFFLINE": "1",
        }
    )
    if settings.get("quantized") == "true":
        assert quantized_dir is not None
        env["RERANKER_QUANTIZED_DIR"] = str(quantized_dir)
    log_path = output.with_suffix(".sidecar.log")
    started = time.perf_counter()
    process: subprocess.Popen[bytes] | None = None
    startup_rss: int | None = None
    try:
        with log_path.open("wb") as log:
            process = subprocess.Popen(
                [sys.executable, str(ROOT / "sidecars/reranker/main.py")],
                cwd=ROOT,
                env=env,
                stdout=log,
                stderr=subprocess.STDOUT,
            )
        readiness = wait_ready(args.health_url, process, args.startup_timeout)
        startup_rss = proc_rss(process.pid)
        command = [
            sys.executable,
            str(ROOT / "scripts/benchmark_reranker.py"),
            "--dataset",
            str(args.dataset),
            "--endpoint",
            args.endpoint,
            "--health-url",
            args.health_url,
            "--corpus-dir",
            str(args.corpus_dir),
            "--runs",
            str(args.runs),
            "--warmup-runs",
            str(args.warmup_runs),
            "--timeout",
            str(args.request_timeout),
            "--pid",
            str(process.pid),
            "--json-out",
            str(output),
        ]
        if settings["device"] == "cuda":
            command.extend(["--gpu-pid", str(process.pid)])
        completed = subprocess.run(command, cwd=ROOT, check=False)
        report: dict[str, object] = json.loads(output.read_text(encoding="utf-8")) if output.is_file() else {}
        report["profile"] = {
            "name": name,
            "requested_model": settings["model"],
            "requested_backend": settings["backend"],
            "requested_device": settings["device"],
            "resolved_model": resolved_model,
            "startup_ms": readiness["startup_ms"],
            "startup_rss_bytes": startup_rss,
            "benchmark_exit_code": completed.returncode,
            "sidecar_log": str(log_path),
            "health": readiness["health"],
            "summary": report.get("summary"),
            "resources": report.get("resources"),
        }
        report["target"] = {**report.get("target", {}), "profile": report["profile"]}
        output.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
        return report["profile"]
    except (OSError, RuntimeError, TimeoutError, json.JSONDecodeError) as exc:
        return {
            "name": name,
            "requested_model": settings["model"],
            "requested_backend": settings["backend"],
            "requested_device": settings["device"],
            "resolved_model": resolved_model,
            "status": "unavailable",
            "startup_ms": round((time.perf_counter() - started) * 1000, 3),
            "startup_rss_bytes": startup_rss,
            "error": str(exc),
            "sidecar_log": str(log_path),
        }
    finally:
        if process is not None and process.poll() is None:
            process.send_signal(signal.SIGTERM)
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=5)


def parser() -> argparse.ArgumentParser:
    result = argparse.ArgumentParser(description=__doc__)
    result.add_argument("--dataset", type=Path, required=True)
    result.add_argument("--corpus-dir", type=Path, required=True)
    result.add_argument("--output-dir", type=Path, required=True)
    result.add_argument("--profiles", default=",".join(PROFILES), help="comma-separated profile names")
    result.add_argument("--port", type=int, default=5002)
    result.add_argument("--endpoint", default=DEFAULT_ENDPOINT)
    result.add_argument("--health-url", default=DEFAULT_HEALTH)
    result.add_argument("--runs", type=int, default=1)
    result.add_argument("--warmup-runs", type=int, default=1)
    result.add_argument("--request-timeout", type=float, default=60.0)
    result.add_argument("--startup-timeout", type=float, default=DEFAULT_STARTUP_TIMEOUT)
    result.add_argument("--quantized-dir", type=Path, help="baked ONNX directory for the int8 profile")
    result.add_argument("--json-out", type=Path, required=True)
    return result


def main() -> int:
    args = parser().parse_args()
    if not args.dataset.is_file() or not args.corpus_dir.is_dir():
        print("benchmark_reranker_profiles: dataset and corpus-dir must exist", file=sys.stderr)
        return 2
    names = [name.strip() for name in args.profiles.split(",") if name.strip()]
    unknown = sorted(set(names) - set(PROFILES))
    if unknown:
        print(f"unknown profiles: {', '.join(unknown)}", file=sys.stderr)
        return 2
    args.output_dir.mkdir(parents=True, exist_ok=True)
    profiles: list[dict[str, object]] = []
    for name in names:
        print(f"==> profile {name}", file=sys.stderr)
        output = args.output_dir / f"{name}.json"
        profiles.append(run_profile(args, name, PROFILES[name], output))
    report = {
        "schema_version": 1,
        "generated_at": dt.datetime.now(dt.timezone.utc).isoformat(),
        "dataset": str(args.dataset),
        "corpus_dir": str(args.corpus_dir),
        "runs": args.runs,
        "profiles": profiles,
        "notes": [
            "The dataset is the repository's engineering-generated synthetic fixture unless release-gate metadata is supplied separately.",
            "Unavailable profiles are retained as evidence gaps and are not substituted with another model or device.",
            "Startup latency ends at the first HTTP 200 readiness response; request latency is measured separately by benchmark_reranker.py.",
        ],
    }
    args.json_out.parent.mkdir(parents=True, exist_ok=True)
    args.json_out.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    print(f"comparison report: {args.json_out}")
    return 0 if all(profile.get("status", "ok") == "ok" for profile in profiles) else 1


if __name__ == "__main__":
    raise SystemExit(main())
