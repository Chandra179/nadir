"""Placement gates must prevent inference when GPU placement is unknown."""

import json
import queue
import unittest

import httpx

from eval.generation import ModelEndpoint
from eval.ollama import GPUOnlyTransport


class GPUOnlyTests(unittest.IsolatedAsyncioTestCase):
    async def exercise(self, *, role="generator", initial=None, loaded=None, after=None):
        full = {"name": "chosen", "size": 100, "size_vram": 100, "context_length": 8192}
        state = list(initial if initial is not None else [full])
        requests = []
        events = queue.Queue()

        async def handler(request):
            requests.append((request.url.path, json.loads(request.content) if request.content else None))
            if request.url.path == "/api/ps":
                return httpx.Response(200, json={"models": state})
            if request.url.path in {"/api/generate", "/api/embed"}:
                state[:] = loaded if loaded is not None else [full]
                return httpx.Response(200, json={})
            if after is not None:
                state[:] = after
            return httpx.Response(200, json={"choices": []})

        transport = GPUOnlyTransport(ModelEndpoint("http://ollama/v1", "chosen"), role, 1, events,
                                     transport=httpx.MockTransport(handler))
        try:
            async with httpx.AsyncClient(transport=transport) as client:
                await client.post("http://ollama/v1/chat/completions", json={"model": "chosen"})
        except RuntimeError as error:
            return error, requests, events
        return None, requests, events

    async def test_verified_before_and_after_inference(self):
        error, requests, events = await self.exercise()
        self.assertIsNone(error)
        self.assertEqual([p for p, _ in requests], ["/api/ps", "/v1/chat/completions", "/api/ps"])
        self.assertIsNone(events.get()["request_seconds"])
        self.assertGreaterEqual(events.get()["request_seconds"], 0)

    async def test_empty_preload_after_model_eviction(self):
        for role, path in (("generator", "/api/generate"), ("embedding", "/api/embed")):
            error, requests, events = await self.exercise(role=role, initial=[])
            self.assertIsNone(error)
            self.assertEqual(requests[1][0], path)
            self.assertNotIn("prompt", requests[1][1])
            if role == "embedding":
                self.assertEqual(requests[1][1]["input"], [])
            self.assertEqual(events.qsize(), 2)

    async def test_cpu_or_unknown_placement_blocks_inference(self):
        for model in ({"name": "chosen", "size": 100, "size_vram": 90},
                      {"name": "chosen", "size": 100},
                      {"name": "chosen", "size": 0, "size_vram": 0}):
            error, requests, events = await self.exercise(initial=[model])
            self.assertRegex(str(error), "not entirely on GPU")
            self.assertEqual(len(requests), 1)
            self.assertEqual(events.get()["event"], "error")

    async def test_failed_preload_blocks_inference(self):
        error, requests, _ = await self.exercise(initial=[], loaded=[])
        self.assertRegex(str(error), "not loaded")
        self.assertNotIn("/v1/chat/completions", [p for p, _ in requests])

    async def test_placement_loss_after_response_fails(self):
        error, _, events = await self.exercise(after=[{"name": "chosen", "size": 100, "size_vram": 90}])
        self.assertRegex(str(error), "not entirely on GPU")
        self.assertEqual(events.qsize(), 2)
