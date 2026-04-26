#!/usr/bin/env bash
# Backup Qdrant Docker volume to a timestamped tar.gz archive.
# Usage: ./scripts/backup-qdrant.sh [backup-dir]
# Cron example (daily 2am): 0 2 * * * /path/to/nadir/scripts/backup-qdrant.sh /var/backups/qdrant
set -euo pipefail

BACKUP_DIR="${1:-./backups}"
VOLUME="nadir_qdrant_data"
TIMESTAMP="$(date +%Y%m%d_%H%M%S)"
ARCHIVE="${BACKUP_DIR}/qdrant_${TIMESTAMP}.tar.gz"
KEEP_DAYS="${KEEP_DAYS:-7}"

mkdir -p "$BACKUP_DIR"

echo "[backup] snapshotting volume ${VOLUME} → ${ARCHIVE}"
docker run --rm \
  -v "${VOLUME}:/data:ro" \
  -v "$(realpath "$BACKUP_DIR"):/backup" \
  alpine \
  tar czf "/backup/qdrant_${TIMESTAMP}.tar.gz" -C /data .

echo "[backup] done: ${ARCHIVE}"

# Prune archives older than KEEP_DAYS (default 7)
find "$BACKUP_DIR" -name "qdrant_*.tar.gz" -mtime "+${KEEP_DAYS}" -delete
echo "[backup] pruned archives older than ${KEEP_DAYS} days"
