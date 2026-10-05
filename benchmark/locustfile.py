"""Locust API workloads. Run with python -m benchmark from the repository root."""

from __future__ import annotations

import datetime as dt
import hashlib
import json
import logging
from pathlib import Path
import platform
import subprocess
import time

import gevent
from locust import HttpUser, __version__ as locust_version, constant, events, task
from locust.exception import StopUser
import requests

from benchmark.api import ApiClient, BenchmarkError, Config, Measurement


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
    parser.add_argument("--report-dir", default="", include_in_web_ui=False, help="metadata directory; set by python -m benchmark")


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


def save_metadata(environment):
    if getattr(environment, "benchmark_metadata_path", None):
        environment.benchmark_metadata_path.write_text(json.dumps(environment.benchmark_metadata, indent=2) + "\n")


@events.test_start.add_listener
def prepare(environment, **kwargs):
    environment.benchmark_startup_error = None
    environment.benchmark_config = None
    try:
        config = configuration(environment)
        readiness = snapshot(config.host, "/api/v1/ready", config.timeout, required=True)
        if readiness.get("ready") is not True or readiness.get("error"):
            raise BenchmarkError("API readiness did not confirm ready=true")
        environment.benchmark_config = config
        environment.benchmark_started = time.perf_counter()
        if not environment.parsed_options.report_dir:
            return
        directory = Path(environment.parsed_options.report_dir)
        directory.mkdir(parents=True, exist_ok=True)
        path = directory / "metadata.json"
        if not getattr(environment, "benchmark_metadata", None):
            environment.benchmark_metadata = {"schema_version": 1, "runs": []}
            environment.benchmark_metadata_path = path
            with path.open("x") as output:
                json.dump(environment.benchmark_metadata, output)
        try:
            head = subprocess.check_output(["git", "rev-parse", "HEAD"], text=True, stderr=subprocess.DEVNULL).strip()
        except (OSError, subprocess.CalledProcessError):
            head = None
        run = {"started_at": dt.datetime.now(dt.timezone.utc).isoformat(), "host": config.host,
               "workload": config.workload, "query": config.query, "follow_up": config.follow_up,
               "top_k": config.top_k, "timeout_seconds": config.timeout,
               "users": environment.parsed_options.num_users, "spawn_rate": environment.parsed_options.spawn_rate,
               "run_time_seconds": environment.parsed_options.run_time, "git_head": head,
               "locust_version": locust_version, "python_version": platform.python_version(),
               "process_scope": "metrics describe one target API process",
               "corpus_before": snapshot(config.host, "/api/v1/documents", config.timeout),
               "metrics_before": snapshot(config.host, "/debug/metrics", config.timeout)}
        if config.workload == "upload":
            run["upload"] = {"path": str(config.upload_file), "bytes": config.upload_file.stat().st_size,
                             "sha256": hashlib.sha256(config.upload_file.read_bytes()).hexdigest()}
        environment.benchmark_metadata["runs"].append(run)
        save_metadata(environment)
    except (BenchmarkError, OSError, ValueError) as error:
        logging.error("Benchmark startup failed: %s", error)
        environment.benchmark_startup_error = str(error)
        environment.process_exit_code = 2
        gevent.spawn(environment.runner.quit)


@events.test_stop.add_listener
def finish(environment, **kwargs):
    config = getattr(environment, "benchmark_config", None)
    metadata = getattr(environment, "benchmark_metadata", None)
    if config and metadata and metadata["runs"]:
        run = metadata["runs"][-1]
        elapsed = time.perf_counter() - environment.benchmark_started
        workflows = [entry for entry in environment.stats.entries.values() if entry.method == "WORKFLOW"]
        failed = environment.stats.total.num_failures > 0
        if failed:
            environment.process_exit_code = 1
        run.update({"ended_at": dt.datetime.now(dt.timezone.utc).isoformat(),
                    "status": "failed" if failed else "passed" if workflows else "empty",
                    "failed_http_operations": sum(entry.num_failures for entry in environment.stats.entries.values()
                                                  if entry.method not in {"WORKFLOW", "TTFT"}),
                    "elapsed_seconds": elapsed,
                    "workflows": [{"name": entry.name, "completed": entry.num_requests - entry.num_failures,
                                   "failed": entry.num_failures} for entry in workflows],
                    "completed_workflows_per_second": sum(entry.num_requests - entry.num_failures
                                                           for entry in workflows) / elapsed,
                    "corpus_after": snapshot(config.host, "/api/v1/documents", config.timeout),
                    "metrics_after": snapshot(config.host, "/debug/metrics", config.timeout)})
        save_metadata(environment)


@events.quitting.add_listener
def check_exit(environment, **kwargs):
    workflows = [entry for entry in environment.stats.entries.values() if entry.method == "WORKFLOW"]
    if getattr(environment, "benchmark_startup_error", None) or not sum(entry.num_requests for entry in workflows):
        environment.process_exit_code = 2
    elif environment.stats.total.num_failures or any(entry.num_failures for entry in workflows):
        environment.process_exit_code = 1
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
