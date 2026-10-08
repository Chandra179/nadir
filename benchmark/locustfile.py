"""Locust API workloads. Run with python -m benchmark from the repository root."""

from __future__ import annotations

import hashlib
import logging
from pathlib import Path
import platform
import time

import gevent
from locust import HttpUser, __version__ as locust_version, constant, events, task
from locust.exception import StopUser
from locust.stats import StatsError
import requests

from benchmark.api import ApiClient, BenchmarkError, Config, Measurement
from benchmark.report import begin, now, operation_deltas, save, snapshot_artifacts


@events.init_command_line_parser.add_listener
def arguments(parser):
    parser.add_argument("--ui", action="store_true", include_in_web_ui=False,
                        help="start the local UI through python -m benchmark")
    parser.add_argument("--output-dir", include_in_web_ui=False,
                        help="new report directory for python -m benchmark")
    parser.add_argument("--workload", choices=["retrieval", "chat", "upload", "mixed", "cache", "followup"], default="retrieval")
    parser.add_argument("--query", default="", help="question for the indexed corpus")
    parser.add_argument("--follow-up", default="", help="follow-up question after the seed query")
    parser.add_argument("--upload-file", default="", help="file to index with a unique filename each iteration")
    parser.add_argument("--top-k", type=int, default=5)
    parser.add_argument("--timeout", type=float, default=120, help="request and complete-workflow timeout in seconds")
    parser.add_argument("--report-dir", default="", include_in_web_ui=False, help="run report directory; set by python -m benchmark")


def configuration(environment) -> Config:
    options = environment.parsed_options
    config = Config(host=environment.host or options.host or "", workload=options.workload,
                    query=options.query, follow_up=options.follow_up,
                    upload_file=Path(options.upload_file) if options.upload_file else None,
                    top_k=options.top_k, timeout=options.timeout)
    config.validate()
    return config


def snapshot(host: str, path: str, timeout: float, *, required=False):
    try:
        with requests.get(host.rstrip("/") + path, timeout=min(timeout, 5)) as response:
            response.raise_for_status()
            value = response.json()
            if not isinstance(value, dict):
                raise ValueError("snapshot must be an object")
            return value
    except (requests.RequestException, ValueError, OSError) as error:
        if required:
            raise BenchmarkError(f"API readiness failed: {error}") from error
        return {"unavailable": str(error)}


@events.test_start.add_listener
def prepare(environment, **kwargs):
    environment.benchmark_startup_error = None
    environment.benchmark_config = None
    try:
        options = environment.parsed_options
        inputs = {"host": environment.host or options.host or "", "workload": options.workload,
                  "query": options.query, "follow_up": options.follow_up, "upload_file": options.upload_file,
                  "top_k": options.top_k, "timeout_seconds": options.timeout,
                  "users": options.num_users, "spawn_rate": options.spawn_rate,
                  "run_time_seconds": options.run_time}
        run = begin(environment, inputs, {"locust_version": locust_version,
                    "python_version": platform.python_version(),
                    "process_scope": "metrics describe one target API process"})
        config = configuration(environment)
        environment.benchmark_config = config
        readiness = snapshot(config.host, "/api/v1/ready", config.timeout, required=True)
        if readiness.get("ready") is not True or readiness.get("error"):
            raise BenchmarkError("API readiness did not confirm ready=true")
        run["provenance"].update({"corpus_before": snapshot(config.host, "/api/v1/documents", config.timeout),
                                  "metrics_before": snapshot(config.host, "/debug/metrics", config.timeout)})
        if config.workload == "upload":
            run["inputs"]["upload"] = {"path": str(config.upload_file), "bytes": config.upload_file.stat().st_size,
                                       "sha256": hashlib.sha256(config.upload_file.read_bytes()).hexdigest()}
        save(environment)
    except (BenchmarkError, OSError, ValueError) as error:
        logging.error("Benchmark startup failed: %s", error)
        environment.benchmark_startup_error = str(error)
        environment.process_exit_code = 2
        gevent.spawn(environment.runner.quit)


@events.test_stop.add_listener
def finish(environment, **kwargs):
    run = getattr(environment, "benchmark_run", None)
    if not run or run["ended_at"] is not None:
        return
    config = getattr(environment, "benchmark_config", None)
    elapsed = max(time.perf_counter() - environment.benchmark_started, 1e-9)
    entries = list(environment.stats.entries.values())
    workflows = [entry for entry in entries if entry.method == "WORKFLOW"]
    failures = [f"{item.method} {item.name}: {StatsError.parse_error(item.error)}"
                for item in environment.stats.errors.values()]
    failures.extend(str(item["msg"]) for item in environment.runner.exceptions.values())
    startup_error = getattr(environment, "benchmark_startup_error", None)
    if startup_error:
        failures.insert(0, startup_error)
    interrupted = any("workflow interrupted before completion" in error for error in failures)
    status = "interrupted" if interrupted else "failed" if failures else "completed" if workflows else "empty"
    if status == "empty":
        failures.append("no workflows were attempted")
    run.update({"ended_at": now(), "status": status, "errors": list(dict.fromkeys(failures)),
                "results": {"stats": [entry.serialize() for entry in entries],
                            "exceptions": environment.runner.exceptions},
                "summary": {"elapsed_seconds": elapsed,
                    "failed_http_operations": sum(entry.num_failures for entry in entries
                                                  if entry.method not in {"WORKFLOW", "TTFT"}),
                    "workflows": [{"name": entry.name, "completed": entry.num_requests - entry.num_failures,
                                   "failed": entry.num_failures} for entry in workflows],
                    "completed_workflows_per_second": sum(entry.num_requests - entry.num_failures
                                                           for entry in workflows) / elapsed,
                    "latency": [{"type": entry.method, "name": entry.name,
                                 "p50_ms": entry.get_response_time_percentile(0.5),
                                 "p95_ms": entry.get_response_time_percentile(0.95),
                                 "p99_ms": entry.get_response_time_percentile(0.99)} for entry in entries]}})
    if config:
        run["provenance"].update({"corpus_after": snapshot(config.host, "/api/v1/documents", config.timeout),
                                  "metrics_after": snapshot(config.host, "/debug/metrics", config.timeout)})
        run["summary"]["server_operations"] = operation_deltas(
            run["provenance"].get("metrics_before"), run["provenance"]["metrics_after"])
    try:
        run["artifacts"] = snapshot_artifacts(environment)
    except Exception as error:
        run["errors"].append(f"artifact reporting failed: {error}")
        run["status"] = "failed"
    if run["status"] != "completed":
        environment.process_exit_code = 2 if startup_error or status == "empty" else 1
    try:
        save(environment)
    except (OSError, ValueError) as error:
        logging.error("Benchmark report failed: %s", error)
        environment.process_exit_code = 2


@events.quitting.add_listener
def check_exit(environment, **kwargs):
    # Locust fires quitting before stopping users; test_stop records final samples.
    report = getattr(environment, "benchmark_report", {})
    runs = report.get("runs") or [getattr(environment, "benchmark_run", {})]
    if not any(run for run in runs):
        environment.process_exit_code = 2
    elif any(run.get("ended_at") is not None and run.get("status") != "completed" for run in runs):
        environment.process_exit_code = environment.process_exit_code or 1
    # Preserve Locust's native exception/failure exit status otherwise.


class NadirUser(HttpUser):
    wait_time = constant(1)

    def on_start(self):
        if getattr(self.environment, "benchmark_startup_error", None):
            raise StopUser()
        self.client.base_url = self.client.base_url.rstrip("/")
        self.api = ApiClient(self.client, configuration(self.environment))
        self.mixed_chat = False

    def on_stop(self):
        if hasattr(self, "api"):
            self.api.cleanup()

    @task
    def workload(self):
        config = self.api.config
        name = config.workload
        if name == "mixed":
            name = "chat" if self.mixed_chat else "retrieval"
            self.mixed_chat = not self.mixed_chat
        started = time.perf_counter()
        measured = Measurement()
        error = BenchmarkError("workflow interrupted before completion")
        try:
            with gevent.Timeout(config.timeout, BenchmarkError("workflow timed out")):
                if name == "upload":
                    measured = self.api.upload()
                elif name == "cache":
                    measured = self.api.cache()
                elif name == "followup":
                    measured = self.api.followup()
                else:
                    _, measured = self.api.turn(config.query, generate=name == "chat", label=name)
                error = None
        except Exception as caught:
            error = caught
        finally:
            self.environment.events.request.fire(request_type="WORKFLOW", name=name,
                response_time=(time.perf_counter() - started) * 1000, response_length=measured.response_bytes,
                exception=error, context={"delivery": measured.delivery})
            if error is None and measured.first_token_ms is not None:
                self.environment.events.request.fire(request_type="TTFT", name=name,
                    response_time=measured.first_token_ms, response_length=0, exception=None, context={})
            self.api.cleanup()
