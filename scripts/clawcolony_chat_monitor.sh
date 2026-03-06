#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://127.0.0.1:8080}"
LIMIT="${LIMIT:-200}"
INTERVAL="${INTERVAL:-1}"

if ! command -v curl >/dev/null 2>&1; then
  echo "curl not found"
  exit 1
fi
if ! command -v jq >/dev/null 2>&1; then
  echo "jq not found"
  exit 1
fi

last_id=0
echo "[monitor] BASE_URL=${BASE_URL} LIMIT=${LIMIT} INTERVAL=${INTERVAL}s"
echo "[monitor] watching all channels from /v1/chat/history ..."

while true; do
  resp="$(curl -fsS "${BASE_URL}/v1/chat/history?limit=${LIMIT}" 2>/dev/null || true)"
  if [[ -n "${resp}" ]]; then
    new_max="$(jq -r '[.items[]?.id] | max // 0' <<<"${resp}")"
    jq -r --argjson last "${last_id}" '
      (.items // [])
      | map(select(.id > $last))
      | sort_by(.id)
      | .[]
      | "[\(.id)] [\(.target_type)] \(.sender) -> \(.target): \(.content)"
    ' <<<"${resp}"
    if [[ "${new_max}" =~ ^[0-9]+$ ]] && (( new_max > last_id )); then
      last_id="${new_max}"
    fi
  fi
  sleep "${INTERVAL}"
done

