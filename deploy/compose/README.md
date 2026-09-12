# Compose deployment

The base file is the supported portable deployment for Linux, Windows Docker
Desktop, and macOS. It runs the React static dashboard, Go API, Qdrant, and a
CPU reranker. The API reaches host Ollama through
`host.docker.internal:11434`.

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
