#!/usr/bin/env bash
set -euo pipefail

# Full bootstrap for a fresh environment:
# 1) create/update required secrets
# 2) deploy clawcolony stack
# 3) apply runtime env overrides
# 4) health check
# 5) register N OpenClaw users (optional)
#
# Secrets are loaded from an env file (default: .local/oneclick.env).
# Keep real credentials out of git.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

ENV_FILE="${ENV_FILE:-${ROOT_DIR}/.local/oneclick.env}"
CLAWCOLONY_NS="${CLAWCOLONY_NS:-clawcolony}"
USER_NS="${USER_NS:-freewill}"
KUBE_CONTEXT="${KUBE_CONTEXT:-}"
IMAGE="${IMAGE:-clawcolony:dev-$(date +%Y%m%d%H%M%S)}"
BUILD_IMAGE="${BUILD_IMAGE:-true}"
LOAD_MINIKUBE="${LOAD_MINIKUBE:-auto}"
WAIT_TIMEOUT="${WAIT_TIMEOUT:-300s}"
API_PORT="${API_PORT:-18080}"
AGENTS="${AGENTS:-3}"
SKIP_REGISTER="${SKIP_REGISTER:-false}"
REGISTER_TIMEOUT_SECONDS="${REGISTER_TIMEOUT_SECONDS:-2400}"
REGISTER_POLL_SECONDS="${REGISTER_POLL_SECONDS:-5}"
REGISTER_FAIL_FAST="${REGISTER_FAIL_FAST:-true}"
VERIFY_ISOLATION="${VERIFY_ISOLATION:-true}"
SPLIT_SERVICES="${SPLIT_SERVICES:-false}"
DEPLOYER_API_PORT="${DEPLOYER_API_PORT:-18081}"

usage() {
  cat <<'USAGE'
Usage:
  bootstrap_full_stack.sh [options]

Options:
  --env-file <path>           Env file with secrets/config (default: .local/oneclick.env)
  --context <name>            kubectl context
  --image <name:tag>          Clawcolony image tag
  --skip-build                Skip image build
  --load-minikube             Force minikube image load
  --skip-minikube-load        Disable minikube image load
  --clawcolony-ns <name>        Clawcolony namespace (default: clawcolony)
  --user-ns <name>            User namespace (default: freewill)
  --timeout <duration>        Rollout timeout (default: 300s)
  --api-port <port>           Local port for temporary port-forward (default: 18080)
  --deployer-api-port <port>  Local port for deployer API in split mode (default: 18081)
  --agents <n>                Number of users to register (default: 3)
  --split-services            Deploy runtime + deployer split mode
  --skip-register             Deploy only, do not register users
  --register-timeout <sec>    Timeout per register task (default: 2400)
  --register-poll <sec>       Poll interval per register task (default: 5)
  --no-fail-fast              Continue when a register task fails
  --skip-verify-isolation     Skip isolation check after register
  -h, --help                  Show help
USAGE
}

normalize_bool() {
  local v="${1:-}"
  case "$(to_lower "${v}")" in
    1|true|yes|on) echo "true" ;;
    0|false|no|off) echo "false" ;;
    *) echo "false" ;;
  esac
}

to_lower() {
  printf '%s' "${1:-}" | tr '[:upper:]' '[:lower:]'
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 1
  }
}

ts() { date -u +"%Y-%m-%dT%H:%M:%SZ"; }
log() { echo "[$(ts)] $*"; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --env-file)
      ENV_FILE="$2"; shift 2 ;;
    --context)
      KUBE_CONTEXT="$2"; shift 2 ;;
    --image)
      IMAGE="$2"; shift 2 ;;
    --skip-build)
      BUILD_IMAGE="false"; shift ;;
    --load-minikube)
      LOAD_MINIKUBE="true"; shift ;;
    --skip-minikube-load)
      LOAD_MINIKUBE="false"; shift ;;
    --clawcolony-ns)
      CLAWCOLONY_NS="$2"; shift 2 ;;
    --user-ns)
      USER_NS="$2"; shift 2 ;;
    --timeout)
      WAIT_TIMEOUT="$2"; shift 2 ;;
    --api-port)
      API_PORT="$2"; shift 2 ;;
    --deployer-api-port)
      DEPLOYER_API_PORT="$2"; shift 2 ;;
    --agents)
      AGENTS="$2"; shift 2 ;;
    --split-services)
      SPLIT_SERVICES="true"; shift ;;
    --skip-register)
      SKIP_REGISTER="true"; shift ;;
    --register-timeout)
      REGISTER_TIMEOUT_SECONDS="$2"; shift 2 ;;
    --register-poll)
      REGISTER_POLL_SECONDS="$2"; shift 2 ;;
    --no-fail-fast)
      REGISTER_FAIL_FAST="false"; shift ;;
    --skip-verify-isolation)
      VERIFY_ISOLATION="false"; shift ;;
    -h|--help)
      usage
      exit 0 ;;
    *)
      echo "unknown argument: $1" >&2
      usage
      exit 1 ;;
  esac
done

need_cmd kubectl
need_cmd curl
need_cmd jq

if [[ ! -f "${ENV_FILE}" ]]; then
  echo "env file not found: ${ENV_FILE}" >&2
  echo "create it from ${ROOT_DIR}/scripts/oneclick.env.example" >&2
  exit 1
fi

set -a
# shellcheck disable=SC1090
source "${ENV_FILE}"
set +a

SKIP_REGISTER="$(normalize_bool "${SKIP_REGISTER}")"
REGISTER_FAIL_FAST="$(normalize_bool "${REGISTER_FAIL_FAST}")"
BUILD_IMAGE="$(normalize_bool "${BUILD_IMAGE}")"
VERIFY_ISOLATION="$(normalize_bool "${VERIFY_ISOLATION}")"
SPLIT_SERVICES="$(normalize_bool "${SPLIT_SERVICES}")"

BOT_ENV_SECRET_NAME="${BOT_ENV_SECRET_NAME:-aibot-llm-secret}"
BOT_GIT_SSH_SECRET_NAME="${BOT_GIT_SSH_SECRET_NAME:-}"
BOT_GIT_SSH_HOST="${BOT_GIT_SSH_HOST:-github.com}"
BOT_DEFAULT_IMAGE="${BOT_DEFAULT_IMAGE:-openclaw:onepod-dev}"
BOT_OPENCLAW_MODEL="${BOT_OPENCLAW_MODEL:-openai/gpt-5.1-codex}"
CLAWCOLONY_DEPLOYER_API_BASE_URL="${CLAWCOLONY_DEPLOYER_API_BASE_URL:-http://clawcolony-deployer.clawcolony.svc.cluster.local:8080}"
GITHUB_API_MOCK_ENABLED="${GITHUB_API_MOCK_ENABLED:-false}"
GITHUB_API_MOCK_OWNER="${GITHUB_API_MOCK_OWNER:-clawcolony}"
GITHUB_API_MOCK_MACHINE_USER="${GITHUB_API_MOCK_MACHINE_USER:-claw-archivist}"
GITHUB_API_MOCK_RELEASE_TAG="${GITHUB_API_MOCK_RELEASE_TAG:-v2026.3.1}"

if [[ -z "${OPENAI_API_KEY:-}" ]]; then
  echo "OPENAI_API_KEY is required in ${ENV_FILE}" >&2
  exit 1
fi
if [[ -z "${UPGRADE_INTERNAL_TOKEN:-}" ]]; then
  echo "UPGRADE_INTERNAL_TOKEN is required in ${ENV_FILE}" >&2
  exit 1
fi
if [[ "$(to_lower "${GITHUB_API_MOCK_ENABLED}")" != "true" ]]; then
  if [[ -z "${GITHUB_TOKEN:-}" || -z "${GITHUB_OWNER:-}" || -z "${GITHUB_MACHINE_USER:-}" ]]; then
    echo "GITHUB_TOKEN, GITHUB_OWNER, GITHUB_MACHINE_USER are required when GITHUB_API_MOCK_ENABLED=false" >&2
    exit 1
  fi
fi
if ! [[ "${AGENTS}" =~ ^[0-9]+$ ]]; then
  echo "invalid --agents value: ${AGENTS}" >&2
  exit 1
fi
if ! [[ "${REGISTER_TIMEOUT_SECONDS}" =~ ^[0-9]+$ ]]; then
  echo "invalid --register-timeout value: ${REGISTER_TIMEOUT_SECONDS}" >&2
  exit 1
fi
if ! [[ "${REGISTER_POLL_SECONDS}" =~ ^[0-9]+$ ]]; then
  echo "invalid --register-poll value: ${REGISTER_POLL_SECONDS}" >&2
  exit 1
fi
if ! [[ "${DEPLOYER_API_PORT}" =~ ^[0-9]+$ ]]; then
  echo "invalid --deployer-api-port value: ${DEPLOYER_API_PORT}" >&2
  exit 1
fi

if [[ -n "${KUBE_CONTEXT}" ]]; then
  log "switch kubectl context -> ${KUBE_CONTEXT}"
  kubectl config use-context "${KUBE_CONTEXT}" >/dev/null
fi
CURRENT_CONTEXT="$(kubectl config current-context)"
log "current context: ${CURRENT_CONTEXT}"

if [[ "${LOAD_MINIKUBE}" == "auto" ]]; then
  if [[ "${CURRENT_CONTEXT}" == *"minikube"* ]] && command -v minikube >/dev/null 2>&1; then
    LOAD_MINIKUBE="true"
  else
    LOAD_MINIKUBE="false"
  fi
fi

upsert_secret_literals() {
  local ns="$1"
  local name="$2"
  shift 2
  local args=()
  local pair
  for pair in "$@"; do
    args+=(--from-literal="${pair}")
  done
  kubectl -n "${ns}" create secret generic "${name}" "${args[@]}" --dry-run=client -o yaml | kubectl apply -f -
}

log "upsert secret ${USER_NS}/${BOT_ENV_SECRET_NAME}"
upsert_secret_literals "${USER_NS}" "${BOT_ENV_SECRET_NAME}" \
  "OPENAI_API_KEY=${OPENAI_API_KEY}"

upgrade_pairs=("UPGRADE_INTERNAL_TOKEN=${UPGRADE_INTERNAL_TOKEN}")
if [[ -n "${UPGRADE_REPO_USER:-}" ]]; then
  upgrade_pairs+=("UPGRADE_REPO_USER=${UPGRADE_REPO_USER}")
fi
if [[ -n "${UPGRADE_REPO_TOKEN:-}" ]]; then
  upgrade_pairs+=("UPGRADE_REPO_TOKEN=${UPGRADE_REPO_TOKEN}")
fi
log "upsert secret ${CLAWCOLONY_NS}/clawcolony-upgrade-secret"
upsert_secret_literals "${CLAWCOLONY_NS}" "clawcolony-upgrade-secret" "${upgrade_pairs[@]}"

if [[ "$(to_lower "${GITHUB_API_MOCK_ENABLED}")" != "true" ]]; then
  log "upsert secret ${CLAWCOLONY_NS}/clawcolony-github"
  upsert_secret_literals "${CLAWCOLONY_NS}" "clawcolony-github" \
    "GITHUB_TOKEN=${GITHUB_TOKEN}" \
    "GITHUB_OWNER=${GITHUB_OWNER}" \
    "GITHUB_MACHINE_USER=${GITHUB_MACHINE_USER}"
else
  log "github mock mode enabled, skip required github token secret"
fi

deploy_cmd=("${SCRIPT_DIR}/deploy_dev_server.sh"
  "--image" "${IMAGE}"
  "--clawcolony-ns" "${CLAWCOLONY_NS}"
  "--user-ns" "${USER_NS}"
  "--timeout" "${WAIT_TIMEOUT}")
if [[ "${BUILD_IMAGE}" == "false" ]]; then
  deploy_cmd+=("--skip-build")
fi
if [[ "${LOAD_MINIKUBE}" == "true" ]]; then
  deploy_cmd+=("--load-minikube")
elif [[ "${LOAD_MINIKUBE}" == "false" ]]; then
  deploy_cmd+=("--skip-minikube-load")
fi
if [[ -n "${KUBE_CONTEXT}" ]]; then
  deploy_cmd+=("--context" "${KUBE_CONTEXT}")
fi
if [[ "${SPLIT_SERVICES}" == "true" ]]; then
  deploy_cmd+=("--split-services")
fi

log "deploy base stack"
"${deploy_cmd[@]}"

set_env_args=(
  "BOT_ENV_SECRET_NAME=${BOT_ENV_SECRET_NAME}"
  "BOT_GIT_SSH_SECRET_NAME=${BOT_GIT_SSH_SECRET_NAME}"
  "BOT_GIT_SSH_HOST=${BOT_GIT_SSH_HOST}"
  "BOT_DEFAULT_IMAGE=${BOT_DEFAULT_IMAGE}"
  "BOT_OPENCLAW_MODEL=${BOT_OPENCLAW_MODEL}"
  "CLAWCOLONY_DEPLOYER_API_BASE_URL=${CLAWCOLONY_DEPLOYER_API_BASE_URL}"
  "GITHUB_API_MOCK_ENABLED=${GITHUB_API_MOCK_ENABLED}"
  "GITHUB_API_MOCK_OWNER=${GITHUB_API_MOCK_OWNER}"
  "GITHUB_API_MOCK_MACHINE_USER=${GITHUB_API_MOCK_MACHINE_USER}"
  "GITHUB_API_MOCK_RELEASE_TAG=${GITHUB_API_MOCK_RELEASE_TAG}"
)
if [[ -n "${UPGRADE_REPO_URL:-}" ]]; then
  set_env_args+=("UPGRADE_REPO_URL=${UPGRADE_REPO_URL}")
fi

if [[ "${SPLIT_SERVICES}" == "true" ]]; then
  log "apply env overrides to deployment/clawcolony-runtime"
  kubectl -n "${CLAWCOLONY_NS}" set env deployment/clawcolony-runtime "${set_env_args[@]}" >/dev/null
  kubectl -n "${CLAWCOLONY_NS}" rollout status deployment/clawcolony-runtime --timeout="${WAIT_TIMEOUT}"
  log "apply env overrides to deployment/clawcolony-deployer"
  kubectl -n "${CLAWCOLONY_NS}" set env deployment/clawcolony-deployer "${set_env_args[@]}" >/dev/null
  kubectl -n "${CLAWCOLONY_NS}" rollout status deployment/clawcolony-deployer --timeout="${WAIT_TIMEOUT}"
else
  log "apply runtime env overrides to deployment/clawcolony"
  kubectl -n "${CLAWCOLONY_NS}" set env deployment/clawcolony "${set_env_args[@]}" >/dev/null
  kubectl -n "${CLAWCOLONY_NS}" rollout status deployment/clawcolony --timeout="${WAIT_TIMEOUT}"
fi

PF_LOG="${ROOT_DIR}/.local/bootstrap_portforward.log"
PF_DEPLOYER_LOG="${ROOT_DIR}/.local/bootstrap_portforward_deployer.log"
mkdir -p "$(dirname "${PF_LOG}")"

PF_PID=""
PF_DEPLOYER_PID=""
cleanup() {
  if [[ -n "${PF_PID}" ]] && kill -0 "${PF_PID}" >/dev/null 2>&1; then
    kill "${PF_PID}" >/dev/null 2>&1 || true
  fi
  if [[ -n "${PF_DEPLOYER_PID}" ]] && kill -0 "${PF_DEPLOYER_PID}" >/dev/null 2>&1; then
    kill "${PF_DEPLOYER_PID}" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

log "start temporary port-forward 127.0.0.1:${API_PORT} -> svc/clawcolony:8080"
kubectl -n "${CLAWCOLONY_NS}" port-forward svc/clawcolony "${API_PORT}:8080" >"${PF_LOG}" 2>&1 &
PF_PID=$!
if [[ "${SPLIT_SERVICES}" == "true" ]]; then
  log "start temporary port-forward 127.0.0.1:${DEPLOYER_API_PORT} -> svc/clawcolony-deployer:8080"
  kubectl -n "${CLAWCOLONY_NS}" port-forward svc/clawcolony-deployer "${DEPLOYER_API_PORT}:8080" >"${PF_DEPLOYER_LOG}" 2>&1 &
  PF_DEPLOYER_PID=$!
fi

for _ in $(seq 1 60); do
  if curl -fsS "http://127.0.0.1:${API_PORT}/healthz" >/dev/null 2>&1; then
    break
  fi
  sleep 2
done
curl -fsS "http://127.0.0.1:${API_PORT}/healthz" >/dev/null
log "api health check passed"
if [[ "${SPLIT_SERVICES}" == "true" ]]; then
  for _ in $(seq 1 60); do
    if curl -fsS "http://127.0.0.1:${DEPLOYER_API_PORT}/healthz" >/dev/null 2>&1; then
      break
    fi
    sleep 2
  done
  curl -fsS "http://127.0.0.1:${DEPLOYER_API_PORT}/healthz" >/dev/null
  log "deployer api health check passed"
fi

if [[ "${SKIP_REGISTER}" == "true" || "${AGENTS}" == "0" ]]; then
  log "skip register stage"
  log "done. dashboard: http://127.0.0.1:${API_PORT}/dashboard (while this script is running)"
  if [[ "${SPLIT_SERVICES}" == "true" ]]; then
    log "deployer base: http://127.0.0.1:${DEPLOYER_API_PORT}"
  fi
  exit 0
fi

register_one() {
  local idx="$1"
  local resp task_id status now started elapsed user_id user_name last_step_msg
  local register_base="http://127.0.0.1:${API_PORT}"
  if [[ "${SPLIT_SERVICES}" == "true" ]]; then
    register_base="http://127.0.0.1:${DEPLOYER_API_PORT}"
  fi
  resp="$(curl -fsS -X POST "${register_base}/v1/openclaw/admin/action" \
    -H "Content-Type: application/json" \
    -d '{"action":"register"}')"
  task_id="$(echo "${resp}" | jq -r '.register_task_id // empty')"
  if [[ -z "${task_id}" ]]; then
    echo "register(${idx}) failed: no register_task_id in response: ${resp}" >&2
    return 1
  fi
  log "register(${idx}) task accepted: register_task_id=${task_id}"

  started="$(date +%s)"
  while true; do
    status="$(curl -fsS "${register_base}/v1/openclaw/admin/register/task?register_task_id=${task_id}")"
    user_id="$(echo "${status}" | jq -r '.task.user_id // empty')"
    user_name="$(echo "${status}" | jq -r '.task.user_name // empty')"
    last_step_msg="$(echo "${status}" | jq -r '.last_step.message // empty')"
    case "$(echo "${status}" | jq -r '.task.status // empty')" in
      succeeded)
        log "register(${idx}) succeeded: user_id=${user_id} user_name=${user_name}"
        echo "${user_id}"
        return 0
        ;;
      failed)
        echo "register(${idx}) failed: task_id=${task_id} reason=${last_step_msg}" >&2
        return 1
        ;;
    esac
    now="$(date +%s)"
    elapsed="$((now - started))"
    if (( elapsed >= REGISTER_TIMEOUT_SECONDS )); then
      echo "register(${idx}) timeout after ${REGISTER_TIMEOUT_SECONDS}s: task_id=${task_id}" >&2
      return 1
    fi
    sleep "${REGISTER_POLL_SECONDS}"
  done
}

success_count=0
failed_count=0
registered_users=()

for i in $(seq 1 "${AGENTS}"); do
  if uid="$(register_one "${i}")"; then
    success_count="$((success_count + 1))"
    registered_users+=("${uid}")
  else
    failed_count="$((failed_count + 1))"
    if [[ "${REGISTER_FAIL_FAST}" == "true" ]]; then
      break
    fi
  fi
done

log "register summary: success=${success_count} failed=${failed_count}"
if [[ "${#registered_users[@]}" -gt 0 ]]; then
  log "registered users: ${registered_users[*]}"
fi

log "open dashboard:"
log "  http://127.0.0.1:${API_PORT}/dashboard"
log "openclaw overview:"
if [[ "${SPLIT_SERVICES}" == "true" ]]; then
  log "  http://127.0.0.1:${DEPLOYER_API_PORT}/v1/openclaw/admin/overview"
else
  log "  http://127.0.0.1:${API_PORT}/v1/openclaw/admin/overview"
fi

if (( failed_count > 0 )); then
  exit 1
fi

if [[ "${VERIFY_ISOLATION}" == "true" ]]; then
  log "run post-register isolation check"
  verify_cmd=("${SCRIPT_DIR}/check_agent_isolation.sh" "--namespace" "${USER_NS}" "--use-minikube" "auto")
  if [[ -n "${KUBE_CONTEXT}" ]]; then
    verify_cmd+=("--context" "${KUBE_CONTEXT}")
  fi
  "${verify_cmd[@]}"
fi

log "done."
