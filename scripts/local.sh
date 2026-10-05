#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$ROOT"

COMPOSE=(podman compose -f deploy/compose/compose.yaml)
export INFERENCE_PROFILE="${INFERENCE_PROFILE:-local}"
export INFERENCE_OLLAMA_KEEP_ALIVE="${INFERENCE_OLLAMA_KEEP_ALIVE:-5m}"
export RERANKER_DEVICE="${RERANKER_DEVICE:-cpu}"
export RERANKER_BACKEND="${RERANKER_BACKEND:-torch}"
export RERANKER_MAX_CONCURRENT="${RERANKER_MAX_CONCURRENT:-1}"
export RERANKER_QUEUE_TIMEOUT="${RERANKER_QUEUE_TIMEOUT:-30s}"
export DASHBOARD_PORT="${DASHBOARD_PORT:-3002}"
STARTUP_TIMEOUT="${LOCAL_STARTUP_TIMEOUT:-180}"
CONFIG_PATH="${NADIR_CONFIG:-internal/bootstrap/configuration/config.yaml}"
LOCAL_BUILD_DIR="$(mktemp -d "${TMPDIR:-/tmp}/nadir-local.XXXXXX")"
RERANKER_PID=""
SERVER_PID=""
cleanup() {
  [[ -z "$SERVER_PID" ]] || kill "$SERVER_PID" 2>/dev/null || true
  [[ -z "$RERANKER_PID" ]] || kill "$RERANKER_PID" 2>/dev/null || true
  rm -rf "$LOCAL_BUILD_DIR"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

echo "==> Building API..."
go build -o "$LOCAL_BUILD_DIR/api" ./cmd/api
STARTUP_JSON="$("$LOCAL_BUILD_DIR/api" --config "$CONFIG_PATH" --startup-config)"
STARTUP_LINES="$(python3 -c 'import json,sys; c=json.load(sys.stdin); print(c["api_url"]); print(str(c["reranker_enabled"]).lower()); print(c["reranker_model"]); print(c["reranker_addr"]); print(str(c["documents_paths_configured"]).lower())' <<< "$STARTUP_JSON")"
{
  IFS= read -r API_URL
  IFS= read -r RERANKER_ENABLED
  IFS= read -r RERANKER_MODEL
  IFS= read -r RERANKER_URL
  IFS= read -r DOCUMENTS_PATHS_CONFIGURED
} <<< "$STARTUP_LINES"
export RERANKER_MODEL
RERANKER_URL="${RERANKER_URL%/}"

wait_ready() {
  local label="$1" url="$2" child_pid="${3:-}" deadline=$((SECONDS + STARTUP_TIMEOUT))
  while ! curl -sf --max-time 3 "$url" >/dev/null; do
    if [[ -n "$child_pid" ]] && ! kill -0 "$child_pid" 2>/dev/null; then
      echo "$label exited before becoming ready" >&2
      return 1
    fi
    if (( SECONDS >= deadline )); then
      echo "Timed out waiting for $label ($url)" >&2
      return 1
    fi
    sleep 1
  done
}

echo "==> Starting Qdrant (Podman)..."
"${COMPOSE[@]}" up -d qdrant
wait_ready Qdrant "http://localhost:${QDRANT_HTTP_PORT:-6333}/healthz"

if [[ "$RERANKER_ENABLED" == true ]]; then
  echo "==> Starting reranker (model=$RERANKER_MODEL, device=$RERANKER_DEVICE, backend=$RERANKER_BACKEND)..."
  "${COMPOSE[@]}" stop reranker >/dev/null 2>&1 || true
  venv/bin/python sidecars/reranker/main.py &
  RERANKER_PID=$!
  wait_ready Reranker "$RERANKER_URL/health" "$RERANKER_PID"
else
  echo "==> Reranker disabled; no sidecar needed."
fi

echo "==> Starting API..."
"$LOCAL_BUILD_DIR/api" --config "$CONFIG_PATH" &
SERVER_PID=$!
wait_ready API "$API_URL/api/v1/ready" "$SERVER_PID"

if [[ "$DOCUMENTS_PATHS_CONFIGURED" == true ]]; then
  echo "==> Ingesting configured source documents..."
  curl -sf --max-time "$STARTUP_TIMEOUT" -X POST "$API_URL/api/v1/documents"
  echo ""
else
  echo "==> No source directories configured. Upload documents using the dashboard paperclip."
fi
echo "Local stack running. API: $API_URL; server PID=$SERVER_PID"
echo "  Dashboard: http://localhost:${DASHBOARD_PORT} (cd web/dashboard && npm run dev)"
echo "  Ctrl-C stops this launcher's API and sidecar. Qdrant remains available."
wait "$SERVER_PID"
