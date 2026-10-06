import locust  # Apply cooperative I/O before importing HTTP/test helpers.

from dataclasses import replace
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
from urllib.parse import quote
import unittest
from unittest.mock import patch

import gevent
from gevent.pywsgi import WSGIServer
from locust.env import Environment

from benchmark.api import BenchmarkError, Config, consume_stream
from benchmark.locustfile import NadirUser


class StubAPI:
    def __init__(self):
        self.server = WSGIServer(("127.0.0.1", 0), self.application, log=None)
        self.records = []
        self.sessions = {"existing-session": "existing question"}
        self.deleted = []
        self.cancelled = []
        self.polls = {}
        self.cache_queries = {}
        self.ready = True
        self.fail_upload = False
        self.fail_cleanup = False
        self.server.start()
        self.host = f"http://127.0.0.1:{self.server.server_port}"

    def close(self):
        self.server.stop(timeout=0.2)

    def application(self, environ, start_response):
        method, path = environ["REQUEST_METHOD"], environ["PATH_INFO"]
        body = environ["wsgi.input"].read(int(environ.get("CONTENT_LENGTH") or 0))
        self.records.append((method, path, body))

        def response(value=None, status="200 OK"):
            raw = json.dumps(value or {}).encode()
            start_response(status, [("Content-Type", "application/json"), ("Content-Length", str(len(raw)))])
            return [raw]

        if path == "/api/v1/ready":
            return response({"ready": self.ready}, "200 OK" if self.ready else "503 Unavailable")
        if path == "/debug/metrics":
            return response({"operations": [{"operation": "turn", "count": len(self.records)}], "gauges": {}})
        if path == "/api/v1/documents":
            if method == "POST":
                return response({"processed": int(not self.fail_upload), "skipped": 0, "failed": int(self.fail_upload)})
            return response({"count": 1, "documents": [{"file_path": "fixture.md", "source_sha": "fixture-hash"}]})
        if path == "/api/v1/turns" and method == "POST":
            request = json.loads(body)
            query = request["query"]
            if query == "http-error":
                return response({"error": "unavailable"}, "503 Unavailable")
            session = request.get("session_id") or f"session-{len(self.records)}"
            self.sessions[session] = query
            turn = {"session_id": session, "from_cache": False}
            if query in {"api-error", "generate-error"}:
                turn["error" if query == "api-error" else "generate_error"] = "domain failure"
                return response(turn)
            if not request["skip_cache"]:
                self.cache_queries[query] = self.cache_queries.get(query, 0) + 1
                turn["from_cache"] = self.cache_queries[query] > 1 and query != "no-cache"
            if request.get("session_id") and query != "no-rewrite":
                turn["rewritten_query"] = "resolved follow-up question"
            if request["generate"]:
                if query == "immediate":
                    turn.update({"has_answer": True, "answer": "An immediate answer"})
                elif query != "no-answer":
                    turn.update({"streaming": True, "turn_id": query,
                                 "stream_url": f"/api/v1/turns/{quote(query, safe='')}/events"})
            return response(turn)
        if path.endswith("/events"):
            query = path.split("/")[-2]
            start_response("200 OK", [("Content-Type", "text/event-stream")])

            def stream():
                gevent.sleep(0.01)
                yield "event: token\ndata: alpha\ndata: café\n\n".encode()
                if query == "timeout":
                    while True:
                        gevent.sleep(0.02)
                        yield b": heartbeat\n\n"
                gevent.sleep(0.01)
                if query == "stream-error":
                    yield b"event: generror\ndata: failed\n\n"
                elif query == "gap":
                    yield b"event: resync\ndata: lost\n\n"
                elif query != "no-done":
                    yield b"event: done\ndata: 1\n\n"
            return stream()
        if path.endswith("/cancel") and method == "POST":
            self.cancelled.append(path.split("/")[-2])
            start_response("204 No Content", [])
            return []
        if path.startswith("/api/v1/sessions/"):
            session = path.split("/")[-1]
            if method == "DELETE":
                if self.fail_cleanup:
                    return response({"error": "cleanup failed"}, "500 Failed")
                self.deleted.append(session)
                self.sessions.pop(session, None)
                return response({"deleted": True})
            self.polls[session] = self.polls.get(session, 0) + 1
            if self.polls[session] < 3 or self.sessions.get(session) == "never-saved":
                return response({"error": "not yet saved"}, "404 Not Found")
            return response({"turns": [{"query": self.sessions.get(session)}]})
        return response({"error": "unknown path"}, "404 Not Found")


def user_for(stub, workload="retrieval", query="question", **kwargs):
    config = Config(stub.host, workload, query, **kwargs)
    environment = Environment(user_classes=[NadirUser], host=stub.host, parsed_options=SimpleNamespace(
        host=stub.host, workload=config.workload, query=config.query, follow_up=config.follow_up,
        upload_file=str(config.upload_file or ""), top_k=config.top_k, timeout=config.timeout))
    environment.create_local_runner()
    # Locust's runner normally copies environment.host onto each user class.
    with patch.object(NadirUser, "host", stub.host):
        user = NadirUser(environment)
    user.on_start()
    return user


class ApiTests(unittest.TestCase):
    def setUp(self):
        self.stub = StubAPI()
        self.addCleanup(self.stub.close)

    def test_stream_framing_and_utf8(self):
        measured = consume_stream([b"id: 1", b"event: token", b"data: alpha", "data: café".encode(),
                                   b"", b"event: done", b"data: 1", b""], 0)
        self.assertEqual(measured.response_bytes, len("alpha\ncafé".encode()))
        self.assertIsNotNone(measured.first_token_ms)

    def test_stream_errors_and_missing_terminal(self):
        for event in ["generror", "resync", "token"]:
            with self.subTest(event=event), self.assertRaises(BenchmarkError):
                consume_stream([f"event: {event}".encode(), b"data: message", b""], 0)
        with self.assertRaises(BenchmarkError):
            consume_stream([b"event: done", b"data:", b""], 0)

    def test_config_requires_host_and_workload_inputs(self):
        valid = Config(self.stub.host, "retrieval", "question")
        valid.validate()
        invalid = [replace(valid, host=""), replace(valid, host="file:///tmp/data"),
                   replace(valid, query=""), replace(valid, workload="followup"),
                   replace(valid, workload="upload"), replace(valid, timeout=float("nan")),
                   replace(valid, timeout=0), replace(valid, top_k=0)]
        for config in invalid:
            with self.subTest(config=config), self.assertRaises(BenchmarkError):
                config.validate()

    def test_retrieval_bypasses_cache_and_cleans_only_own_sessions(self):
        user = user_for(self.stub)
        user.workload()
        requests = [json.loads(body) for method, path, body in self.stub.records if path == "/api/v1/turns"]
        self.assertTrue(requests[0]["skip_cache"])
        self.assertFalse(requests[0]["generate"])
        self.assertEqual(user.environment.stats.get("retrieval", "WORKFLOW").num_requests, 1)
        self.assertIn("existing-session", self.stub.sessions)
        self.assertTrue(self.stub.deleted)
        self.assertNotIn("existing-session", self.stub.deleted)

    def test_chat_records_stream_duration_and_ttft_without_retaining_answers(self):
        user = user_for(self.stub, "chat")
        user.workload()
        stats = user.environment.stats
        self.assertEqual(stats.get("chat", "WORKFLOW").num_requests, 1)
        self.assertEqual(stats.get("chat", "TTFT").num_requests, 1)
        self.assertGreater(stats.get("stream/chat", "GET").avg_response_time, 15)
        self.assertLess(stats.get("chat", "TTFT").avg_response_time, stats.get("chat", "WORKFLOW").avg_response_time)
        self.assertEqual(self.stub.cancelled, [])

    def test_immediate_answer_has_no_fake_ttft_sample(self):
        user = user_for(self.stub, "chat", "immediate")
        user.workload()
        self.assertEqual(user.environment.stats.get("chat", "WORKFLOW").num_failures, 0)
        self.assertEqual(user.environment.stats.get("chat", "TTFT").num_requests, 0)
        self.assertFalse(any(path.endswith("/events") for _, path, _ in self.stub.records))

    def test_mixed_alternates_chat_and_retrieval(self):
        user = user_for(self.stub, "mixed")
        user.workload()
        user.workload()
        self.assertEqual(user.environment.stats.get("chat", "WORKFLOW").num_requests, 1)
        self.assertEqual(user.environment.stats.get("retrieval", "WORKFLOW").num_requests, 1)

    def test_cache_requires_observed_repeat_hit(self):
        user = user_for(self.stub, "cache")
        user.workload()
        self.assertEqual(user.environment.stats.get("cache", "WORKFLOW").num_failures, 0)
        self.assertEqual(self.stub.cache_queries["question"], 2)
        missing = user_for(self.stub, "cache", "no-cache", timeout=0.05)
        missing.workload()
        self.assertEqual(missing.environment.stats.get("cache", "WORKFLOW").num_failures, 1)

    def test_followup_waits_for_history_and_observes_rewriting(self):
        user = user_for(self.stub, "followup", follow_up="followup question")
        user.workload()
        self.assertEqual(user.environment.stats.get("followup", "WORKFLOW").num_failures, 0)
        self.assertTrue(any(count >= 3 for count in self.stub.polls.values()))
        turns = [json.loads(body) for _, path, body in self.stub.records if path == "/api/v1/turns"]
        self.assertTrue(turns[1]["session_id"])
        self.assertTrue(all(turn["skip_cache"] for turn in turns))

    def test_followup_without_rewriting_is_a_failure(self):
        user = user_for(self.stub, "followup", follow_up="no-rewrite")
        user.workload()
        self.assertEqual(user.environment.stats.get("followup", "WORKFLOW").num_failures, 1)

    def test_followup_history_timeout_cleans_seed_session(self):
        user = user_for(self.stub, "followup", "never-saved", follow_up="followup", timeout=0.05)
        user.workload()
        self.assertEqual(user.environment.stats.get("followup", "WORKFLOW").num_failures, 1)
        self.assertEqual(user.environment.stats.get("history/wait", "GET").num_failures, 0)
        self.assertEqual(set(self.stub.sessions), {"existing-session"})

    def test_upload_has_unique_names_and_reports_indexing_errors(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "document.md"
            path.write_text("# A document")
            user = user_for(self.stub, "upload", upload_file=path)
            user.workload()
            user.workload()
            bodies = [body for method, route, body in self.stub.records if method == "POST" and route == "/api/v1/documents"]
            self.assertNotEqual(bodies[0].split(b"filename=")[1].split(b"\r\n")[0],
                                bodies[1].split(b"filename=")[1].split(b"\r\n")[0])
            self.stub.fail_upload = True
            user.workload()
            self.assertEqual(user.environment.stats.get("upload", "WORKFLOW").num_failures, 1)

    def test_errors_cancel_unfinished_turns_and_cleanup_history(self):
        for query in ["http-error", "api-error", "generate-error", "stream-error", "gap", "no-done", "no-answer", "timeout"]:
            with self.subTest(query=query):
                user = user_for(self.stub, "chat", query, timeout=0.05)
                user.workload()
                self.assertEqual(user.environment.stats.get("chat", "WORKFLOW").num_failures, 1)
                self.assertEqual(user.api.sessions, set())
                self.assertEqual(user.api.active_turns, set())
        self.assertTrue({"stream-error", "gap", "no-done", "timeout"}.issubset(self.stub.cancelled))
        self.assertIn("existing-session", self.stub.sessions)

    def test_cleanup_failure_is_not_silent(self):
        user = user_for(self.stub)
        self.stub.fail_cleanup = True
        user.workload()
        self.assertGreater(user.environment.stats.total.num_failures, 0)
        self.stub.fail_cleanup = False
        user.on_stop()
        self.assertEqual(user.api.sessions, set())


if __name__ == "__main__":
    unittest.main()
