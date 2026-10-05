import locust  # Cooperative subprocess waits let the stub serve child requests.

import csv
import json
from pathlib import Path
import socket
import signal
import subprocess
import sys
import tempfile
import unittest

import gevent
import requests

from test_api import StubAPI


ROOT = Path(__file__).resolve().parents[2]


class CliTests(unittest.TestCase):
    def setUp(self):
        self.stub = StubAPI()
        self.addCleanup(self.stub.close)
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)

    def run_suite(self, name, *arguments, host=True):
        output = self.root / name
        command = [sys.executable, "-m", "benchmark", "--output-dir", str(output),
                   "--run-time", "1s", "--stop-timeout", "1", "--timeout", "0.5", "--only-summary"]
        if host:
            command.extend(["--host", self.stub.host])
        command.extend(arguments)
        completed = subprocess.run(command, cwd=ROOT, capture_output=True, text=True, timeout=15)
        return completed, output

    def test_each_workload_runs_with_native_reports_and_metadata(self):
        upload = self.root / "input.md"
        upload.write_text("# Test document")
        for workload in ["retrieval", "chat", "upload", "mixed", "cache", "followup"]:
            with self.subTest(workload=workload):
                completed, output = self.run_suite(workload, "--workload", workload, "--query", "question",
                                                  "--follow-up", "followup question", "--upload-file", str(upload))
                self.assertEqual(completed.returncode, 0, completed.stdout + completed.stderr)
                self.assertTrue((output / "locust.html").is_file())
                with (output / "locust_stats.csv").open() as source:
                    rows = list(csv.DictReader(source))
                workflows = [row for row in rows if row["Type"] == "WORKFLOW"]
                self.assertTrue(workflows)
                self.assertGreater(sum(int(row["Request Count"]) for row in workflows), 0)
                metadata = json.loads((output / "metadata.json").read_text())["runs"][0]
                self.assertEqual(metadata["workload"], workload)
                self.assertEqual(metadata["corpus_before"]["count"], 1)
                self.assertIn("metrics_after", metadata)
                self.assertEqual(metadata["users"], 1)
                self.assertEqual(metadata["spawn_rate"], 1)
                self.assertEqual(metadata["status"], "passed")
                self.assertEqual(sum(row["completed"] for row in metadata["workflows"]),
                                 sum(int(row["Request Count"]) for row in workflows))
                self.assertAlmostEqual(metadata["completed_workflows_per_second"],
                                       sum(row["completed"] for row in metadata["workflows"]) / metadata["elapsed_seconds"])
        self.assertEqual(set(self.stub.sessions), {"existing-session"})

    def test_failed_workflows_and_cleanup_exit_nonzero(self):
        for query in ["api-error", "stream-error", "no-done", "timeout"]:
            with self.subTest(query=query):
                completed, output = self.run_suite(query, "--workload", "chat", "--query", query)
                self.assertNotEqual(completed.returncode, 0, completed.stdout + completed.stderr)
                self.assertTrue((output / "locust.html").is_file())
                metadata = json.loads((output / "metadata.json").read_text())["runs"][0]
                self.assertEqual(metadata["status"], "failed")
                self.assertEqual(metadata["completed_workflows_per_second"], 0)
                with (output / "locust_failures.csv").open() as source:
                    self.assertTrue(any(row["Method"] == "WORKFLOW" for row in csv.DictReader(source)))
        self.stub.fail_cleanup = True
        completed, _ = self.run_suite("cleanup-error", "--query", "question")
        self.assertNotEqual(completed.returncode, 0, completed.stdout + completed.stderr)

    def test_missing_inputs_and_unready_api_exit_nonzero(self):
        invalid = [("no-host", ["--query", "question"], False), ("no-query", [], True),
                   ("no-upload", ["--workload", "upload"], True),
                   ("no-followup", ["--workload", "followup", "--query", "question"], True)]
        for name, arguments, host in invalid:
            with self.subTest(name=name):
                completed, _ = self.run_suite(name, *arguments, host=host)
                self.assertNotEqual(completed.returncode, 0)
        self.stub.ready = False
        completed, _ = self.run_suite("not-ready", "--query", "question")
        self.assertNotEqual(completed.returncode, 0)
        self.assertFalse(any(path == "/api/v1/turns" for _, path, _ in self.stub.records))

    def test_help_exposes_native_and_workload_options(self):
        completed = subprocess.run([sys.executable, "-m", "benchmark", "--help"], cwd=ROOT,
                                   capture_output=True, text=True, timeout=15)
        self.assertEqual(completed.returncode, 0, completed.stderr)
        for option in ["--workload", "--query", "--users", "--follow-up", "--upload-file", "--top-k", "--timeout"]:
            self.assertIn(option, completed.stdout)

    def test_empty_run_and_interrupted_stream_exit_nonzero(self):
        completed, output = self.run_suite("empty", "--query", "question", "--users", "0")
        self.assertNotEqual(completed.returncode, 0, completed.stdout + completed.stderr)
        self.assertTrue((output / "locust.html").is_file())
        completed, _ = self.run_suite("interrupted", "--workload", "chat", "--query", "timeout",
                                      "--timeout", "5", "--stop-timeout", "0")
        self.assertNotEqual(completed.returncode, 0, completed.stdout + completed.stderr)
        self.assertIn("timeout", self.stub.cancelled)
        self.assertEqual(set(self.stub.sessions), {"existing-session"})

    def test_ui_exposes_options_and_preserves_multiple_run_metadata(self):
        with socket.socket() as socket_for_port:
            socket_for_port.bind(("127.0.0.1", 0))
            port = socket_for_port.getsockname()[1]
        output = self.root / "ui"
        process = subprocess.Popen([sys.executable, "-m", "benchmark", "--ui", "--host", self.stub.host,
                                    "--query", "question", "--web-port", str(port), "--stop-timeout", "0",
                                    "--output-dir", str(output)], cwd=ROOT, stdout=subprocess.PIPE,
                                   stderr=subprocess.PIPE, text=True)
        url = f"http://127.0.0.1:{port}"
        try:
            for _ in range(100):
                try:
                    page = requests.get(url, timeout=0.2)
                    if page.status_code == 200:
                        break
                except requests.RequestException:
                    pass
                if process.poll() is not None:
                    self.fail("Locust UI exited before startup: " + "".join(process.communicate()))
                gevent.sleep(0.05)
            else:
                self.fail("Locust UI did not start")
            for option in ["workload", "follow_up", "upload_file", "top_k"]:
                self.assertIn(option, page.text)
            for workload, query in [("retrieval", "question"), ("chat", "immediate")]:
                result = requests.post(url + "/swarm", data={"user_count": 1, "spawn_rate": 1,
                                       "host": self.stub.host, "workload": workload, "query": query}, timeout=2)
                self.assertTrue(result.json()["success"])
                for _ in range(100):
                    stats = requests.get(url + "/stats/requests", timeout=2).json()
                    if any(row["method"] == "WORKFLOW" and row["name"] == workload for row in stats["stats"]):
                        break
                    gevent.sleep(0.05)
                else:
                    self.fail(f"UI workload {workload} did not complete")
                self.assertTrue(requests.get(url + "/stop", timeout=2).json()["success"])
            result = requests.post(url + "/swarm", data={"user_count": 1, "spawn_rate": 1,
                                   "host": self.stub.host, "workload": "chat", "query": "timeout",
                                   "timeout": "5"}, timeout=2)
            self.assertTrue(result.json()["success"])
            for _ in range(100):
                if any(path == "/api/v1/turns/timeout/events" for _, path, _ in self.stub.records):
                    break
                gevent.sleep(0.05)
            else:
                self.fail("UI streaming turn did not start")
            process.send_signal(signal.SIGTERM)
            stdout, stderr = process.communicate(timeout=10)
            self.assertNotEqual(process.returncode, 0, stdout + stderr)
            self.assertTrue((output / "locust.html").is_file())
            runs = json.loads((output / "metadata.json").read_text())["runs"]
            self.assertEqual([run["workload"] for run in runs], ["retrieval", "chat", "chat"])
            self.assertEqual([run["status"] for run in runs], ["passed", "passed", "failed"])
            self.assertIn("timeout", self.stub.cancelled)
            self.assertEqual(set(self.stub.sessions), {"existing-session"})
        finally:
            if process.poll() is None:
                process.kill()
                process.communicate(timeout=5)


if __name__ == "__main__":
    unittest.main()
