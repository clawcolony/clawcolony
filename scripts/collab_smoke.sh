#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://127.0.0.1:18080}"
ROUNDS="${ROUNDS:-3}"

need() { command -v "$1" >/dev/null 2>&1 || { echo "$1 not found"; exit 1; }; }
need curl
need jq

post() {
  local path="$1"
  local body="$2"
  curl -fsS -X POST "${BASE_URL}${path}" -H 'Content-Type: application/json' -d "$body"
}

get() {
  local path="$1"
  curl -fsS "${BASE_URL}${path}"
}

register_user() {
  post /v1/bots/register '{"provider":"openclaw"}' | jq -r '.item.user_id'
}

echo "[collab-smoke] base=${BASE_URL} rounds=${ROUNDS}"

for n in $(seq 1 "$ROUNDS"); do
  echo "[round ${n}] register users"
  A="$(register_user)"
  B="$(register_user)"
  C="$(register_user)"

  echo "[round ${n}] propose"
  COLL="$(post /v1/collab/propose "{\"proposer_user_id\":\"${A}\",\"title\":\"smoke-${n}\",\"goal\":\"prove-collab-feasible\",\"complexity\":\"high\",\"min_members\":2,\"max_members\":3}" | jq -r '.item.collab_id')"

  echo "[round ${n}] apply"
  post /v1/collab/apply "{\"collab_id\":\"${COLL}\",\"user_id\":\"${B}\",\"pitch\":\"executor\"}" >/dev/null
  post /v1/collab/apply "{\"collab_id\":\"${COLL}\",\"user_id\":\"${C}\",\"pitch\":\"reviewer\"}" >/dev/null

  echo "[round ${n}] assign/start"
  post /v1/collab/assign "{\"collab_id\":\"${COLL}\",\"orchestrator_user_id\":\"${A}\",\"assignments\":[{\"user_id\":\"${A}\",\"role\":\"orchestrator\"},{\"user_id\":\"${B}\",\"role\":\"executor\"},{\"user_id\":\"${C}\",\"role\":\"reviewer\"}],\"rejected_user_ids\":[]}" >/dev/null
  post /v1/collab/start "{\"collab_id\":\"${COLL}\",\"orchestrator_user_id\":\"${A}\",\"status_or_summary_note\":\"start\"}" >/dev/null

  echo "[round ${n}] submit/review/close"
  ART_ID="$(post /v1/collab/submit "{\"collab_id\":\"${COLL}\",\"user_id\":\"${B}\",\"role\":\"executor\",\"kind\":\"code\",\"summary\":\"done\",\"content\":\"artifact-${n}\"}" | jq -r '.item.id')"
  post /v1/collab/review "{\"collab_id\":\"${COLL}\",\"reviewer_user_id\":\"${C}\",\"artifact_id\":${ART_ID},\"status\":\"accepted\",\"review_note\":\"ok\"}" >/dev/null
  post /v1/collab/close "{\"collab_id\":\"${COLL}\",\"orchestrator_user_id\":\"${A}\",\"result\":\"closed\",\"status_or_summary_note\":\"done\"}" >/dev/null

  PHASE="$(get "/v1/collab/get?collab_id=${COLL}" | jq -r '.item.phase')"
  EVENTS="$(get "/v1/collab/events?collab_id=${COLL}&limit=500" | jq -r '.items | length')"
  if [[ "$PHASE" != "closed" ]]; then
    echo "[round ${n}] FAIL: phase=${PHASE}" >&2
    exit 1
  fi
  echo "[round ${n}] PASS collab_id=${COLL} phase=${PHASE} events=${EVENTS}"
done

echo "[collab-smoke] all rounds passed"
