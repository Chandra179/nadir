#!/usr/bin/env bash
# Trigger Qdrant collection snapshot via REST API.
# Cron example (daily 2am): 0 2 * * * /path/to/nadir/scripts/snapshot-qdrant.sh
# Snapshots land in the qdrant_data volume under /qdrant/snapshots.
set -euo pipefail

QDRANT_URL="${QDRANT_URL:-http://localhost:6333}"
COLLECTION="${QDRANT_COLLECTION:-pkb_chunks}"

echo "[snapshot] triggering snapshot: ${QDRANT_URL}/collections/${COLLECTION}/snapshots"
RESPONSE=$(curl -sf -X POST "${QDRANT_URL}/collections/${COLLECTION}/snapshots")
echo "[snapshot] done: ${RESPONSE}"
