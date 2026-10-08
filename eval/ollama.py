"""Opt-in GPU placement checks around native model requests."""

import time

import httpx


class GPUOnlyTransport(httpx.AsyncBaseTransport):
    def __init__(self, endpoint, role, timeout, events, *, transport=None):
        url = httpx.URL(endpoint.base_url)
        self.origin = str(url.copy_with(path="", query=None))
        self.model = endpoint.model
        self.role = role
        self.timeout = timeout
        self.events = events
        self.test_transport = transport
        self.upstream = transport or httpx.AsyncHTTPTransport(
            trust_env=False, limits=httpx.Limits(max_keepalive_connections=0))

    async def inventory(self, client):
        response = await client.get("/api/ps")
        response.raise_for_status()
        models = response.json().get("models")
        if not isinstance(models, list):
            raise RuntimeError("invalid Ollama model inventory")
        return models

    def verify(self, models):
        if not any(model.get("name") == self.model or model.get("model") == self.model for model in models):
            self.fail(f"GPU check: {self.model} is not loaded")
        for model in models:
            size, vram = model.get("size"), model.get("size_vram")
            if type(size) is not int or type(vram) is not int or size <= 0 or vram < size:
                self.fail(f"GPU check: {model.get('name')} is not entirely on GPU")

    def fail(self, message):
        # SDK retries wrap transport errors; retain the placement reason itself.
        self.events.put({"event": "error", "message": message})
        raise RuntimeError(message)

    def record(self, models, elapsed=None):
        self.events.put({"event": "gpu", "role": self.role, "models": models,
                         "request_seconds": elapsed})

    async def handle_async_request(self, request):
        # Empty native requests load weights without evaluating a user prompt.
        # A fresh client is needed because Ragas stages use separate event loops.
        headers = {"authorization": request.headers["authorization"]} if "authorization" in request.headers else {}
        async with httpx.AsyncClient(base_url=self.origin, headers=headers, timeout=self.timeout,
                                     trust_env=False, transport=self.test_transport,
                                     limits=httpx.Limits(max_keepalive_connections=0)) as client:
            models = await self.inventory(client)
            if not any(model.get("name") == self.model or model.get("model") == self.model for model in models):
                body = {"model": self.model, "keep_alive": "30m"}
                if self.role == "embedding":
                    body["input"] = []
                    path = "/api/embed"
                else:
                    body["stream"] = False
                    path = "/api/generate"
                response = await client.post(path, json=body)
                response.raise_for_status()
                models = await self.inventory(client)
            self.verify(models)
            self.record(models)
            started = time.monotonic()
            response = await self.upstream.handle_async_request(request)
            try:
                await response.aread()
                models = await self.inventory(client)
                self.verify(models)
                self.record(models, time.monotonic() - started)
            except BaseException:
                await response.aclose()
                raise
            return response

    async def aclose(self):
        await self.upstream.aclose()
