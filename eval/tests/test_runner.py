import asyncio
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import AsyncMock, patch

from eval.__main__ import main
from eval.dataset import load_capture, load_dataset
from eval.judge import JudgeConfig, METRICS
from eval.report import validate_report
from eval.runner import Run, collect, score


class FakeApi:
    instances = []
    def __init__(self, host, timeout):
        self.cleaned = False
        self.instances.append(self)
    async def provenance(self):
        return {"corpus_inventory": {"documents": ["source"]}}
    async def turn(self, query, top_k):
        if query == "fail":
            raise RuntimeError("generation failure")
        if query == "interrupt":
            raise asyncio.CancelledError()
        return {"results": [{"text": "full", "retrieval_rank": 1}],
                "citations": [{"text": "truncated", "truncated": True}]}, "answer"
    async def cleanup(self):
        self.cleaned = True
        return []
    async def close(self):
        pass


class FakeJudge:
    value = .2
    async def score(self, name, sample):
        if sample["user_input"] == "judge-failure":
            raise RuntimeError("bad judge JSON")
        if sample["user_input"] == "judge-timeout":
            await asyncio.sleep(1)
        if sample["user_input"] == "judge-interrupt":
            raise asyncio.CancelledError()
        return self.value
    def __init__(self, config):
        pass
    async def close(self):
        pass


class RunnerTests(unittest.IsolatedAsyncioTestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.dataset = self.root / "dataset.json"
        self.config = JudgeConfig("http://judge/v1", "test-model", timeout=.02)

    def inputs(self, *queries):
        self.dataset.write_text(json.dumps([{"user_input": query, "reference": "reference"} for query in queries]))

    async def capture(self, *queries):
        self.inputs(*queries)
        run = Run("collect", {}, self.root / "collect")
        capture = await collect(run, self.dataset, "http://unused", api_factory=FakeApi)
        run.finish()
        return run, capture

    async def test_rescore_without_api_and_immutable_capture(self):
        collected, capture = await self.capture("question")
        before = capture.read_bytes()
        for index in (1, 2):
            run = Run("score", {}, self.root / f"score{index}")
            with patch("eval.runner.ApiClient", side_effect=AssertionError("no API during scoring")):
                await score(run, capture, self.config, judge_factory=FakeJudge)
            run.finish()
            self.assertEqual(run.record["status"], "completed")
            self.assertEqual(run.record["summary"]["scored_samples"], 1)
            self.assertEqual(run.record["provenance"]["capture"]["sha256"], hashlib.sha256(before).hexdigest())
            for metric in METRICS:
                self.assertEqual(run.record["summary"]["metrics"][metric]["mean"], .2)
            validate_report(json.loads((run.path / "report.json").read_text()))
            for a in run.record["artifacts"]:
                self.assertEqual(hashlib.sha256((run.path / a["path"]).read_bytes()).hexdigest(), a["sha256"])
        self.assertEqual(capture.read_bytes(), before)
        with self.assertRaises(FileExistsError):
            Run("score", {}, self.root / "score1")

    async def test_partial_collect_and_repetitions(self):
        self.inputs("good", "fail")
        run = Run("run", {}, self.root / "run")
        capture = await collect(run, self.dataset, "http://unused", repetitions=2, api_factory=FakeApi)
        self.assertEqual(len(load_capture(capture)["samples"]), 4)
        await score(run, capture, self.config, judge_factory=FakeJudge)
        run.finish()
        self.assertEqual(run.record["status"], "failed")
        self.assertEqual(run.record["summary"]["scored_samples"], 2)
        self.assertTrue(FakeApi.instances[-1].cleaned)

    async def test_judge_failures_deadlines_and_nan(self):
        for query in ("judge-failure", "judge-timeout", "question"):
            with self.subTest(query=query), tempfile.TemporaryDirectory() as folder:
                capture = Path(folder) / "capture.json"
                capture.write_text(json.dumps({"schema_version": 1, "producer": "nadir-eval", "provenance": {}, "samples": [{
                    "sample_id": "s", "user_input": query, "reference": "ref", "response": "answer",
                    "retrieved_contexts": ["full"], "admitted_contexts": ["short"], "status": "completed", "errors": []}]}))
                run = Run("score", {}, Path(folder) / "output")
                with patch.object(FakeJudge, "value", float("nan")):
                    await score(run, capture, self.config, judge_factory=FakeJudge)
                run.finish()
                self.assertEqual(run.record["status"], "failed")
                self.assertIn("no samples produced a finite score", run.record["errors"])
                expected = "undefined" if query == "question" else "failed"
                for metric in METRICS:
                    self.assertEqual(run.record["results"]["samples"][0]["metrics"][metric]["status"], expected)

    async def test_empty_contexts_do_not_invent_scores(self):
        _, capture = await self.capture("question")
        data = json.loads(capture.read_text())
        data["samples"][0].update(retrieved_contexts=[], admitted_contexts=[])
        capture.write_text(json.dumps(data))
        run = Run("score", {}, self.root / "score")
        await score(run, capture, self.config, judge_factory=FakeJudge)
        run.finish()
        self.assertEqual(run.record["status"], "completed")
        self.assertEqual(run.record["summary"]["metrics"]["faithfulness"]["undefined"], 1)
        self.assertEqual(run.record["summary"]["metrics"]["factual_correctness"]["scored"], 1)

    async def test_interruption_saves_capture_and_scores(self):
        self.inputs("good", "interrupt")
        run = Run("collect", {}, self.root / "collect")
        with self.assertRaises(asyncio.CancelledError):
            await collect(run, self.dataset, "http://unused", api_factory=FakeApi)
        run.finish(True)
        self.assertTrue((run.path / "capture.json").exists())
        self.assertTrue(FakeApi.instances[-1].cleaned)
        data = load_capture(run.path / "capture.json")
        data["samples"][0]["user_input"] = "judge-interrupt"
        capture = self.root / "interrupted-capture.json"
        capture.write_text(json.dumps(data))
        scored = Run("score", {}, self.root / "score")
        with self.assertRaises(asyncio.CancelledError):
            await score(scored, capture, self.config, judge_factory=FakeJudge)
        scored.finish(True)
        self.assertTrue((scored.path / "scores.csv").exists())
        self.assertEqual(scored.record["status"], "interrupted")


class InputTests(unittest.TestCase):
    def test_dataset_rejects_legacy_empty_invalid_and_duplicate(self):
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "data.json"
            good = {"user_input": "question", "reference": "reference"}
            for value in ({"schema_version": 3, "queries": []}, [], [good, {**good, "id": "sample-1"}],
                          [{**good, "reference": " "}], [{**good, "unknown": True}], [{**good, "id": 2}]):
                path.write_text(json.dumps(value))
                with self.assertRaises(ValueError):
                    load_dataset(path)
            path.write_text(json.dumps([good]))
            self.assertEqual(load_dataset(path)[0]["id"], "sample-1")

    def test_judge_credentials_and_endpoint(self):
        with patch.dict(os.environ, {"JUDGE_TEST_KEY": "private"}):
            config = JudgeConfig("https://host/v1", "explicit", "JUDGE_TEST_KEY")
            config.validate()
            self.assertNotIn("private", json.dumps(config.provenance()))
        with self.assertRaises(ValueError):
            JudgeConfig("http://local/v1", "model", "NONEXISTENT_NADIR_JUDGE_KEY").validate()
        with self.assertRaises(ValueError):
            JudgeConfig("http://local/v1", " ").validate()
        with self.assertRaises(ValueError):
            JudgeConfig("http://local/v1", "model", reasoning_effort=" ").validate()
        config = JudgeConfig("http://local/v1", "qwen3.5:4b", reasoning_effort="none")
        config.validate()
        self.assertEqual(config.provenance()["reasoning_effort"], "none")

    def test_cli_judge_failure_and_interruption_exit_codes(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            capture = root / "capture.json"
            capture.write_text(json.dumps({"schema_version": 1, "producer": "nadir-eval", "provenance": {}, "samples": []}))
            for name, error, code, status in (("failure", RuntimeError("judge failed"), 1, "failed"),
                                               ("interrupted", asyncio.CancelledError(), 130, "interrupted")):
                with self.subTest(name=name), patch("eval.__main__.score", new=AsyncMock(side_effect=error)):
                    result = main(["score", "--capture", str(capture), "--judge-base-url", "http://judge/v1",
                                   "--judge-model", "explicit", "--output-dir", str(root / name)])
                self.assertEqual(result, code)
                report = json.loads((root / name / "report.json").read_text())
                self.assertEqual(report["runs"][0]["status"], status)
                self.assertTrue(report["runs"][0]["errors"])

    def test_cli_failure_nonzero_with_report(self):
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder)
            (path / "data.json").write_text('[{"user_input":"question","reference":"reference"}]')
            result = subprocess.run([sys.executable, "-m", "eval", "collect", "--dataset", str(path / "data.json"),
                                     "--host", "http://127.0.0.1:1", "--timeout", ".05", "--output-dir", str(path / "output")],
                                    capture_output=True, text=True)
            self.assertEqual(result.returncode, 1, result.stderr)
            report = json.loads((path / "output/report.json").read_text())
            self.assertEqual(report["runs"][0]["status"], "failed")
