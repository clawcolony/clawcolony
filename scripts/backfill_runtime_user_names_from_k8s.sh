#!/usr/bin/env bash
set -euo pipefail

NS="${NS:-freewill}"
DB_NAME="${DB_NAME:-clawcolony_runtime}"
POSTGRES_SECRET_NAME="${POSTGRES_SECRET_NAME:-clawcolony-postgres}"
POSTGRES_SERVICE_NAME="${POSTGRES_SERVICE_NAME:-clawcolony-postgres}"
POSTGRES_USER="${POSTGRES_USER:-}"
KUBE_CONTEXT="${KUBE_CONTEXT:-}"
USE_MINIKUBE="${USE_MINIKUBE:-auto}"
APPLY=0

usage() {
  cat <<USAGE
Usage: $(basename "$0") [options]

One-time backfill for runtime user_name:
- Source: deployment label/env in namespace ${NS}
  - label: clawcolony.user_id
  - env:   CLAWCOLONY_USER_NAME
- Target: freewill runtime DB (${DB_NAME}) table user_accounts
- Update condition: user_name is empty OR user_name == user_id

Options:
  --namespace <ns>              Kubernetes namespace (default: ${NS})
  --db-name <name>              Runtime DB name (default: ${DB_NAME})
  --postgres-secret <name>      Postgres secret (default: ${POSTGRES_SECRET_NAME})
  --postgres-service <name>     Postgres service/pod label app value (default: ${POSTGRES_SERVICE_NAME})
  --postgres-user <name>        Postgres user (default: read from secret POSTGRES_USER)
  --context <name>              kubectl context
  --use-minikube <auto|true|false>
  --apply                       Apply updates (default: dry-run preview only)
  -h, --help                    Show help
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --namespace)
      NS="$2"; shift 2 ;;
    --db-name)
      DB_NAME="$2"; shift 2 ;;
    --postgres-secret)
      POSTGRES_SECRET_NAME="$2"; shift 2 ;;
    --postgres-service)
      POSTGRES_SERVICE_NAME="$2"; shift 2 ;;
    --postgres-user)
      POSTGRES_USER="$2"; shift 2 ;;
    --context)
      KUBE_CONTEXT="$2"; shift 2 ;;
    --use-minikube)
      USE_MINIKUBE="$2"; shift 2 ;;
    --apply)
      APPLY=1; shift ;;
    -h|--help)
      usage; exit 0 ;;
    *)
      echo "unknown arg: $1" >&2
      usage
      exit 1 ;;
  esac
done

if ! command -v kubectl >/dev/null 2>&1; then
  echo "kubectl is required" >&2
  exit 1
fi

run_kubectl() {
  if [[ -n "${KUBE_CONTEXT}" ]]; then
    kubectl --context "${KUBE_CONTEXT}" "$@"
  else
    kubectl "$@"
  fi
}

if [[ "${USE_MINIKUBE}" == "auto" ]]; then
  if command -v minikube >/dev/null 2>&1; then
    ctx="$(kubectl config current-context 2>/dev/null || true)"
    if [[ "${ctx}" == *"minikube"* ]]; then
      USE_MINIKUBE="true"
    else
      USE_MINIKUBE="false"
    fi
  else
    USE_MINIKUBE="false"
  fi
fi

if [[ "${USE_MINIKUBE}" == "true" ]]; then
  k() { minikube kubectl -- "$@"; }
else
  k() { run_kubectl "$@"; }
fi

tmp_deps="$(mktemp)"
tmp_rows="$(mktemp)"
trap 'rm -f "${tmp_deps}" "${tmp_rows}"' EXIT

k -n "${NS}" get deploy -l app=aibot -o name >"${tmp_deps}" 2>/dev/null || true

if [[ ! -s "${tmp_deps}" ]]; then
  echo "no app=aibot deployments found in namespace ${NS}" >&2
  exit 1
fi

while IFS= read -r dep; do
  [[ -z "${dep}" ]] && continue
  uid="$(k -n "${NS}" get "${dep}" -o jsonpath='{.metadata.labels.clawcolony\.user_id}' 2>/dev/null || true)"
  name="$(k -n "${NS}" get "${dep}" -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="CLAWCOLONY_USER_NAME")].value}' 2>/dev/null || true)"

  uid="$(echo "${uid}" | xargs)"
  name="$(echo "${name}" | xargs)"
  name="${name//$'\r'/ }"
  name="${name//$'\n'/ }"
  name="${name//$'\t'/ }"
  name="$(echo "${name}" | xargs)"

  if [[ -z "${uid}" || -z "${name}" ]]; then
    continue
  fi
  if [[ "${name}" == "${uid}" ]]; then
    continue
  fi
  printf '%s\t%s\n' "${uid}" "${name}" >>"${tmp_rows}"
done <"${tmp_deps}"

if [[ ! -s "${tmp_rows}" ]]; then
  echo "no usable user_id -> user_name candidates found from deployments"
  exit 0
fi

tmp_unique="$(mktemp)"
trap 'rm -f "${tmp_deps}" "${tmp_rows}" "${tmp_unique}"' EXIT

if ! awk -F '\t' '
  NF < 2 { next }
  {
    uid = $1
    name = $2
    if (!(uid in seen)) {
      seen[uid] = name
      order[++n] = uid
      next
    }
    if (seen[uid] != name) {
      printf "conflicting CLAWCOLONY_USER_NAME for user_id=%s: %s vs %s\n", uid, seen[uid], name > "/dev/stderr"
      conflict = 1
    }
  }
  END {
    if (conflict) exit 2
    for (i = 1; i <= n; i++) {
      uid = order[i]
      printf "%s\t%s\n", uid, seen[uid]
    }
  }
' "${tmp_rows}" >"${tmp_unique}"; then
  echo "candidate map has conflicts, abort" >&2
  exit 1
fi

if [[ ! -s "${tmp_unique}" ]]; then
  echo "candidate map is empty after de-dup"
  exit 0
fi

pg_pass="$(k -n "${NS}" get secret "${POSTGRES_SECRET_NAME}" -o jsonpath='{.data.POSTGRES_PASSWORD}' | base64 --decode)"
if [[ -z "${POSTGRES_USER}" ]]; then
  POSTGRES_USER="$(k -n "${NS}" get secret "${POSTGRES_SECRET_NAME}" -o jsonpath='{.data.POSTGRES_USER}' | base64 --decode)"
fi
POSTGRES_USER="${POSTGRES_USER:-clawcolony}"

pg_pod="$(k -n "${NS}" get pod -l "app=${POSTGRES_SERVICE_NAME}" -o jsonpath='{.items[0].metadata.name}')"
if [[ -z "${pg_pod}" ]]; then
  echo "failed to locate postgres pod with label app=${POSTGRES_SERVICE_NAME} in namespace ${NS}" >&2
  exit 1
fi
pg_host="${POSTGRES_SERVICE_NAME}.${NS}.svc.cluster.local"

values_sql=""
while IFS=$'\t' read -r uid name; do
  [[ -z "${uid}" || -z "${name}" ]] && continue
  uid_esc="${uid//\'/\'\'}"
  name_esc="${name//\'/\'\'}"
  values_sql+=$'\n'"  ('${uid_esc}','${name_esc}'),"
done <"${tmp_unique}"
values_sql="${values_sql%,}"

if [[ -z "${values_sql}" ]]; then
  echo "no SQL values generated"
  exit 0
fi

echo "[info] namespace=${NS} db=${DB_NAME} pg_pod=${pg_pod}"
echo "[info] candidates=$(wc -l <"${tmp_unique}" | xargs)"

echo "[preview] rows eligible for backfill (current user_name is empty or equals user_id):"
k -n "${NS}" exec "${pg_pod}" -- sh -lc "PGPASSWORD='${pg_pass}' psql -U '${POSTGRES_USER}' -h '${pg_host}' -d '${DB_NAME}' -v ON_ERROR_STOP=1 -P pager=off <<'SQL'
WITH incoming(user_id, user_name) AS (VALUES${values_sql}
), matched AS (
  SELECT ua.user_id,
         ua.user_name AS current_user_name,
         i.user_name  AS target_user_name,
         ua.status
  FROM user_accounts ua
  JOIN incoming i ON i.user_id = ua.user_id
  WHERE btrim(ua.user_name) = '' OR btrim(ua.user_name) = ua.user_id
)
SELECT * FROM matched ORDER BY user_id;
SQL"

echo "[preview] candidates missing in runtime user_accounts (not updated):"
k -n "${NS}" exec "${pg_pod}" -- sh -lc "PGPASSWORD='${pg_pass}' psql -U '${POSTGRES_USER}' -h '${pg_host}' -d '${DB_NAME}' -v ON_ERROR_STOP=1 -P pager=off <<'SQL'
WITH incoming(user_id, user_name) AS (VALUES${values_sql}
)
SELECT i.user_id, i.user_name
FROM incoming i
LEFT JOIN user_accounts ua ON ua.user_id = i.user_id
WHERE ua.user_id IS NULL
ORDER BY i.user_id;
SQL"

if [[ "${APPLY}" != "1" ]]; then
  echo "[dry-run] no change applied. Re-run with --apply to execute update."
  exit 0
fi

echo "[apply] updating runtime user_accounts..."
k -n "${NS}" exec "${pg_pod}" -- sh -lc "PGPASSWORD='${pg_pass}' psql -U '${POSTGRES_USER}' -h '${pg_host}' -d '${DB_NAME}' -v ON_ERROR_STOP=1 -P pager=off <<'SQL'
WITH incoming(user_id, user_name) AS (VALUES${values_sql}
), updated AS (
  UPDATE user_accounts ua
  SET user_name = i.user_name,
      updated_at = NOW()
  FROM incoming i
  WHERE ua.user_id = i.user_id
    AND (btrim(ua.user_name) = '' OR btrim(ua.user_name) = ua.user_id)
  RETURNING ua.user_id
)
SELECT count(*) AS updated_rows FROM updated;
SQL"

echo "[done] backfill complete"
