"""API and stream semantics shared by the Locust workloads.

Stream framing and scoped history cleanup follow the retired API probes.
Scheduling, HTTP statistics and percentile calculation belong to Locust.
"""

from __future__ import annotations

from dataclasses import dataclass
import mimetypes
from pathlib import Path
import time
from typing import Iterable
from urllib.parse import quote, urlsplit
import uuid

import gevent
from requests import RequestException


class BenchmarkError(Exception):
    pass


@dataclass(frozen=True)
class Config:
    host: str
    workload: str
    query: str = ""
    follow_up: str = ""
    upload_file: Path | None = None
    top_k: int = 5
    timeout: float = 120

    def validate(self) -> None:
        url = urlsplit(self.host)
        if url.scheme not in {"http", "https"} or not url.netloc or url.query or url.fragment:
            raise BenchmarkError("--host must be an explicit HTTP(S) API URL")
        if self.workload not in {"retrieval", "chat", "upload", "mixed", "cache", "followup"}:
            raise BenchmarkError("unknown workload")
        if self.top_k < 1 or not 0 < self.timeout < float("inf"):
            raise BenchmarkError("--top-k and --timeout must be positive and finite")
        if self.workload != "upload" and not self.query.strip():
            raise BenchmarkError("--query is required for this workload")
        if self.workload == "followup" and not self.follow_up.strip():
            raise BenchmarkError("--follow-up is required for the followup workload")
        if self.workload == "upload" and (not self.upload_file or not self.upload_file.is_file()):
            raise BenchmarkError("--upload-file must name an existing file")


@dataclass
class Measurement:
    first_token_ms: float | None = None
    response_bytes: int = 0
    delivery: str = "none"


def consume_stream(lines: Iterable[bytes], started: float) -> Measurement:
    """Parse raw SSE data and require a successful terminal event."""
    measured = Measurement(delivery="stream")
    event = ""
    payload: list[str] = []

    def consume() -> bool:
        text = "\n".join(payload)
        if event == "token":
            measured.response_bytes += len(text.encode("utf-8"))
            if measured.first_token_ms is None and text.strip():
                measured.first_token_ms = (time.perf_counter() - started) * 1000
        elif event == "generror":
            raise BenchmarkError(f"generation stream failed: {text}")
        elif event == "resync":
            raise BenchmarkError("generation stream replay gap")
        elif event == "done" and measured.first_token_ms is None:
            raise BenchmarkError("generation completed without a nonempty answer")
        return event == "done"

    for raw in lines:
        line = raw.decode("utf-8", errors="strict").rstrip("\r\n")
        if not line:
            if consume():
                return measured
            event, payload = "", []
        elif line.startswith("event:"):
            event = line[6:].lstrip(" ")
        elif line.startswith("data:"):
            payload.append(line[5:].removeprefix(" "))
    if (event or payload) and consume():
        return measured
    raise BenchmarkError("generation stream ended without done")


class ApiClient:
    def __init__(self, client, config: Config):
        self.client = client
        self.config = config
        self.sessions: set[str] = set()
        self.active_turns: set[str] = set()

    def json_request(self, method: str, path: str, name: str, *, allowed=(200,), **kwargs) -> dict:
        with self.client.request(method, path, name=name, catch_response=True,
                                 timeout=self.config.timeout, **kwargs) as response:
            try:
                if response.status_code not in allowed:
                    response.raise_for_status()
                    raise BenchmarkError(f"unexpected HTTP {response.status_code}")
                if response.status_code in {204, 404}:
                    response.success()  # Expected absence while history is being persisted.
                    return {}
                result = response.json()
                if not isinstance(result, dict):
                    raise BenchmarkError("API response must be a JSON object")
                # Track newly minted history before checking domain errors.
                if method == "POST" and path == "/api/v1/turns":
                    requested = kwargs.get("json", {}).get("session_id", "")
                    returned = result.get("session_id", "")
                    if not requested and returned:
                        self.sessions.add(returned)
                    if requested and returned != requested:
                        raise BenchmarkError("follow-up returned a different session")
                    if result.get("streaming") and result.get("turn_id"):
                        self.active_turns.add(result["turn_id"])
                if result.get("error") or result.get("generate_error"):
                    raise BenchmarkError(result.get("error") or result["generate_error"])
                return result
            except (BenchmarkError, RequestException, ValueError, OSError) as error:
                response.failure(error)
                raise BenchmarkError(str(error)) from error

    def turn(self, query: str, *, generate=False, skip_cache=True, session_id="", label="retrieval"):
        started = time.perf_counter()
        result = self.json_request("POST", "/api/v1/turns", f"turn/{label}", json={
            "query": query, "top_k": self.config.top_k, "generate": generate,
            "skip_cache": skip_cache, "session_id": session_id,
        })
        measured = Measurement()
        if not generate:
            return result, measured
        stream_url = result.get("stream_url")
        if not stream_url and result.get("has_answer") and result.get("answer"):
            return result, Measurement(response_bytes=len(result["answer"].encode()), delivery="immediate")
        if not stream_url or not result.get("turn_id"):
            raise BenchmarkError("generation requested but no answer or stream was returned")
        expected = f"/api/v1/turns/{quote(result['turn_id'], safe='')}/events"
        if stream_url != expected:
            raise BenchmarkError("unexpected generation stream URL")
        self.active_turns.add(result["turn_id"])
        stream_started = time.perf_counter()
        with self.client.get(stream_url, name=f"stream/{label}", stream=True,
                             catch_response=True, timeout=self.config.timeout,
                             headers={"Accept": "text/event-stream"}) as response:
            try:
                response.raise_for_status()
                if response.status_code != 200:
                    raise BenchmarkError(f"unexpected stream HTTP {response.status_code}")
                measured = consume_stream(response.iter_lines(chunk_size=1), started)
                self.active_turns.discard(result["turn_id"])
            except (BenchmarkError, RequestException, ValueError, OSError) as error:
                response.failure(error)
                raise BenchmarkError(str(error)) from error
            finally:
                # Locust normally measures headers only for stream=True.
                response.request_meta["response_time"] = (time.perf_counter() - stream_started) * 1000
                response.request_meta["response_length"] = measured.response_bytes
                response.close()
        return result, measured

    def wait_saved(self, session: str, query: str) -> None:
        deadline = time.perf_counter() + min(15, self.config.timeout)
        while True:
            detail = self.json_request("GET", f"/api/v1/sessions/{quote(session, safe='')}",
                                       "history/wait", allowed=(200, 404))
            if any(turn.get("query") == query for turn in detail.get("turns", [])):
                return
            if time.perf_counter() >= deadline:
                raise BenchmarkError("seed history was not persisted")
            gevent.sleep(0.1)

    def cache(self) -> Measurement:
        self.turn(self.config.query, skip_cache=False, label="cache-seed")
        deadline = time.perf_counter() + min(5, self.config.timeout)
        while True:
            result, measured = self.turn(self.config.query, skip_cache=False, label="cache-repeat")
            if result.get("from_cache"):
                return measured
            if time.perf_counter() >= deadline:
                raise BenchmarkError("repeat query did not produce a cache hit")
            gevent.sleep(0.1)

    def followup(self) -> Measurement:
        seed, _ = self.turn(self.config.query, label="followup-seed")
        session = seed.get("session_id")
        if not session:
            raise BenchmarkError("follow-up requires enabled history")
        self.wait_saved(session, self.config.query)
        result, measured = self.turn(self.config.follow_up, generate=True, session_id=session, label="followup")
        if not result.get("rewritten_query"):
            raise BenchmarkError("follow-up rewriting was not observed")
        return measured

    def upload(self) -> Measurement:
        path = self.config.upload_file
        raw = path.read_bytes()
        filename = f"benchmark-{uuid.uuid4().hex}{path.suffix}"
        result = self.json_request("POST", "/api/v1/documents", "documents/upload", files={
            "files": (filename, raw, mimetypes.guess_type(path.name)[0] or "application/octet-stream"),
        })
        if result.get("failed", 0) or result.get("processed", 0) < 1 or result.get("skipped", 0):
            raise BenchmarkError("upload did not complete fresh indexing")
        return Measurement()

    def cleanup(self) -> list[str]:
        errors = []
        for turn in sorted(self.active_turns):
            try:
                self.json_request("POST", f"/api/v1/turns/{quote(turn, safe='')}/cancel",
                                  "cleanup/cancel", allowed=(200, 204, 404))
                self.active_turns.discard(turn)
            except BenchmarkError as error:
                errors.append(str(error))
        for session in sorted(self.sessions):
            try:
                self.json_request("DELETE", f"/api/v1/sessions/{quote(session, safe='')}",
                                  "cleanup/session", allowed=(200, 204, 404))
                self.sessions.discard(session)
            except BenchmarkError as error:
                errors.append(str(error))
        return errors
