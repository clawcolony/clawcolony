#!/usr/bin/env bash
set -euo pipefail

KB_BASE_URL="${KB_BASE_URL:-http://127.0.0.1:8080}"
DEFAULT_USER_ID="${DEFAULT_USER_ID:-}"
AUTH_TOKEN="${AUTH_TOKEN:-}"
TIMEOUT_SECONDS="${TIMEOUT_SECONDS:-20}"

usage() {
  cat <<'EOF'
Usage: scripts/mcp_knowledgebase_smoke.sh [options]

Run end-to-end MCP smoke against mcp-knowledgebase stdio server:
1) initialize
2) tools/list
3) tools/call mcp-knowledgebase.governance.protocol

Options:
  --kb-base-url <url>       Runtime API base URL (default: http://127.0.0.1:8080)
  --default-user-id <id>    user_id used in MCP calls (auto-pick first running user when empty)
  --auth-token <token>      Optional runtime token (adds ?token=... when resolving user_id)
  --timeout-seconds <sec>   MCP request timeout (default: 20)
  -h, --help                Show help
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --kb-base-url)
      KB_BASE_URL="$2"; shift 2 ;;
    --default-user-id)
      DEFAULT_USER_ID="$2"; shift 2 ;;
    --auth-token)
      AUTH_TOKEN="$2"; shift 2 ;;
    --timeout-seconds)
      TIMEOUT_SECONDS="$2"; shift 2 ;;
    -h|--help)
      usage; exit 0 ;;
    *)
      echo "Unknown argument: $1" >&2
      usage
      exit 2 ;;
  esac
done

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 2
  }
}
need_cmd jq
need_cmd curl
need_cmd go

if [[ -z "${DEFAULT_USER_ID}" ]]; then
  user_url="${KB_BASE_URL%/}/v1/bots?include_inactive=0&limit=200"
  if [[ -n "${AUTH_TOKEN}" ]]; then
    user_url="${user_url}&token=${AUTH_TOKEN}"
  fi
  DEFAULT_USER_ID="$(curl -fsS "${user_url}" | jq -r '.items[0].user_id // empty')"
fi

if [[ -z "${DEFAULT_USER_ID}" ]]; then
  echo "no running user found for mcp smoke (pass --default-user-id to override)" >&2
  exit 1
fi

json_len() {
  printf '%s' "$1" | wc -c | tr -d '[:space:]'
}

emit_frame() {
  local payload="$1"
  local n
  n="$(json_len "${payload}")"
  printf 'Content-Length: %s\r\nContent-Type: application/json\r\n\r\n%s' "${n}" "${payload}"
}

P1='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"mcp-smoke","version":"1"}}}'
P2='{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}'
P3=$(printf '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"mcp-knowledgebase.governance.protocol","arguments":{"user_id":"%s"}}}' "${DEFAULT_USER_ID}")

TMP_OUT="$(mktemp)"
cleanup() {
  rm -f "${TMP_OUT}"
}
trap cleanup EXIT

args=(go run ./cmd/mcp-knowledgebase --kb-base-url "${KB_BASE_URL}" --default-user-id "${DEFAULT_USER_ID}")
if [[ -n "${AUTH_TOKEN}" ]]; then
  args+=(--auth-token "${AUTH_TOKEN}")
fi

run_cmd=("${args[@]}")
if command -v timeout >/dev/null 2>&1; then
  run_cmd=(timeout "${TIMEOUT_SECONDS}" "${args[@]}")
fi

{
  emit_frame "${P1}"
  emit_frame "${P2}"
  emit_frame "${P3}"
} | "${run_cmd[@]}" >"${TMP_OUT}"

grep -q '"protocolVersion":"2024-11-05"' "${TMP_OUT}"
grep -q 'mcp-knowledgebase.governance.protocol' "${TMP_OUT}"
grep -q 'knowledgebase-governance-v1' "${TMP_OUT}"

echo "PASS: mcp-knowledgebase smoke ok (user_id=${DEFAULT_USER_ID}, kb_base_url=${KB_BASE_URL})"
