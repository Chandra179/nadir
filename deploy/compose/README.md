# Compose deployment

The base file is the supported portable backend deployment for Linux, Windows
Docker Desktop, and macOS. It runs the Go API, Qdrant, and a CPU reranker. The
React dashboard runs separately with the local Node.js toolchain. The API
reaches host Ollama through `host.docker.internal:11434`.

```bash
docker compose -f deploy/compose/docker-compose.yml up -d --build
```

On Linux or Windows WSL2 with NVIDIA Container Toolkit, layer the GPU override
for the reranker:

```bash
docker compose -f deploy/compose/docker-compose.yml \
  -f deploy/compose/docker-compose.gpu.yml up -d --build
```

Do not apply the GPU override on macOS. Apple Silicon uses the CPU reranker;
host Ollama can still use its native Metal acceleration.

Start the dashboard locally in a second terminal:

```bash
cd web/dashboard
npm ci
npm run dev
```

Open `http://localhost:3000`. Vite proxies `/api/` and the SSE stream to the
Go API on `localhost:8100`. The repository does not build or run a frontend
container; a production deployment may serve the built `dist/` directory from
an independently managed static host.
