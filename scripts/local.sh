#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

cd "$ROOT"

COMPOSE=(docker compose -f deploy/compose/docker-compose.yml)

# addrs come from config/config.yaml (already localhost for host-side server);
# only override here if you need something config.yaml doesn't already have.

# The reranker model travels from config.yaml (reranker.model) through the
# RERANKER_MODEL env var, to the sidecar however it is hosted.
RERANKER_MODEL="$(awk '/^reranker:/{f=1; next} f && /^[^ ]/{f=0} f && /model:/{gsub(/[\"'"'"']/, ""); sub(/#.*/, "", $2); print $2; exit}' config/config.yaml)"
export RERANKER_MODEL
echo "==> Reranker model: ${RERANKER_MODEL:-<compose default>}"

echo "==> Starting Qdrant (Docker)..."
"${COMPOSE[@]}" up -d --remove-orphans qdrant
# The compose reranker needs the NVIDIA container toolkit for its GPU
# reservation; the dev flow runs the sidecar from the repo venv on the host
# GPU instead (same as Ollama). Free the port from any compose instance.
"${COMPOSE[@]}" stop reranker >/dev/null 2>&1 || true

echo "==> Killing any process on :5002, :8100 and :6063..."
kill $(lsof -ti :5002,8100,6063 2>/dev/null) 2>/dev/null || true
sleep 1

echo "==> Starting reranker sidecar (repo venv, host GPU)..."
RERANKER_DEVICE=auto venv/bin/python services/reranker/main.py &
RERANKER_PID=$!
trap 'kill $RERANKER_PID 2>/dev/null || true' EXIT

echo "==> Waiting for Qdrant to be ready..."
until curl -sf http://localhost:6333/healthz > /dev/null 2>&1; do sleep 1; done

echo "==> Waiting for Reranker on :5002..."
until curl -sf http://localhost:5002/health > /dev/null 2>&1; do sleep 1; done

echo "==> Starting server (background)..."
go run ./cmd/api &
SERVER_PID=$!

echo "==> Waiting for server on :8100..."
until curl -sf http://localhost:8100/api/v1/health > /dev/null 2>&1; do sleep 1; done

echo "==> Ingesting configured source documents..."
curl -sf -X POST localhost:8100/api/v1/documents

echo ""
echo "Local stack running. Server PID=$SERVER_PID, Reranker PID=$RERANKER_PID"
echo "  Dashboard: http://localhost:3000 (cd web/dashboard && npm ci && npm run dev)"
echo "  Search: curl -X POST localhost:8100/api/v1/turns -H 'content-type: application/json' -d '{\"query\":\"...\"}'"
echo "  Stop:   kill $SERVER_PID $RERANKER_PID && docker compose -f deploy/compose/docker-compose.yml stop"
echo "  (full-Docker reranker needs the NVIDIA container toolkit; see AGENTS.md)"

wait "$SERVER_PID"
