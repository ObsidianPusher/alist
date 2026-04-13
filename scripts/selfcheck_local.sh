#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP_DATA_DIR="$(mktemp -d /tmp/alist-selfcheck-XXXXXX)"
COREPACK_HOME_DIR="$(mktemp -d /tmp/corepack-selfcheck-XXXXXX)"
SERVER_LOG="$TMP_DATA_DIR/server.log"

cleanup() {
  if [[ -n "${SERVER_PID:-}" ]] && kill -0 "$SERVER_PID" >/dev/null 2>&1; then
    kill "$SERVER_PID" >/dev/null 2>&1 || true
    wait "$SERVER_PID" >/dev/null 2>&1 || true
  fi
  rm -rf "$TMP_DATA_DIR" "$COREPACK_HOME_DIR"
}
trap cleanup EXIT

cd "$ROOT_DIR"

echo "[1/7] run go tests"
go test ./...

echo "[2/7] build web"
COREPACK_HOME="$COREPACK_HOME_DIR" pnpm --dir web build >/dev/null

echo "[3/7] static guard checks"
if rg -n 'set_aria2|/set_aria2|offline_download/aria2|pkg/aria2' cmd drivers internal pkg server web/src >/dev/null 2>&1; then
  echo "found forbidden aria2 references"
  exit 1
fi
if rg -n 'func S3\(|InitS3\(|server/s3' cmd internal server >/dev/null 2>&1; then
  echo "found forbidden s3 service references"
  exit 1
fi

echo "[4/7] start server for runtime checks"
go run . server --data "$TMP_DATA_DIR" --no-prefix >"$SERVER_LOG" 2>&1 &
SERVER_PID=$!

for _ in {1..40}; do
  if curl -fsS "http://127.0.0.1:5244/ping" >/dev/null 2>&1; then
    break
  fi
  sleep 0.5
done

if ! curl -fsS "http://127.0.0.1:5244/ping" >/dev/null 2>&1; then
  echo "server health check failed"
  tail -n 100 "$SERVER_LOG" || true
  exit 1
fi

echo "[5/7] verify offline download API removed"
TOOLS_RESP="$(curl -sS "http://127.0.0.1:5244/api/public/offline_download_tools" || true)"
if echo "$TOOLS_RESP" | grep -Eq '"code"[[:space:]]*:[[:space:]]*200' && echo "$TOOLS_RESP" | grep -Eq '"data"[[:space:]]*:[[:space:]]*\['; then
  echo "offline download tools endpoint should not return a success API payload"
  echo "$TOOLS_RESP"
  exit 1
fi

echo "[6/7] verify request id header"
REQ_HEADER="$(curl -sI "http://127.0.0.1:5244/ping" | tr -d '\r' | grep -i '^X-Request-Id:' | head -n 1 | awk '{print $2}')"
if [[ -z "$REQ_HEADER" ]]; then
  echo "missing X-Request-ID header"
  exit 1
fi

echo "[7/7] self-check passed"
