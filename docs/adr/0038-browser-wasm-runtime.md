# 0038 — In-browser WASM runtime

Status: Accepted

## Context

Nadir needs a deployment with no server: static hosting, documents and
conversations that never leave the user's device, and no containers. The Go
core (`internal/core/**`) already owns its provider seams (ADR 0020) and has no
OS dependencies, and the dashboard reaches the API through one transport seam.
Qdrant (gRPC), Ollama, the Python reranker sidecar and Docling cannot run in a
browser.

## Decision

Add a second runtime next to the Podman stack, not a replacement:

- `internal/bootstrap/assembly` builds the indexing/retrieval graph
  (`Assemble`) and the Chat + HTTP surface (`BuildAPI`) from injected providers
  and links no gRPC or Qdrant code. `internal/bootstrap/runtime` and `server`
  are now the Qdrant/Ollama adapter set on top of it.
- `cmd/wasm` (GOOS=js GOARCH=wasm) runs that graph over in-memory adapters
  (`internal/providers/memory/{documents,history}`) and host-page model
  functions bridged through `internal/providers/browser` (embedder, generator,
  rewriter, reranker, PDF converter, in-page HTTP `Fetch`). The real router,
  SSE event log, cancel, edit and history code run unchanged.
- The dashboard's transport (`web/dashboard/src/lib/transport.ts`) can be
  replaced by an in-page fetch plus an EventSource shim. The Go core runs in a
  Web Worker; transformers.js (EmbeddingGemma-300m q8, MiniLM cross-encoder)
  and WebLLM run in their own workers; pdf.js extracts PDF text. Documents and
  history persist as snapshots in the origin-private file system.
- Select it with `VITE_RUNTIME=browser` or `?runtime=browser`; the server
  runtime stays the default. The public JSON/SSE contract is unchanged.

## Consequences

- Retrieval keeps the server's chunking, BM25 + dense RRF fusion, versioned
  replacement and query rewriting. The store keeps everything in memory and
  rewrites whole snapshots, so it targets personal corpora (thousands of
  chunks), not large ones. sqlite-vec/OPFS-native storage is the upgrade path.
- The dense leg is an exact scan, and switches to a pure-Go HNSW index
  (`internal/providers/memory/hnsw`) once the corpus reaches
  `Config.IndexThreshold` (default 20,000 chunks). Filtered queries always scan
  exactly, and the index only chooses candidates: they are re-scored with exact
  cosine, so scores match the scan. Measured natively on synthetic
  embedding-like 768-dimension vectors (`BenchmarkHybridSearch`):

  | Chunks | Exact query | HNSW query | HNSW load |
  |--------|-------------|------------|-----------|
  | 1k     | 1.1 ms      | 0.6 ms     | 0.8 s     |
  | 10k    | 11.8 ms     | 3.8 ms     | 15 s      |
  | 50k    | 63.5 ms     | 19.8 ms    | 109 s     |

  The gain is capped by BM25, which still scans every chunk (18 ms of the 20 ms
  at 50k); an inverted index would be the next step. The graph is rebuilt on
  every `Restore` because snapshots hold vectors only, so the 15 s per 10k
  chunks is paid on each page load (wasm will be slower). That is why the
  threshold is high: below it an exact query is a few milliseconds, far less
  than embedding or generation. Persisting the graph in the snapshot is the
  prerequisite for lowering the threshold. Recall@10 is about 0.975 at
  ef=64 on structured data; on isotropic random 768-dimension vectors it is
  0.3, because no neighbour structure exists, so benchmarks must not use them.
- Models differ from the server defaults (reranker is MiniLM, not the server's
  model; the generator is a ~1.5B WebLLM model, WebGPU only). ADRs 0032–0035
  were measured on the server stack, so this runtime needs its own Ragas run
  before any quality claim; none has been done.
- PDF intake is plain text extraction (no layout or tables, no OCR).
- Static hosting works (including Cloudflare Workers assets); onnxruntime-web's
  ~27 MB wasm exceeds Cloudflare's 25 MiB asset limit, so it is pruned from the
  build and loaded from `VITE_ORT_BASE_URL` or jsDelivr. Running the core
  inside a Cloudflare Worker is not supported: no WebGPU, OPFS or nested
  workers.
- Verification status:
  - `make wasm-check` (CI) builds, vets and runs the assembly and store tests
    inside wasm and a Node smoke test of the full API.
  - The dashboard e2e job builds the wasm and runs the browser spec with scripted
    models.
  - Opt-in real-model specs have passed locally: retrieval with the real
    embedder and reranker (`E2E_REAL_MODELS=1`), and a grounded answer from real
    WebLLM Qwen2.5-1.5B on an NVIDIA GPU through Vulkan (`E2E_REAL_LLM=1`).
  - Not yet exercised: LLM query rewriting, pdf.js on real PDFs, a Ragas
    comparison, and a Cloudflare deploy.
- WebLLM's default `q4f16_1` builds need the WebGPU `shader-f16` feature, which
  some drivers (including the Linux NVIDIA/Vulkan one tested) do not expose; use
  a `q4f32_1` build with `?llm=<model id>` there. The runtime does not fall back
  automatically yet.
