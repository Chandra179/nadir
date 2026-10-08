"""Native Ragas generation with deterministic endpoints and supervised failures."""

from contextlib import contextmanager
from dataclasses import replace
import hashlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import signal
import shutil
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from unittest.mock import patch

from eval.__main__ import main, parser
from eval.dataset import load_dataset
from eval.generation import (GenerationConfig, ModelEndpoint, document_snapshot,
                             cache_namespace, generate, generation_summary, prepare_chunks, write_samples)
from eval.report import validate_report
from eval.runner import Run


ROW = {"user_input": "How does Redis cache data?", "reference": "Redis caches data in memory.",
       "reference_contexts": ["Redis caches data in memory."],
       "synthesizer_name": "single_hop_specific_query_synthesizer"}


def partial_worker(path, documents, config, events):
    write_samples(path, [ROW])
    # Simulate a stop between artifact replacements and during a temp write.
    (path / "questions.json").write_text("[]")
    (path / ".report-orphan").write_text("unfinished")
    events.put({"event": "sample", "count": 1})
    time.sleep(30)


def error_worker(path, documents, config, events):
    write_samples(path, [ROW])
    events.put({"event": "error", "message": "model failed gen-secret embed-secret"})


def interrupt_worker(path, documents, config, events):
    write_samples(path, [ROW])
    os.kill(os.getppid(), signal.SIGTERM)
    time.sleep(30)


def short_worker(path, documents, config, events):
    write_samples(path, [ROW])
    events.put({"event": "done", "distribution": []})


def crash_worker(path, documents, config, events):
    os._exit(7)


@contextmanager
def endpoints(mode="normal", delay=0):
    requests = []
    class Handler(BaseHTTPRequestHandler):
        protocol_version = "HTTP/1.1"
        def log_message(self, *args):
            pass

        def do_GET(self):
            result = {"models": [{"name": model, "size": 100, "size_vram": 90 if mode == "cpu" else 100,
                                  "digest": "stub", "context_length": 8192}
                                 for model in ("selected-generator", "selected-embedding")]}
            raw = json.dumps(result).encode()
            self.send_response(200)
            self.send_header("content-type", "application/json")
            self.send_header("content-length", str(len(raw)))
            self.end_headers()
            self.wfile.write(raw)

        def do_POST(self):
            body = json.loads(self.rfile.read(int(self.headers["content-length"])))
            requests.append((self.path, self.headers.get("authorization"), body))
            time.sleep(delay)
            if mode == "failure":
                self.send_response(503)
                self.send_header("connection", "close")
                self.end_headers()
                self.wfile.write(b'{"error":{"message":"model unavailable"}}')
                return
            if self.path.endswith("/embeddings"):
                values = body["input"]
                if isinstance(values, str):
                    values = [values]
                result = {"object": "list", "model": body["model"], "data": [
                    {"object": "embedding", "index": i, "embedding": [1.0, 0.0, 0.0]}
                    for i in range(len(values))], "usage": {"prompt_tokens": 1, "total_tokens": 1}}
            else:
                schema = body["messages"][0]["content"]
                prompt = body["messages"][-1]["content"]
                # Framework's final input follows its examples; use its schema
                # and inputs rather than replacing any framework prompts.
                data = json.JSONDecoder().raw_decode(prompt.rsplit("\ninput: ", 1)[1])[0]
                if '"title": "StringIO"' in schema:
                    answer = {"text": "Redis caches data in memory."}
                elif '"title": "ThemesAndConcepts"' in schema:
                    answer = {"output": ["Redis", "caching"]}
                elif '"title": "NEROutput"' in schema:
                    # Native overlap removes the most common entity as noise.
                    answer = {"entities": [] if mode == "empty" else ["Redis", "Cache"]}
                elif '"title": "Persona"' in schema:
                    answer = {"name": "Engineer", "role_description": "Studies Redis caching."}
                elif '"title": "PersonaThemesMapping"' in schema:
                    answer = {"mapping": {p["name"]: data["themes"] for p in data["personas"]}}
                elif '"title": "ConceptCombinations"' in schema:
                    answer = {"combinations": [[items[0] for items in data["lists_of_concepts"]]]}
                elif '"title": "GeneratedQueryAnswer"' in schema:
                    generated = sum('"title": "GeneratedQueryAnswer"' in r[2].get("messages", [{}])[0].get("content", "")
                                    for r in requests)
                    if mode == "partial" and generated > 1:
                        self.send_response(503)
                        self.send_header("connection", "close")
                        self.end_headers()
                        self.wfile.write(b'{"error":{"message":"sample generation failed"}}')
                        return
                    answer = {"query": f"How does Redis cache data in case {len(requests)}?",
                              "answer": "Redis caches data in memory."}
                elif '"title": "QuestionPotentialOutput"' in schema:
                    answer = {"score": 5}
                else:
                    raise AssertionError(f"Unexpected native schema: {schema}")
                result = {"id": "stub", "object": "chat.completion", "created": 1,
                          "model": body["model"], "choices": [{"index": 0, "finish_reason": "stop",
                              "message": {"role": "assistant", "content": "bad JSON" if mode == "malformed" else json.dumps(answer)}}],
                          "usage": {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}}
            raw = json.dumps(result).encode()
            self.send_response(200)
            self.send_header("content-type", "application/json")
            self.send_header("content-length", str(len(raw)))
            self.end_headers()
            try:
                self.wfile.write(raw)
            except BrokenPipeError:
                pass
    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        yield f"http://127.0.0.1:{server.server_port}", requests
    finally:
        server.shutdown()
        server.server_close()
        thread.join()


class GenerationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.docs = self.root / "docs"
        self.docs.mkdir()
        (self.docs / "a.md").write_text("# Redis\n\nRedis caches data in memory.\n", encoding="utf-8")

    def tearDown(self):
        self.temp.cleanup()

    def config(self, base="http://127.0.0.1:1/v1", **kwargs):
        return GenerationConfig(ModelEndpoint(base, "selected-generator", "GEN_TEST_KEY"),
            ModelEndpoint(base, "selected-embedding", "EMBED_TEST_KEY"), size=2,
            reasoning_effort="none", **kwargs)

    def checked_run(self, config, target=None):
        run = Run("generate", {}, self.root / "output")
        with patch.dict(os.environ, {"GEN_TEST_KEY": "gen-secret", "EMBED_TEST_KEY": "embed-secret"}):
            code = generate(run, document_snapshot(self.docs), config,
                            **({"worker_target": target} if target else {}))
        report = json.loads((run.path / "report.json").read_text())
        validate_report(report)
        for item in run.record["artifacts"]:
            self.assertEqual(item["sha256"], hashlib.sha256((run.path / item["path"]).read_bytes()).hexdigest())
        self.assertNotIn("gen-secret", json.dumps(report))
        self.assertNotIn("embed-secret", json.dumps(report))
        self.assertEqual(run.record["summary"]["review_status"], "unreviewed")
        return code, run

    def test_sorted_recursive_discovery_validation_and_preservation(self):
        (self.docs / "z").mkdir()
        (self.docs / "z" / "b.MD").write_bytes("Unicode ✓ café".encode())
        (self.docs / "ignore.txt").write_text("ignored")
        before = {p: p.read_bytes() for p in self.docs.rglob("*") if p.is_file()}
        documents = document_snapshot(self.docs)
        self.assertEqual([d["relative_path"] for d in documents], ["a.md", "z/b.MD"])
        chunks = prepare_chunks(documents)
        self.assertEqual(chunks[0].metadata["sha256"], documents[0]["sha256"])
        self.assertEqual(chunks[0].metadata["start_index"], 0)
        self.assertEqual(before, {p: p.read_bytes() for p in before})
        for raw, message in ((b"\xff", "UTF-8"), (b" \n", "blank")):
            (self.docs / "bad.md").write_bytes(raw)
            with self.assertRaisesRegex(ValueError, message):
                document_snapshot(self.docs)
        (self.docs / "bad.md").unlink()
        with self.assertRaisesRegex(ValueError, "existing directory"):
            document_snapshot(self.root / "absent")
        for p in self.docs.rglob("*.md"):
            p.unlink()
        (self.docs / "z/b.MD").unlink()
        with self.assertRaisesRegex(ValueError, "no Markdown"):
            document_snapshot(self.docs)

    def test_native_gpu_checks_and_cpu_failure_report(self):
        for mode in ("normal", "cpu"):
            with self.subTest(mode=mode), endpoints(mode) as (base, requests):
                config = replace(self.config(base + "/v1"), gpu_only=True)
                code, run = self.checked_run(config)
                gpu = run.record["provenance"]["gpu"]
                if mode == "normal":
                    self.assertEqual(code, 0)
                    self.assertGreater(gpu["completed_requests"]["generator"], 0)
                    self.assertGreater(gpu["completed_requests"]["embedding"], 0)
                    self.assertEqual(gpu["placement_checks"], 2 * sum(gpu["completed_requests"].values()))
                else:
                    self.assertEqual(code, 1)
                    self.assertTrue(any("not entirely on GPU" in error for error in run.record["errors"]))
                    self.assertEqual(requests, [])
                shutil.rmtree(run.path)

    def test_bounded_chunking_and_metadata(self):
        import tiktoken
        encoder = tiktoken.get_encoding("cl100k_base")
        (self.docs / "a.md").write_text("Redis cache information. " * 2500)
        docs = document_snapshot(self.docs)
        chunks = prepare_chunks(docs)
        self.assertGreater(len(chunks), 1)
        self.assertTrue(all(0 < len(encoder.encode(c.page_content)) <= 1000 for c in chunks))
        self.assertTrue(all(c.metadata["path"] == docs[0]["path"] for c in chunks))
        self.assertTrue(all(c.metadata["sha256"] == docs[0]["sha256"] for c in chunks))
        self.assertLess(chunks[1].metadata["start_index"], len(chunks[0].page_content))

    def test_native_response_cache_reuse_and_configuration_namespace(self):
        with endpoints() as (base, requests):
            config = replace(self.config(base + "/v1"), top_p=.8, cache_dir=self.root / "cache")
            code, run = self.checked_run(config)
            self.assertEqual(code, 0, run.record["errors"])
            first_request_count = len(requests)
            shutil.rmtree(run.path)
            requests.clear()
            code, run = self.checked_run(config)
            self.assertEqual(code, 0, run.record["errors"])
            # Framework scenario ordering may change; expensive extraction and
            # embeddings must be reused even if downstream prompts differ.
            self.assertLess(len(requests), first_request_count)
            for path, _, body in requests:
                self.assertNotIn("embeddings", path)
                schema = body["messages"][0]["content"]
                for title in ("StringIO", "ThemesAndConcepts", "NEROutput"):
                    self.assertNotIn(f'"title": "{title}"', schema)
            self.assertEqual(run.record["provenance"]["generation"]["response_cache"]["namespace"], cache_namespace(config))
            self.assertNotEqual(cache_namespace(config), cache_namespace(replace(config, top_p=.9)))
            self.assertNotEqual(cache_namespace(config), cache_namespace(replace(config, seed=1)))
            self.assertNotEqual(cache_namespace(config), cache_namespace(replace(config,
                generator=ModelEndpoint(base + "/v1", "different-model", "GEN_TEST_KEY"))))

    def test_input_defaults_and_invalid_configs(self):
        opts = parser().parse_args(["generate", "--documents", str(self.docs), "--generator-base-url", "http://a/v1",
            "--generator-model", "a", "--embedding-base-url", "http://b/v1", "--embedding-model", "b"])
        self.assertEqual((opts.size, opts.seed, opts.timeout, opts.run_timeout), (40, 42, 300, 7200))
        self.assertEqual(opts.generator_max_output_tokens, 4096)
        self.assertEqual(opts.workers, 1)
        for endpoint in (ModelEndpoint("http://user:password@a/v1", "x"), ModelEndpoint("http://a/v1?key=secret", "x"),
                         ModelEndpoint("ftp://a", "x"), ModelEndpoint("http://a", ""), ModelEndpoint("http://a", "x", "MISSING_KEY")):
            with self.assertRaises(ValueError):
                endpoint.validate()
        with patch.dict(os.environ, {"GEN_TEST_KEY": "g", "EMBED_TEST_KEY": "e"}):
            for options in ({"run_timeout": float("nan")}, {"timeout": 0}):
                with self.assertRaises(ValueError):
                    self.config(**options).validate()
            for options in ({"size": 0}, {"seed": -1}, {"max_workers": 0}, {"reasoning_effort": " "}, {"max_output_tokens": 0},
                            {"temperature": -1}, {"temperature": float("nan")}):
                with self.assertRaises(ValueError):
                    replace(self.config(), **options).validate()
            for value in (0, 1.1, float("nan")):
                with self.assertRaises(ValueError):
                    replace(self.config(), top_p=value).validate()
        (self.docs / "blank.md").write_text(" ")
        with patch("eval.__main__.generate") as worker, self.assertRaises(SystemExit):
            main(["generate", "--documents", str(self.docs), "--generator-base-url", "http://a",
                  "--generator-model", "a", "--embedding-base-url", "http://b", "--embedding-model", "b"])
        worker.assert_not_called()

    def test_full_native_pipeline_rounding_roles_and_coverage(self):
        (self.docs / "b.md").write_text("# Redis persistence\n\nRedis saves cache snapshots to disk.")
        with endpoints() as (base, requests):
            config = replace(self.config(base + "/generator/v1", run_timeout=60), temperature=.7, top_p=.8, max_workers=2,
                             embedding=ModelEndpoint(base + "/embedding/v1", "selected-embedding", "EMBED_TEST_KEY"))
            code, run = self.checked_run(config)
        self.assertEqual(code, 0, run.record["errors"])
        self.assertEqual(run.record["provenance"]["generation"]["max_workers"], 2)
        self.assertEqual(run.record["summary"]["generated"], 3)  # native ceil(2/3) per type
        self.assertEqual(len(run.record["summary"]["question_types"]), 3)
        self.assertFalse(run.record["summary"]["uncovered_documents"])
        self.assertFalse(run.record["summary"]["unmapped_samples"])
        self.assertEqual(len(load_dataset(run.path / "questions.json")), 3)
        for path, auth, body in requests:
            embedding = path.endswith("embeddings")
            self.assertEqual(path, "/embedding/v1/embeddings" if embedding else "/generator/v1/chat/completions")
            self.assertEqual(auth, "Bearer embed-secret" if embedding else "Bearer gen-secret")
            self.assertEqual(body["model"], "selected-embedding" if embedding else "selected-generator")
            if not embedding:
                self.assertEqual(body["top_p"], .8)
                self.assertEqual(body["seed"], 42)
                self.assertEqual(body["reasoning_effort"], "none")
                self.assertEqual(body["max_tokens"], 4096)
                self.assertEqual(body["temperature"], .7)
        with self.assertRaises(FileExistsError):
            Run("generate", {}, run.path)

    def test_single_document_uses_only_eligible_question_type(self):
        with endpoints() as (base, _):
            code, run = self.checked_run(self.config(base + "/v1", run_timeout=60))
        self.assertEqual(code, 0, run.record["errors"])
        self.assertEqual(len(run.record["summary"]["question_types"]), 1)
        self.assertEqual(run.record["summary"]["generated"], 2)

    def test_native_malformed_and_model_failures_leave_valid_reports(self):
        for mode in ("malformed", "failure"):
            with self.subTest(mode=mode), endpoints(mode) as (base, _):
                code, run = self.checked_run(self.config(base + "/v1", run_timeout=60, timeout=1))
                self.assertEqual(code, 1)
                self.assertEqual(run.record["status"], "failed")
                self.assertTrue(run.record["errors"])
                self.assertEqual(json.loads((run.path / "questions.json").read_text()), [])
            for p in (self.root / "output").iterdir():
                p.unlink()
            (self.root / "output").rmdir()

    def test_empty_entities_do_not_produce_a_successful_empty_draft(self):
        with endpoints("empty") as (base, _):
            code, run = self.checked_run(self.config(base + "/v1", run_timeout=60))
        self.assertEqual(code, 1)
        self.assertTrue(run.record["errors"])
        self.assertEqual(run.record["summary"]["generated"], 0)

    def test_request_timeout_stops_native_generation(self):
        with endpoints(delay=.2) as (base, _):
            code, run = self.checked_run(self.config(base + "/v1", timeout=.02, run_timeout=60))
        self.assertEqual(code, 1)
        self.assertTrue(run.record["errors"])

    def test_native_sample_failure_keeps_completed_callback_sample(self):
        with endpoints("partial") as (base, _):
            code, run = self.checked_run(self.config(base + "/v1", run_timeout=60))
        self.assertEqual(code, 1)
        self.assertEqual(run.record["summary"]["generated"], 1)
        self.assertEqual(len(load_dataset(run.path / "questions.json")), 1)

    def test_deadline_and_partial_checkpoint(self):
        code, run = self.checked_run(self.config(run_timeout=1), partial_worker)
        self.assertEqual(code, 1)
        self.assertIn("overall deadline", " ".join(run.record["errors"]))
        self.assertEqual(len(load_dataset(run.path / "questions.json")), 1)
        self.assertFalse(list(run.path.glob(".report-*")))

    def test_interrupt_preserves_checkpoint_and_exits_130(self):
        code, run = self.checked_run(self.config(run_timeout=10), interrupt_worker)
        self.assertEqual(code, 130)
        self.assertEqual(run.record["status"], "interrupted")
        self.assertEqual(len(load_dataset(run.path / "questions.json")), 1)

    def test_insufficient_samples_and_unexpected_worker_exit_fail(self):
        for target, expected in ((short_worker, "fewer samples"), (crash_worker, "exit 7")):
            with self.subTest(target=target.__name__):
                code, run = self.checked_run(self.config(run_timeout=10), target)
                self.assertEqual(code, 1)
                self.assertIn(expected, " ".join(run.record["errors"]))
            for p in (self.root / "output").iterdir():
                p.unlink()
            (self.root / "output").rmdir()

    def test_error_partial_secret_redaction_and_duplicates(self):
        code, run = self.checked_run(self.config(), error_worker)
        self.assertEqual(code, 1)
        self.assertIn("[redacted]", run.record["errors"][0])
        rows = [ROW, {**ROW, "user_input": " HOW does Redis cache data? "}]
        summary, _ = generation_summary(document_snapshot(self.docs), rows, 2)
        self.assertEqual(summary["duplicate_questions"], [["generated-0001", "generated-0002"]])
        summary, _ = generation_summary(document_snapshot(self.docs), [{**ROW, "reference_contexts": ["absent passage"]}], 1)
        self.assertEqual(summary["uncovered_documents"], ["a.md"])
        normalized = "Redis  caches data in memory."
        summary, _ = generation_summary(document_snapshot(self.docs), [{**ROW, "reference_contexts": ["<1-hop>\n\n" + normalized]}], 1,
            [{"source_path": "a.md", "sha256": hashlib.sha256(normalized.encode()).hexdigest()}])
        self.assertFalse(summary["uncovered_documents"])
        self.assertFalse(summary["unmapped_samples"])
        for row in ({**ROW, "reference": ""}, {**ROW, "reference_contexts": []}, {**ROW, "synthesizer_name": None}):
            with self.assertRaises(ValueError):
                write_samples(run.path, [row])

    def test_cli_help_and_validation_without_generation_dependencies(self):
        blocker = "import sys; from importlib.abc import MetaPathFinder\nclass Block(MetaPathFinder):\n def find_spec(self, name, *args):\n  if name.split('.')[0] in {'ragas', 'langchain_text_splitters', 'langchain_core'}: raise ImportError('generation dependency blocked')\nsys.meta_path.insert(0, Block())\n"
        code = blocker + "from eval.__main__ import parser; parser().parse_args(['generate','--help'])"
        result = subprocess.run([sys.executable, "-c", code], capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        result = subprocess.run([sys.executable, "-c", blocker + "import eval.generation; assert 'ragas' not in sys.modules; assert 'langchain_text_splitters' not in sys.modules"], capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        write_samples(self.root, [ROW])
        code = blocker + f"from eval.__main__ import main; raise SystemExit(main(['validate', '--dataset', {str(self.root / 'questions.json')!r}]))"
        result = subprocess.run([sys.executable, "-c", code], capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Valid dataset: 1 samples", result.stdout)
