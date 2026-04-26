#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

cd "$ROOT"

# Load .env if present
if [[ -f .env ]]; then
  set -a; source .env; set +a
fi

echo "==> Starting Qdrant, Splade, Reranker..."
docker compose up -d qdrant splade reranker

echo "==> Waiting for Qdrant to be ready..."
until curl -sf http://localhost:6333/healthz > /dev/null 2>&1; do sleep 1; done

echo "==> Waiting for Splade on :5001..."
until curl -sf http://localhost:5001/health > /dev/null 2>&1; do sleep 1; done

echo "==> Waiting for Reranker on :5002..."
until curl -sf http://localhost:5002/health > /dev/null 2>&1; do sleep 1; done

echo "==> Starting server (background)..."
go run ./cmd/http &
SERVER_PID=$!

echo "==> Waiting for server on :8080..."
until curl -sf http://localhost:8080/healthz > /dev/null 2>&1; do sleep 1; done

echo "==> Ingesting notes..."
curl -sf -X POST localhost:8080/ingest

echo ""
echo "Local stack running. Server PID=$SERVER_PID"
echo "  Search: curl -X POST localhost:8080/search -H 'Content-Type: application/json' -d '{\"query\":\"...\",\"top_k\":5}'"
echo "  Stop:   kill $SERVER_PID && docker compose down"

wait "$SERVER_PID"
