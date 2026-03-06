#!/usr/bin/env bash
set -euo pipefail

# Deploy Clawcolony to a dev Kubernetes cluster.
#
# Features:
# - clear step-by-step output
# - optional image build
# - optional minikube image load
# - namespace/bootstrap manifests apply
# - rollout wait checks
# - post-deploy checks and operator hints
#
# Usage examples:
#   ./scripts/deploy_dev_server.sh
#   ./scripts/deploy_dev_server.sh --image clawcolony:dev-20260302 --skip-build
#   ./scripts/deploy_dev_server.sh --context minikube

CLAWCOLONY_NS="clawcolony"
RUNTIME_NS="freewill"
USER_NS="freewill"
KUBE_CONTEXT=""
WAIT_TIMEOUT="300s"
IMAGE="clawcolony:dev-$(date +%Y%m%d%H%M%S)"
BUILD_IMAGE="true"
LOAD_TO_MINIKUBE="auto"

usage() {
  cat <<'USAGE'
Usage:
  deploy_dev_server.sh [options]

Options:
  --image <name:tag>       Container image used by clawcolony deployment.
  --skip-build             Skip docker build step.
  --load-minikube          Force minikube image load step.
  --skip-minikube-load     Disable minikube image load step.
  --context <name>         kubectl context to use.
  --clawcolony-ns <name>     Clawcolony namespace (default: clawcolony).
  --runtime-ns <name>      Runtime namespace (default: freewill).
  --user-ns <name>         User namespace for agents (default: freewill).
  --timeout <duration>     Rollout wait timeout (default: 300s).
  -h, --help               Show this help.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --image)
      IMAGE="${2:?missing value for --image}"
      shift 2
      ;;
    --skip-build)
      BUILD_IMAGE="false"
      shift
      ;;
    --load-minikube)
      LOAD_TO_MINIKUBE="true"
      shift
      ;;
    --skip-minikube-load)
      LOAD_TO_MINIKUBE="false"
      shift
      ;;
    --context)
      KUBE_CONTEXT="${2:?missing value for --context}"
      shift 2
      ;;
    --clawcolony-ns)
      CLAWCOLONY_NS="${2:?missing value for --clawcolony-ns}"
      shift 2
      ;;
    --runtime-ns)
      RUNTIME_NS="${2:?missing value for --runtime-ns}"
      shift 2
      ;;
    --user-ns)
      USER_NS="${2:?missing value for --user-ns}"
      shift 2
      ;;
    --timeout)
      WAIT_TIMEOUT="${2:?missing value for --timeout}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1"
      usage
      exit 1
      ;;
  esac
done

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1"
    exit 1
  }
}

need_cmd kubectl
need_cmd sed
if [[ "$BUILD_IMAGE" == "true" || "$LOAD_TO_MINIKUBE" != "false" ]]; then
  need_cmd docker
fi

if [[ -n "$KUBE_CONTEXT" ]]; then
  echo "[0/9] switch kubectl context -> $KUBE_CONTEXT"
  kubectl config use-context "$KUBE_CONTEXT" >/dev/null
fi

CURRENT_CONTEXT="$(kubectl config current-context)"
echo "[context] current kubectl context: ${CURRENT_CONTEXT}"

if [[ "$LOAD_TO_MINIKUBE" == "auto" ]]; then
  if [[ "$CURRENT_CONTEXT" == *"minikube"* ]] && command -v minikube >/dev/null 2>&1; then
    LOAD_TO_MINIKUBE="true"
  else
    LOAD_TO_MINIKUBE="false"
  fi
fi

if [[ "$BUILD_IMAGE" == "true" ]]; then
  echo "[1/9] build clawcolony image: ${IMAGE}"
  docker build -t "${IMAGE}" .
else
  echo "[1/9] skip image build, use existing image: ${IMAGE}"
fi

if [[ "$LOAD_TO_MINIKUBE" == "true" ]]; then
  need_cmd minikube
  echo "[2/9] load image to minikube cache: ${IMAGE}"
  minikube image load "${IMAGE}"
else
  echo "[2/9] skip minikube image load"
fi

echo "[3/9] apply base manifests (namespaces/nats/postgres/rbac)"
kubectl apply -f k8s/namespaces.yaml
kubectl apply -f k8s/nats.yaml
kubectl apply -f k8s/postgres.yaml
kubectl apply -f k8s/rbac.yaml

echo "[4/9] wait NATS rollout"
kubectl -n "${CLAWCOLONY_NS}" rollout status statefulset/clawcolony-nats --timeout="${WAIT_TIMEOUT}"

echo "[5/9] wait PostgreSQL rollout"
kubectl -n "${CLAWCOLONY_NS}" rollout status statefulset/clawcolony-postgres --timeout="${WAIT_TIMEOUT}"

echo "[6/9] deploy clawcolony runtime service"
sed "s|{{CLAWCOLONY_IMAGE}}|${IMAGE}|g" k8s/clawcolony-runtime-deployment.yaml | kubectl apply -f -
kubectl apply -f k8s/service-runtime.yaml

echo "[7/9] wait clawcolony runtime rollout"
kubectl -n "${RUNTIME_NS}" rollout status deployment/clawcolony-runtime --timeout="${WAIT_TIMEOUT}"

echo "[8/9] quick status"
kubectl -n "${CLAWCOLONY_NS}" get pods -o wide
kubectl -n "${RUNTIME_NS}" get pods -o wide

echo "[9/9] dependency secrets check (agent runtime)"
if ! kubectl -n "${USER_NS}" get secret aibot-llm-secret >/dev/null 2>&1; then
  echo "WARN: secret ${USER_NS}/aibot-llm-secret is missing; newly created agents may fail to start."
fi
if ! kubectl -n "${CLAWCOLONY_NS}" get secret clawcolony-upgrade-secret >/dev/null 2>&1; then
  echo "WARN: secret ${CLAWCOLONY_NS}/clawcolony-upgrade-secret is missing; upgrade API auth/repo token may be unavailable."
fi
echo "INFO: per-user git secret is provisioned during register flow (aibot-git-<user_id>)."
echo "INFO: after registering users, run: ./scripts/check_agent_isolation.sh --namespace ${USER_NS}"

echo
echo "Deploy complete."
echo "Image: ${IMAGE}"
echo
echo "Useful checks:"
echo "  kubectl -n ${RUNTIME_NS} get pods"
echo "  kubectl -n ${RUNTIME_NS} logs deploy/clawcolony-runtime --tail=200"
echo
echo "Dashboard (via port-forward):"
echo "  kubectl -n ${RUNTIME_NS} port-forward svc/clawcolony 8080:8080"
echo "  open: http://127.0.0.1:8080/dashboard"
