# Podman Compose deployment

The base file is the supported portable backend deployment: rootless Podman on
Linux, or Podman inside a `podman machine` VM on macOS and Windows. It runs the
Go API, Qdrant, and a CPU reranker. The React dashboard runs separately with
the local Node.js toolchain. The API reaches host Ollama through
`host.containers.internal:11434`, which Podman injects automatically.

One-time host setup (engine, rootless IDs, newest `podman-compose` provider,
NVIDIA CDI):

```bash
./scripts/setup_podman_host.sh
```

The API container's healthcheck uses `/api/v1/ready`, so it stays unhealthy
until Qdrant, the configured Ollama embedding model, and the enabled reranker
are usable. `/api/v1/health` remains the dependency-free liveness endpoint.

```bash
podman compose -f deploy/compose/compose.yaml up -d --build
```

The base stack starts without a source directory and accepts file uploads
using the dashboard's paperclip. For optional directory ingestion, provide
an existing absolute host path and layer the source override:

```bash
DOCUMENTS_DIR=/absolute/path/to/documents podman compose \
  -f deploy/compose/compose.yaml -f deploy/compose/compose.sources.yaml up -d --build
```

The override mounts that directory at `/app/source` and sets `DOCUMENTS_PATHS`
accordingly. Add it alongside the GPU override when both are needed. The
sample corpus has been removed; evaluation runs require explicit source
documents and matching question sets.

On Linux (or Windows WSL2) with an NVIDIA GPU, layer the GPU override for the
reranker after generating the CDI spec:

```bash
sudo nvidia-ctk cdi generate --output=/etc/cdi/nvidia.yaml
podman compose -f deploy/compose/compose.yaml \
  -f deploy/compose/compose.gpu.yaml up -d --build
```

Do not apply the GPU override on macOS. Apple Silicon uses the CPU reranker;
host Ollama can still use its native Metal acceleration.

Start the dashboard locally in a second terminal:

```bash
cd web/dashboard
npm ci
npm run dev
```

Open `http://localhost:3002`. Set `DASHBOARD_PORT` to choose another available
port. Vite proxies `/api/` and the SSE stream to the Go API on `localhost:8100`.
The repository does not build or run a frontend
container; a production deployment may serve the built `dist/` directory from
an independently managed static host.
