#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

cd "$ROOT"

echo "==> Deploying prod stack..."
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d

echo "==> Waiting for Qdrant..."
until curl -sf http://localhost:6333/healthz > /dev/null 2>&1; do sleep 1; done

echo "==> Waiting for server on :8080..."
until curl -sf http://localhost:8080/healthz > /dev/null 2>&1; do sleep 1; done

echo "==> Converting docs PDF to MD..."
python3 services/docling/main.py --input pdfs/raw --output pdfs/converted || true

echo "==> Ingesting notes..."
curl -sf -X POST localhost:8080/ingest

echo ""
echo "Prod stack running."
echo "  Search: curl -X POST localhost:8080/search -H 'Content-Type: application/json' -d '{\"query\":\"...\",\"top_k\":5}'"
