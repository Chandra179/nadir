#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

cd "$ROOT"

if [[ -f .env ]]; then
  set -a; source .env; set +a
fi

echo "==> Deploying prod stack..."
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d

echo "==> Waiting for Qdrant..."
until curl -sf http://localhost:6333/healthz > /dev/null 2>&1; do sleep 1; done

echo "==> Waiting for server on :8080..."
until curl -sf http://localhost:8080/healthz > /dev/null 2>&1; do sleep 1; done

echo "==> Ingesting notes..."
curl -sf -X POST localhost:8080/ingest

echo ""
echo "Prod stack running."
echo "  Search: curl -X POST localhost:8080/search -H 'Content-Type: application/json' -d '{\"query\":\"...\",\"top_k\":5}'"
echo ""
echo "Schedule Qdrant backups via cron:"
echo "  0 2 * * * $SCRIPT_DIR/snapshot-qdrant.sh"
echo "  0 3 * * * KEEP_DAYS=7 $SCRIPT_DIR/backup-qdrant.sh /var/backups/qdrant"
