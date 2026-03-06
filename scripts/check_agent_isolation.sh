#!/usr/bin/env bash
set -euo pipefail

NS="${NS:-freewill}"
KUBE_CONTEXT="${KUBE_CONTEXT:-}"
USE_MINIKUBE="${USE_MINIKUBE:-auto}"

usage() {
  cat <<USAGE
Usage: $(basename "$0") [--namespace <name>] [--context <name>] [--use-minikube <auto|true|false>]

Checks per deployment in namespace:
- unique user_id
- unique readable name (CLAWCOLONY_USER_NAME)
- per-user git secret is set
- git secret is NOT global aibot-git-ssh
- git secret matches aibot-git-<user_id>
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --namespace)
      NS="$2"; shift 2 ;;
    --context)
      KUBE_CONTEXT="$2"; shift 2 ;;
    --use-minikube)
      USE_MINIKUBE="$2"; shift 2 ;;
    -h|--help)
      usage; exit 0 ;;
    *)
      echo "unknown arg: $1" >&2
      usage
      exit 1 ;;
  esac
done

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

tmp_rows="$(mktemp)"
tmp_deps="$(mktemp)"
trap 'rm -f "${tmp_rows}" "${tmp_deps}"' EXIT

k -n "${NS}" get deploy -o name >"${tmp_deps}" 2>/dev/null || true
dep_count="$(awk 'NF{c++} END{print c+0}' "${tmp_deps}")"
if [[ "${dep_count}" == "0" ]]; then
  echo "no deployments found in namespace ${NS}" >&2
  exit 1
fi

failed=0
echo "checking ${dep_count} deployments in namespace ${NS}"

while IFS= read -r d; do
  [[ -z "${d}" ]] && continue
  uid="$(k -n "${NS}" get "${d}" -o jsonpath='{.metadata.labels.clawcolony\.user_id}')"
  name="$(k -n "${NS}" get "${d}" -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="CLAWCOLONY_USER_NAME")].value}')"
  gsecret="$(k -n "${NS}" get "${d}" -o jsonpath='{.spec.template.spec.volumes[?(@.name=="bot-git-ssh")].secret.secretName}')"

  printf '%s | user_id=%s | name=%s | git_secret=%s\n' "${d#deployment.apps/}" "${uid:-<empty>}" "${name:-<empty>}" "${gsecret:-<empty>}"
  printf '%s|%s|%s|%s\n' "${d#deployment.apps/}" "${uid}" "${name}" "${gsecret}" >>"${tmp_rows}"

  if [[ -z "${uid}" || -z "${name}" || -z "${gsecret}" ]]; then
    echo "  [FAIL] empty field detected in ${d#deployment.apps/}" >&2
    failed=1
  fi
  if [[ "${gsecret}" == "aibot-git-ssh" ]]; then
    echo "  [FAIL] global git secret detected in ${d#deployment.apps/}" >&2
    failed=1
  fi
  expect="aibot-git-${uid}"
  if [[ -n "${uid}" && "${gsecret}" != "${expect}" ]]; then
    echo "  [FAIL] git secret mismatch in ${d#deployment.apps/}: expect ${expect}" >&2
    failed=1
  fi
done <"${tmp_deps}"

dup_uid="$(awk -F'|' '{print $2}' "${tmp_rows}" | awk 'NF' | sort | uniq -d)"
dup_name="$(awk -F'|' '{print $3}' "${tmp_rows}" | awk 'NF' | sort | uniq -d)"
dup_secret="$(awk -F'|' '{print $4}' "${tmp_rows}" | awk 'NF' | sort | uniq -d)"

if [[ -n "${dup_uid}" ]]; then
  echo "[FAIL] duplicate user_id:" >&2
  echo "${dup_uid}" >&2
  failed=1
fi
if [[ -n "${dup_name}" ]]; then
  echo "[FAIL] duplicate user name:" >&2
  echo "${dup_name}" >&2
  failed=1
fi
if [[ -n "${dup_secret}" ]]; then
  echo "[FAIL] duplicate git secret:" >&2
  echo "${dup_secret}" >&2
  failed=1
fi

if [[ "${failed}" == "1" ]]; then
  echo "isolation check failed" >&2
  exit 1
fi

echo "isolation check passed"
