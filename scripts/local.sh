#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

cd "$ROOT"

# addrs come from config/config.yaml (already localhost for host-side server);
# only override here if you need something config.yaml doesn't already have.

# The reranker sidecar runs in Docker, so its swappable model name travels
# from config.yaml (reranker.model) through the RERANKER_MODEL env var.
RERANKER_MODEL="$(awk '/^reranker:/{f=1; next} f && /^[^ ]/{f=0} f && /model:/{gsub(/[\"'"'"']/, ""); sub(/#.*/, "", $2); print $2; exit}' config/config.yaml)"
export RERANKER_MODEL
echo "==> Reranker model: ${RERANKER_MODEL:-<compose default>}"

echo "==> Starting Qdrant, Reranker..."
docker compose up -d --remove-orphans qdrant reranker

echo "==> Waiting for Qdrant to be ready..."
until curl -sf http://localhost:6333/healthz > /dev/null 2>&1; do sleep 1; done

echo "==> Waiting for Reranker on :5002..."
until curl -sf http://localhost:5002/health > /dev/null 2>&1; do sleep 1; done

echo "==> Killing any process on :8100 and :6063..."
kill $(lsof -ti :8100,6063) 2>/dev/null || true
sleep 1

echo "==> Starting server (background)..."
go run ./cmd/server &
SERVER_PID=$!

echo "==> Waiting for server on :8100..."
until curl -sf http://localhost:8100/healthz > /dev/null 2>&1; do sleep 1; done

echo "==> Ingesting sample documents..."
curl -sf -X POST localhost:8100/ingest $(for f in samples/*.md; do echo -F "files=@$f"; done)

echo ""
echo "Local stack running. Server PID=$SERVER_PID"
echo "  Search: curl -X POST localhost:8100/retrieval/search --data-urlencode 'query=...'"
echo "  Stop:   kill $SERVER_PID && docker compose down"

wait "$SERVER_PID"
