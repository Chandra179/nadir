# nadir

Semantic document search engine. Ingests text files, chunks + embeds them locally, stores in Qdrant, serves hybrid semantic+keyword search over HTTP, with optional cross-encoder reranking and LLM answer generation.

## Run it (Linux or macOS)

Install the tools below, then run these steps from the repository root. The
two terminals stay open.

```bash
# 1. Models (Ollama must be installed and running). These are the defaults in
#    config.yaml and can be changed, see "Choose your models".
ollama pull embeddinggemma-300m-q8
ollama pull qwen3.5:4b

# 2. Terminal A: Qdrant + API on http://localhost:8100
./scripts/local.sh

# 3. Terminal B: dashboard on http://localhost:3002
cd web/dashboard && npm ci && npm run dev
```

Open `http://localhost:3002`, click **+** → **Import Markdown or PDF**, choose a
`.md` file (try `eval/samples/cache.md`), then ask a question about it.

| | Linux | macOS |
|---|---|---|
| Podman | `./scripts/setup_podman_host.sh` (Ubuntu 24.04+), or your package manager | `brew install podman podman-compose`, then `podman machine init && podman machine start` |
| Go 1.27+, Python 3.12+, Node 24 LTS | Package manager or the official installers | `brew install go python@3.12 node@24` |
| Ollama | [ollama.com](https://ollama.com) | [ollama.com](https://ollama.com) |

The macOS column and `local.sh` on a Mac are untested here; Podman runs inside a
`podman machine` VM there, see the [Compose guide](deploy/compose/README.md). A
60-second [demo video](docs/media/rag-chat-demo.mp4) shows the full flow.

## Choose your models

The two `ollama pull` commands pull the **defaults**, not requirements. Each model
is set in [`config.yaml`](internal/bootstrap/configuration/config.yaml); pull
whichever model you choose and put its name in the matching key:

| Role | Config key | Default |
|---|---|---|
| Embeddings | `embedder.model` (and `embedder.dimensions`) | `embeddinggemma-300m-q8:latest` |
| Answers | `generator.model` | `qwen3.5:4b` |
| Follow-up rewriting | `rewriter.model` | `qwen3.5:4b` |
| Index-time context (off by default) | `enrichment.contextual.model` | `qwen3.5:4b` |
| Reranker (off by default) | `reranker.model` | `BAAI/bge-reranker-v2-m3` |

Changing the embedding model changes the vector size: set `embedder.dimensions` to
match, use a new `qdrant.collection` name or reindex, and adjust the prefixes
(`query_prefix`, `document_prefix`) that the model expects. The answer, rewriter
and enrichment models can be changed without reindexing, except enrichment, which
needs a reindex. The demo video and the measured results in the docs used the
defaults.

## Prerequisites

| Tool | Required? | Purpose |
|------|-----------|---------|
| [Podman](https://podman.io) + podman-compose | **Required for the provided local/Compose flow** | Qdrant and optional containerized reranker (rootless; `./scripts/setup_podman_host.sh` sets the laptop up) |
| Go 1.27+ | **Required** | Server + CLI |
| Python 3.12+ | **Required for `local.sh`; sidecars require 3.12+** | Local startup config parsing, optional reranker and PDF conversion (the `numpy==2.5.2` pin needs 3.12) |
| Node.js 24 LTS, or 22.x ≥22.12 | **Required for dashboard** | React dashboard and browser tests; Node 25 is unsupported |
| [Ollama](https://ollama.com) | **Required** | Serves the embedding and answer models (defaults: `embeddinggemma-300m-q8`, `qwen3.5:4b`; configurable) |

The default text roles use the default answer model with `think: false`. This keeps their bounded
output budgets available for answer text and search rewrites. Each role can set
`think: true`; omitting the setting leaves Ollama's model default in effect.

## Quick start

### 1. Configure your data source

The default has no source directories. Upload documents using the dashboard's
paperclip, or edit `internal/bootstrap/configuration/config.yaml` →
`documents.paths` to enable directory ingestion:

```yaml
documents:
  mode: "upload-only"     # upload-only | mirror
  paths:
    - "/path/to/documents"
```

`upload-only` keeps existing Documents when a source file disappears. Set
`mode: mirror` when the configured directories are the complete corpus; a
successful source sweep then removes indexed files missing from those
directories. Multipart uploads never trigger mirror deletion.

### 2. Start everything

```bash
./scripts/local.sh
```

This starts Qdrant and the Go API, starts the host reranker only when enabled,
ingests source directories when configured, and blocks on the server. With
`documents.paths: []`, it starts ready for uploads. Reranking is off
by default. Run the React dashboard separately:

```bash
cd web/dashboard
nvm use # if using nvm; .nvmrc selects Node 24
npm ci
npm run dev
```

Then open `http://localhost:3002`. Set `DASHBOARD_PORT` if that port is also
occupied; Vite uses a strict port and will fail clearly instead of silently
switching to a different URL.

### 3. Test search

Upload a document first, then ask about its contents:

```bash
curl -X POST localhost:8100/api/v1/turns \
  -H 'content-type: application/json' \
  -d '{"query":"What are the main ideas in the uploaded document?","generate":false}'
```

The control API uses JSON requests and responses. Live generated answers are
delivered by an SSE endpoint in the turn response; the React dashboard handles
that stream.

### 4. Include LLM answer generation

Set `generate` to `true` to run answer generation over the retrieved chunks:

```bash
curl -X POST localhost:8100/api/v1/turns \
  -H 'content-type: application/json' \
  -d '{"query":"What are the main ideas in the uploaded document?","generate":true}'
```
