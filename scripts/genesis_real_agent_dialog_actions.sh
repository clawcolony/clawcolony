#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://127.0.0.1:18080}"
MIN_USERS="${MIN_USERS:-3}"
PF_ENABLED="${PF_ENABLED:-1}"
REPLY_TIMEOUT_SECONDS="${REPLY_TIMEOUT_SECONDS:-120}"
POLL_SECONDS="${POLL_SECONDS:-2}"
ACTION_TIMEOUT_SECONDS="${ACTION_TIMEOUT_SECONDS:-300}"
ACTION_RETRY_COUNT="${ACTION_RETRY_COUNT:-3}"
MAIL_WAIT_PER_TRY_SECONDS="${MAIL_WAIT_PER_TRY_SECONDS:-120}"

log() { printf '[genesis-dialog-actions] %s\n' "$*"; }

json_post() {
  local path="$1"
  local body="$2"
  curl -sf -X POST "${BASE_URL}${path}" -H 'content-type: application/json' -d "$body"
}

json_get() {
  local path="$1"
  curl -sf "${BASE_URL}${path}"
}

ensure_health() {
  json_get "/healthz" | jq -e '.status=="ok"' >/dev/null
}

start_port_forward() {
  if [[ "${PF_ENABLED}" != "1" ]]; then
    return
  fi
  if [[ "${BASE_URL}" != "http://127.0.0.1:18080" ]]; then
    return
  fi
  kubectl -n freewill port-forward svc/clawcolony 18080:8080 >/tmp/clawcolony-pf.log 2>&1 &
  PF_PID=$!
  sleep 2
}

cleanup() {
  if [[ -n "${PF_PID:-}" ]]; then
    kill "${PF_PID}" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

send_chat() {
  local user_id="$1"
  local message="$2"
  json_post "/v1/chat/send" "$(jq -nc --arg uid "$user_id" --arg msg "$message" '{user_id:$uid,message:$msg}')"
}

wait_user_reply_after() {
  local user_id="$1"
  local base_id="$2"
  local timeout="$3"
  local rounds=$((timeout / POLL_SECONDS))
  local i body rid
  for ((i=0; i<rounds; i++)); do
    rid="$(
      json_get "/v1/chat/history?user_id=${user_id}&limit=120" \
        | jq -r --arg uid "$user_id" --argjson base "$base_id" '
            [.items[]? | select(.from==$uid and (.id > $base))] | sort_by(.id) | last | .id // empty
          '
    )"
    if [[ -n "${rid}" ]]; then
      body="$(
        json_get "/v1/chat/history?user_id=${user_id}&limit=120" \
          | jq -r --arg uid "$user_id" --argjson rid "$rid" '
              [.items[]? | select(.from==$uid and (.id == $rid))] | .[0].body // empty
            '
      )"
      printf '%s' "${body}"
      return 0
    fi
    sleep "${POLL_SECONDS}"
  done
  return 1
}

latest_user_reply_id() {
  local user_id="$1"
  json_get "/v1/chat/history?user_id=${user_id}&limit=120" \
    | jq -r --arg uid "$user_id" '[.items[]? | select(.from==$uid) | .id] | max // 0'
}

wait_mail_inbox_match() {
  local user_id="$1"
  local subject="$2"
  local body="$3"
  local timeout="$4"
  local rounds=$((timeout / POLL_SECONDS))
  local i
  for ((i=0; i<rounds; i++)); do
    if json_get "/v1/mail/inbox?user_id=${user_id}&scope=all&limit=200" \
      | jq -e --arg s "${subject}" --arg b "${body}" '
          .items[]?
          | select(
              (.subject == $s and .body == $b)
              or
              (
                ((.subject // "") | ascii_downcase | contains(($s | ascii_downcase)))
                and
                ((.body // "") | contains($b))
              )
            )
        ' >/dev/null; then
      return 0
    fi
    sleep "${POLL_SECONDS}"
  done
  return 1
}

run_mail_action_with_retry() {
  local actor="$1"
  local target="$2"
  local prompt="$3"
  local expected_subject="$4"
  local expected_body="$5"
  local label="$6"
  local attempt base reply wait_seconds

  for attempt in $(seq 1 "${ACTION_RETRY_COUNT}"); do
    base="$(latest_user_reply_id "${actor}")"
    send_chat "${actor}" "${prompt}" >/dev/null
    reply="$(wait_user_reply_after "${actor}" "${base}" "${REPLY_TIMEOUT_SECONDS}" || true)"

    if [[ -n "${reply}" ]]; then
      log "${label} reply(attempt=${attempt}): ${reply}"
    else
      log "${label} no reply(attempt=${attempt}) within ${REPLY_TIMEOUT_SECONDS}s"
    fi

    wait_seconds="${MAIL_WAIT_PER_TRY_SECONDS}"
    if [[ "${reply}" == *"session file locked"* ]]; then
      log "${label} lock conflict(attempt=${attempt}), use short mail check window"
      wait_seconds=25
    fi

    if wait_mail_inbox_match "${target}" "${expected_subject}" "${expected_body}" "${wait_seconds}"; then
      log "${label} PASS subject=${expected_subject} attempt=${attempt}"
      return 0
    fi

    if (( attempt < ACTION_RETRY_COUNT )); then
      log "${label} retrying attempt=$((attempt + 1))"
      sleep 6
    fi
  done

  log "FAIL: ${label} not delivered after ${ACTION_RETRY_COUNT} attempts subject=${expected_subject}"
  return 1
}

start_port_forward
ensure_health

USERS=()
while IFS= read -r line; do
  [[ -n "${line}" ]] && USERS+=("${line}")
done < <(json_get "/v1/openclaw/admin/overview" | jq -r '.pods[].user_id')

if (( ${#USERS[@]} < MIN_USERS )); then
  log "FAIL: active users=${#USERS[@]}, require >= ${MIN_USERS}"
  exit 1
fi

A="${USERS[0]}"
B="${USERS[1]}"

log "dialog action flow A=${A} B=${B}"

S1="genesis-dialog-mail-a2b-$(date +%s)"
B1="dialog-body-a2b-${S1}"

PROMPT1="$(cat <<EOF
执行一个操作任务（不要解释过程）：
1) 使用 mailbox-network 能力，向 user_id=${B} 发送一封邮件。
2) subject 必须精确为：${S1}
3) body 必须精确为：${B1}
4) 禁止改写 subject，禁止添加任何前缀（例如 re: / 回复: / fwd:）。
完成后回复一句确认即可。
EOF
)"

run_mail_action_with_retry "${A}" "${B}" "${PROMPT1}" "${S1}" "${B1}" "A->B"

S2="genesis-dialog-mail-b2a-$(date +%s)"
B2="dialog-body-b2a-${S2}"

PROMPT2="$(cat <<EOF
执行一个操作任务（不要解释过程）：
1) 先检查你的 inbox，确认收到 subject 为 ${S1} 的邮件。
2) 使用 mailbox-network 能力，向 user_id=${A} 发送一封邮件。
3) subject 必须精确为：${S2}
4) body 必须精确为：${B2}
5) 禁止改写 subject，禁止添加任何前缀（例如 re: / 回复: / fwd:）。
完成后回复一句确认即可。
EOF
)"

run_mail_action_with_retry "${B}" "${A}" "${PROMPT2}" "${S2}" "${B2}" "B->A"

log "PASS dialog-driven agent actions"
