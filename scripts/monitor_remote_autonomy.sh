#!/usr/bin/env bash
set -euo pipefail

# Usage:
#   scripts/monitor_remote_autonomy.sh [ssh_target]
#
# Examples:
#   scripts/monitor_remote_autonomy.sh
#   WINDOW_MINUTES=20 LIMIT=1200 scripts/monitor_remote_autonomy.sh lty1993@192.234.79.198

SSH_TARGET="${1:-lty1993@192.234.79.198}"
WINDOW_MINUTES="${WINDOW_MINUTES:-30}"
LIMIT="${LIMIT:-800}"
PER_USER_LIMIT="${PER_USER_LIMIT:-300}"
SSH_OPTS=(
  -o BatchMode=yes
  -o ConnectTimeout=12
  -o ServerAliveInterval=10
  -o ServerAliveCountMax=3
)

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 2
  fi
}

need_cmd ssh
need_cmd jq

remote_get() {
  local path="$1"
  local attempt out
  for attempt in 1 2 3; do
    if out="$(ssh "${SSH_OPTS[@]}" "${SSH_TARGET}" \
      "minikube kubectl -- -n clawcolony exec deploy/clawcolony -- sh -lc 'wget -qO- \"http://127.0.0.1:8080${path}\"'" 2>/dev/null)"; then
      printf '%s' "${out}"
      return 0
    fi
    sleep $((attempt * 2))
  done
  echo "remote_get failed after retries: ${path}" >&2
  return 1
}

echo "[info] target=${SSH_TARGET} window_minutes=${WINDOW_MINUTES} limit=${LIMIT}"

status_json="$(remote_get "/v1/world/tick/status")"
bots_json="$(remote_get "/v1/bots?include_inactive=0")"
outbox_json="$(remote_get "/v1/mail/overview?folder=outbox&limit=${LIMIT}")"
kb_json="$(remote_get "/v1/kb/proposals?limit=50")"
collab_json="$(remote_get "/v1/collab/list?limit=50")"

echo
echo "== World Tick =="
echo "${status_json}" | jq -r '"tick_id=\(.tick_id) frozen=\(.frozen) last_tick_at=\(.last_tick_at) duration_ms=\(.last_duration_ms)"'

echo
echo "== Active Users =="
echo "${bots_json}" | jq -r '.items[] | "\(.user_id)\t\(.name)\t\(.status)"'
active_users=()
while IFS= read -r u; do
  [[ -n "${u}" ]] && active_users+=("${u}")
done < <(jq -r '.items[].user_id' <<<"${bots_json}")

echo
echo "== Recent Autonomous Reports (per user) =="
missing=""
for uid in "${active_users[@]}"; do
  u_outbox="$(remote_get "/v1/mail/overview?folder=outbox&user_id=${uid}&limit=${PER_USER_LIMIT}")"
  stat_line="$(
    jq -r --argjson wm "${WINDOW_MINUTES}" --arg uid "${uid}" '
      def report_subject: (.subject | type == "string") and (.subject | test("autonomy|community-collab|inbox-digest|proposal"; "i"));
      def epoch:
        if ((.sent_at | type) == "string") and (.sent_at != "") then
          (.sent_at | sub("\\.[0-9]+Z$"; "Z") | fromdateiso8601? // 0)
        else
          0
        end;
      def recent: (epoch >= (now - ($wm * 60)));
      [ .items[]
        | select(report_subject and recent)
      ] as $rows
      | if ($rows|length) == 0
        then "\($uid)\t0\t-\t-"
        else "\($uid)\t\($rows|length)\t\(($rows|sort_by(.sent_at)|last).sent_at)\t\(($rows|sort_by(.sent_at)|last).subject)"
        end
    ' <<<"${u_outbox}"
  )"
  IFS=$'\t' read -r uid_r cnt latest subj <<<"${stat_line}"
  printf '%s\tcount=%s\tlatest=%s\t%s\n' "${uid_r}" "${cnt}" "${latest}" "${subj}"
  if [[ "${cnt}" == "0" ]]; then
    missing+="${uid_r}"$'\n'
  fi
done

echo
echo "== Missing Users (no recent report) =="
missing="$(printf '%s' "${missing}" | sed '/^$/d' || true)"

if [[ -z "${missing}" ]]; then
  echo "none"
else
  printf '%s\n' "${missing}"
fi

echo
echo "== Knowledge Base Proposals =="
echo "${kb_json}" | jq -r '
  if (.items | length) == 0 then
    "none"
  else
    .items[]
    | "#\(.id) status=\(.status) proposer=\(.proposer_user_id) title=\(.title)"
  end
'

echo
echo "== Collab Sessions =="
echo "${collab_json}" | jq -r '
  if (.items | length) == 0 then
    "none"
  else
    .items[]
    | "\(.collab_id) phase=\(.phase) proposer=\(.proposer_user_id) title=\(.title)"
  end
'

if [[ -n "${missing}" ]]; then
  echo
  echo "[warn] some active users did not post autonomous report in last ${WINDOW_MINUTES} minutes" >&2
  exit 3
fi

echo
echo "[ok] autonomy monitoring passed"
