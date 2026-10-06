"""Async public API collection, using HTTPX and httpx-sse framing."""

import asyncio
from urllib.parse import quote, urlsplit

import httpx
from httpx_sse import aconnect_sse


class CollectionError(Exception):
    pass


def validate_url(value):
    url = urlsplit(value)
    if url.scheme not in {"http", "https"} or not url.netloc or url.query or url.fragment or url.username or url.password:
        raise ValueError("endpoint must be an explicit HTTP(S) URL without credentials, query or fragment")
    return value.rstrip("/")


class ApiClient:
    def __init__(self, host, timeout=120, *, transport=None):
        self.host = validate_url(host)
        self.timeout = timeout
        self.client = httpx.AsyncClient(timeout=timeout, transport=transport, follow_redirects=False, trust_env=transport is None)
        self.sessions = set()
        self.active_turns = set()

    async def request(self, method, path, *, allowed=(200,), **kwargs):
        response = await self.client.request(method, self.host + path, **kwargs)
        if response.status_code not in allowed:
            raise CollectionError(f"{method} {path}: HTTP {response.status_code}")
        if response.status_code in {204, 404}:
            return {}
        result = response.json()
        if not isinstance(result, dict):
            raise CollectionError("API response must be an object")
        if method == "POST" and path == "/api/v1/turns":
            # Only this request mints a session; no existing ID is supplied.
            if isinstance(result.get("session_id"), str) and result["session_id"]:
                self.sessions.add(result["session_id"])
            if result.get("turn_id") and (result.get("streaming") or result.get("stream_url")):
                self.active_turns.add(result["turn_id"])
        if result.get("error") or result.get("generate_error"):
            raise CollectionError(str(result.get("error") or result["generate_error"]))
        return result

    async def provenance(self):
        async with asyncio.timeout(self.timeout):
            ready = await self.request("GET", "/api/v1/ready")
            if ready.get("ready") is not True:
                raise CollectionError("API is not ready")
            inventory = await self.request("GET", "/api/v1/documents")
        return {"host": self.host, "readiness": ready, "corpus_inventory": inventory}

    async def turn(self, query, top_k):
        async with asyncio.timeout(self.timeout):
            turn = await self.request("POST", "/api/v1/turns", json={
                "query": query, "top_k": top_k, "generate": True, "skip_cache": True,
            })
            if turn.get("from_cache"):
                raise CollectionError("API returned a cache hit despite skip_cache")
            url = turn.get("stream_url")
            if not url:
                answer = turn.get("answer")
                if not turn.get("has_answer") or not isinstance(answer, str) or not answer.strip():
                    raise CollectionError("generation returned neither a complete answer nor a stream")
                return turn, answer
            identifier = turn.get("turn_id")
            if not isinstance(identifier, str) or url != f"/api/v1/turns/{quote(identifier, safe='')}/events":
                raise CollectionError("unexpected generation stream URL")
            self.active_turns.add(identifier)
            tokens = []
            async with aconnect_sse(self.client, "GET", self.host + url) as stream:
                if stream.response.status_code != 200:
                    raise CollectionError(f"stream HTTP {stream.response.status_code}")
                async for event in stream.aiter_sse():
                    if event.event == "token":
                        tokens.append(event.data)
                    elif event.event in {"generror", "resync"}:
                        raise CollectionError(f"stream {event.event}: {event.data}")
                    elif event.event == "done":
                        answer = "".join(tokens)
                        if not answer.strip():
                            raise CollectionError("stream completed without an answer")
                        self.active_turns.discard(identifier)
                        return turn, answer
            raise CollectionError("generation stream ended without done")

    async def cleanup(self):
        errors = []
        for method, ids, prefix, suffix in (
            ("POST", self.active_turns, "/api/v1/turns/", "/cancel"),
            ("DELETE", self.sessions, "/api/v1/sessions/", ""),
        ):
            for identifier in sorted(ids.copy()):
                try:
                    async with asyncio.timeout(min(10, self.timeout)):
                        await self.request(method, prefix + quote(identifier, safe="") + suffix, allowed=(200, 204, 404))
                    ids.discard(identifier)
                except Exception as error:
                    errors.append(f"cleanup {identifier}: {error}")
        return errors

    async def close(self):
        await self.client.aclose()


def contexts(turn):
    chunks = turn.get("results")
    if chunks is None:
        chunks = []
    citations = turn.get("citations")
    if citations is None:
        citations = []
    for name, values in (("results", chunks), ("citations", citations)):
        if not isinstance(values, list) or not all(isinstance(x, dict) and isinstance(x.get("text"), str) for x in values):
            raise CollectionError(f"invalid {name} evidence")
    ranked = sorted(chunks, key=lambda x: x.get("retrieval_rank", 0))
    return [x["text"] for x in ranked], [x["text"] for x in citations]
