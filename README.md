# nadir

Personal knowledge base (PKB) search engine. Ingests markdown notes, chunks + embeds them locally, stores in Qdrant, serves hybrid semantic+keyword search over HTTP.

## Stack

- **Embedder:** Ollama (`nomic-embed-text`, 768-dim)
- **Vector store:** Qdrant (gRPC)
- **Search:** Hybrid dense + BM25 sparse via RRF
- **Runtime:** Single Go binary

## Quick start

```bash
# Dependencies
docker compose up -d   # starts Qdrant

# Config
cp config/config.yaml config/config.local.yaml
# set knowledge_base.path to your notes directory

# Run
make run

# Ingest notes
make ingest

# Search
make search
```

## Routes

| Method | Path | Description |
|--------|------|-------------|
| POST | `/ingest` | Walk notes dir, chunk+embed new/changed files |
| POST | `/search` | Hybrid semantic search over embedded chunks |

## Config

`config/config.yaml` — all settings (chunker, embedder, Qdrant, retry).  
Override notes path: `NOTES_PATH=<path> make run`

## Architecture

```
POST /ingest → FileLister → Fetcher → Pipeline
                                         ├── Chunker (heading→paragraph→sentence→word)
                                         ├── Embedder (Ollama)
                                         └── Store.Upsert (Qdrant)

POST /search → Embedder → Store.HybridSearch (dense + BM25 → RRF)
```

Core logic lives in `internal/pkb/`. SHA-based dedup — unchanged files skip re-embedding.

## Dev

```bash
make vendor      # go mod tidy && go mod vendor
go build ./cmd/http
go test ./...
make d           # delete Qdrant collection (reset)
```
