import asyncio
import json
import unittest

import httpx

from eval.api import ApiClient, CollectionError, contexts, validate_url


class DelayedStream(httpx.AsyncByteStream):
    async def __aiter__(self):
        await asyncio.sleep(1)
        yield b"event: done\n\n"


class ApiTests(unittest.IsolatedAsyncioTestCase):
    def client(self, mode="immediate", *, timeout=1):
        calls = []
        async def handle(request):
            calls.append((request.method, request.url.path))
            if request.url.path == "/api/v1/ready":
                return httpx.Response(200, json={"ready": True, "checks": {"generator": {"model": "answer-model"}}})
            if request.url.path == "/api/v1/documents":
                return httpx.Response(200, json={"documents": [{"file_path": "a.md", "source_sha": "abc"}]})
            if request.url.path == "/api/v1/turns":
                body = json.loads(request.content)
                self.assertTrue(body["skip_cache"])
                self.assertTrue(body["generate"])
                self.assertNotIn("session_id", body)
                response = {"session_id": "owned", "results": [], "citations": []}
                if mode == "immediate":
                    response.update(answer="café", has_answer=True)
                else:
                    response.update(turn_id="turn", streaming=True, stream_url="/api/v1/turns/turn/events")
                if mode == "error":
                    response["generate_error"] = "generation failed"
                if mode == "cache":
                    response["from_cache"] = True
                return httpx.Response(200, json=response)
            if request.url.path.endswith("/events"):
                if mode == "timeout":
                    return httpx.Response(200, headers={"content-type": "text/event-stream"}, stream=DelayedStream())
                messages = {
                    "stream": "event: token\ndata: café\ndata: 世界\n\nevent: done\ndata: ok\n\n",
                    "gap": "event: resync\ndata: missed\n\n",
                    "generation": "event: generror\ndata: failed\n\n",
                    "missing": "event: token\ndata: a\n\n",
                    "empty": "event: done\n\n",
                }
                return httpx.Response(200, headers={"content-type": "text/event-stream"}, content=messages.get(mode, ""))
            return httpx.Response(204)
        return ApiClient("http://nadir", timeout, transport=httpx.MockTransport(handle)), calls

    async def test_immediate_provenance_and_owned_cleanup(self):
        api, calls = self.client()
        provenance = await api.provenance()
        self.assertEqual(provenance["readiness"]["checks"]["generator"]["model"], "answer-model")
        _, response = await api.turn("question", 5)
        self.assertEqual(response, "café")
        self.assertEqual(await api.cleanup(), [])
        self.assertEqual([x for x in calls if x[0] == "DELETE"], [("DELETE", "/api/v1/sessions/owned")])
        await api.close()

    async def test_multiline_utf8(self):
        api, calls = self.client("stream")
        _, response = await api.turn("question", 5)
        self.assertEqual(response, "café\n世界")
        await api.cleanup()
        self.assertNotIn(("POST", "/api/v1/turns/turn/cancel"), calls)
        await api.close()

    async def test_failures_cancel_and_cleanup(self):
        for mode in ("gap", "generation", "missing", "empty", "error", "cache", "timeout"):
            with self.subTest(mode=mode):
                api, calls = self.client(mode, timeout=.02)
                with self.assertRaises((CollectionError, TimeoutError)):
                    await api.turn("question", 5)
                await api.cleanup()
                self.assertIn(("POST", "/api/v1/turns/turn/cancel"), calls)
                self.assertIn(("DELETE", "/api/v1/sessions/owned"), calls)
                await api.close()

    async def test_http_api_and_ready_errors(self):
        for response in (httpx.Response(500), httpx.Response(200, json={"error": "oops"}),
                         httpx.Response(200, json={"ready": False}), httpx.Response(200, json=[])):
            api = ApiClient("http://nadir", transport=httpx.MockTransport(lambda request: response))
            with self.assertRaises(CollectionError):
                await api.provenance()
            await api.close()

    def test_ranked_chunks_and_truncated_citations(self):
        retrieved, admitted = contexts({"results": [{"text": "second", "retrieval_rank": 2},
                                                     {"text": "full first", "retrieval_rank": 1}],
                                        "citations": [{"text": "short first", "truncated": True}]})
        self.assertEqual(retrieved, ["full first", "second"])
        self.assertEqual(admitted, ["short first"])
        self.assertEqual(contexts({"results": []}), ([], []))
        with self.assertRaises(CollectionError):
            contexts({"results": [{"text": None}]})

    def test_explicit_urls(self):
        for url in ("", "localhost", "file:///tmp/a", "http://a?b=1", "http://user:secret@host"):
            with self.assertRaises(ValueError):
                validate_url(url)
