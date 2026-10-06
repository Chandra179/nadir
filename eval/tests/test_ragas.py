"""Actual framework metrics with deterministic OpenAI-compatible responses."""

import json
import math
import os
import unittest
from unittest.mock import patch

import httpx

from eval.judge import JudgeConfig, METRICS, RagasJudge


class RagasTests(unittest.IsolatedAsyncioTestCase):
    async def test_four_metrics_via_configured_hosted_endpoint(self):
        requests = []
        async def handler(request):
            body = json.loads(request.content)
            requests.append(body)
            self.assertEqual(str(request.url), "https://judge.example/custom/v1/chat/completions")
            self.assertEqual(request.headers["authorization"], "Bearer private-test-key")
            self.assertEqual(body["model"], "selected-judge")
            self.assertEqual(body["response_format"], {"type": "json_object"})
            schema = body["messages"][0]["content"]
            prompt = body["messages"][-1]["content"]
            if '"title": "StatementGeneratorOutput"' in schema:
                answer = {"statements": ["claim"]}
            elif '"title": "ClaimDecompositionOutput"' in schema:
                answer = {"claims": ["claim"]}
            elif '"title": "NLIStatementOutput"' in schema:
                answer = {"statements": [{"statement": "claim", "reason": "supported", "verdict": 1}]}
            elif '"title": "ContextPrecisionOutput"' in schema:
                answer = {"reason": "relevant", "verdict": 0 if '"context": "irrelevant"' in prompt else 1}
            elif '"title": "ContextRecallOutput"' in schema:
                answer = {"classifications": [{"statement": "claim", "reason": "supported", "attributed": 1}]}
            else:
                self.fail(f"unexpected framework schema: {schema}")
            return httpx.Response(200, json={"id": "stub", "object": "chat.completion", "created": 1,
                "model": "selected-judge", "choices": [{"index": 0,
                    "message": {"role": "assistant", "content": json.dumps(answer)}, "finish_reason": "stop"}]})
        sample = {"user_input": "question", "response": "claim", "reference": "claim",
                  "retrieved_contexts": ["full-first", "irrelevant", "full-third"],
                  "admitted_contexts": ["truncated admitted evidence"]}
        with patch.dict(os.environ, {"EVAL_TEST_KEY": "private-test-key"}):
            judge = RagasJudge(JudgeConfig("https://judge.example/custom/v1", "selected-judge", "EVAL_TEST_KEY"),
                               transport=httpx.MockTransport(handler))
            try:
                scores = {name: await judge.score(name, sample) for name in METRICS}
            finally:
                await judge.close()
        self.assertEqual(scores["faithfulness"], 1)
        self.assertEqual(scores["factual_correctness"], 1)
        self.assertAlmostEqual(scores["context_precision"], (1 + 2 / 3) / 2)
        self.assertEqual(scores["context_recall"], 1)
        nli_prompts = [b["messages"][-1]["content"] for b in requests
                       if '"title": "NLIStatementOutput"' in b["messages"][0]["content"]]
        self.assertIn("truncated admitted evidence", nli_prompts[0])
        self.assertNotIn("full-first", nli_prompts[0])
        self.assertEqual(len(requests), 10)

    async def test_framework_undefined_and_invalid_json(self):
        async def handler(request):
            return httpx.Response(200, json={"id": "stub", "object": "chat.completion", "created": 1,
                "model": "test", "choices": [{"index": 0,
                    "message": {"role": "assistant", "content": '{"statements": []}'}, "finish_reason": "stop"}]})
        sample = {"user_input": "q", "response": "a", "reference": "r", "admitted_contexts": ["context"], "retrieved_contexts": ["context"]}
        judge = RagasJudge(JudgeConfig("http://local/v1", "test"), transport=httpx.MockTransport(handler))
        try:
            self.assertTrue(math.isnan(await judge.score("faithfulness", sample)))
            with self.assertRaises(Exception):
                await judge.score("context_recall", sample)
        finally:
            await judge.close()
