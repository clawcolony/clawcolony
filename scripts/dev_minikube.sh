#!/usr/bin/env bash
set -euo pipefail

IMAGE="${1:-clawcolony:dev}"
RUNTIME_NAMESPACE="${RUNTIME_NAMESPACE:-freewill}"
LEGACY_RUNTIME_NAMESPACE="${LEGACY_RUNTIME_NAMESPACE:-clawcolony}"
RUNTIME_SERVICE_NAME="${RUNTIME_SERVICE_NAME:-clawcolony}"
CLEANUP_LEGACY_RUNTIME="${CLEANUP_LEGACY_RUNTIME:-true}"
MIGRATE_EXISTING_AGENTS="${MIGRATE_EXISTING_AGENTS:-true}"
RUNTIME_DB_NAME="${RUNTIME_DB_NAME:-clawcolony_runtime}"
RUNTIME_DB_HOST="${RUNTIME_DB_HOST:-clawcolony-postgres.${RUNTIME_NAMESPACE}.svc.cluster.local}"
RUNTIME_DB_PORT="${RUNTIME_DB_PORT:-5432}"
CLAWCOLONY_INTERNAL_SYNC_TOKEN="${CLAWCOLONY_INTERNAL_SYNC_TOKEN:-clawcolony-internal-sync-dev}"
RUNTIME_BASE_URL="http://${RUNTIME_SERVICE_NAME}.${RUNTIME_NAMESPACE}.svc.cluster.local:8080"
LEGACY_RUNTIME_BASE_URL="http://${RUNTIME_SERVICE_NAME}.${LEGACY_RUNTIME_NAMESPACE}.svc.cluster.local:8080"

if command -v kubectl >/dev/null 2>&1; then
  kctl() { kubectl "$@"; }
elif command -v minikube >/dev/null 2>&1; then
  kctl() { minikube kubectl -- "$@"; }
else
  echo "error: kubectl/minikube not found" >&2
  exit 1
fi

echo "[1/11] ensure minikube is running"
minikube status >/dev/null

echo "[2/11] build local image: ${IMAGE}"
docker build -t "${IMAGE}" .

echo "[3/11] load image into minikube"
minikube image load "${IMAGE}"

echo "[4/11] deploy runtime postgres + rbac"
kctl apply -f k8s/postgres.yaml
kctl apply -f k8s/rbac.yaml

echo "[5/11] wait runtime postgres ready"
kctl -n "${RUNTIME_NAMESPACE}" rollout status statefulset/clawcolony-postgres --timeout=120s

echo "[6/11] upsert runtime secret ${RUNTIME_NAMESPACE}/clawcolony-runtime"
PG_USER="$(kctl -n "${RUNTIME_NAMESPACE}" get secret clawcolony-postgres -o jsonpath='{.data.POSTGRES_USER}' | base64 --decode 2>/dev/null || true)"
PG_PASSWORD="$(kctl -n "${RUNTIME_NAMESPACE}" get secret clawcolony-postgres -o jsonpath='{.data.POSTGRES_PASSWORD}' | base64 --decode 2>/dev/null || true)"
if [[ -z "${PG_USER}" ]]; then
  PG_USER="clawcolony"
fi
if [[ -z "${PG_PASSWORD}" ]]; then
  PG_PASSWORD="clawcolony"
fi
RUNTIME_DATABASE_URL="${RUNTIME_DATABASE_URL:-postgres://${PG_USER}:${PG_PASSWORD}@${RUNTIME_DB_HOST}:${RUNTIME_DB_PORT}/${RUNTIME_DB_NAME}?sslmode=disable}"
kctl -n "${RUNTIME_NAMESPACE}" create secret generic clawcolony-runtime \
  --from-literal="DATABASE_URL=${RUNTIME_DATABASE_URL}" \
  --from-literal="CLAWCOLONY_INTERNAL_SYNC_TOKEN=${CLAWCOLONY_INTERNAL_SYNC_TOKEN}" \
  --dry-run=client -o yaml | kctl apply -f -

echo "[7/11] deploy clawcolony runtime"
sed "s|{{CLAWCOLONY_IMAGE}}|${IMAGE}|g" k8s/clawcolony-runtime-deployment.yaml | kctl apply -f -
kctl apply -f k8s/service-runtime.yaml

echo "[8/11] wait runtime ready"
kctl -n "${RUNTIME_NAMESPACE}" rollout status deployment/clawcolony-runtime --timeout=120s

echo "[9/11] clean legacy runtime endpoints (best effort)"
if [[ "${CLEANUP_LEGACY_RUNTIME}" == "true" ]] && [[ "${LEGACY_RUNTIME_NAMESPACE}" != "${RUNTIME_NAMESPACE}" ]]; then
  if kctl get namespace "${LEGACY_RUNTIME_NAMESPACE}" >/dev/null 2>&1; then
    kctl -n "${LEGACY_RUNTIME_NAMESPACE}" delete deployment clawcolony-runtime --ignore-not-found
    kctl -n "${LEGACY_RUNTIME_NAMESPACE}" delete service "${RUNTIME_SERVICE_NAME}" --ignore-not-found
  fi
fi

echo "[10/11] migrate existing agents to ${RUNTIME_BASE_URL} (best effort)"
if [[ "${MIGRATE_EXISTING_AGENTS}" == "true" ]]; then
  DEPLOYS="$(kctl -n "${RUNTIME_NAMESPACE}" get deploy -o name | grep -E '^deployment.apps/user-' || true)"
  if [[ -n "${DEPLOYS}" ]]; then
    while IFS= read -r d; do
      [[ -z "${d}" ]] && continue
      name="${d#deployment.apps/}"
      kctl -n "${RUNTIME_NAMESPACE}" set env "deployment/${name}" \
        "CLAWCOLONY_API_BASE_URL=${RUNTIME_BASE_URL}" \
        "INTERNAL_HTTP_ALLOWLIST=${RUNTIME_SERVICE_NAME}.${RUNTIME_NAMESPACE}.svc.cluster.local,${RUNTIME_SERVICE_NAME}" >/dev/null
      kctl -n "${RUNTIME_NAMESPACE}" rollout status "deployment/${name}" --timeout=180s >/dev/null
    done <<< "${DEPLOYS}"
  fi

  PROFILES="$(kctl -n "${RUNTIME_NAMESPACE}" get configmap -o name | grep -E '^configmap/user-.*-profile$' || true)"
  if [[ -n "${PROFILES}" ]]; then
    while IFS= read -r cm; do
      [[ -z "${cm}" ]] && continue
      kctl -n "${RUNTIME_NAMESPACE}" get "${cm}" -o yaml \
        | sed "s|${LEGACY_RUNTIME_BASE_URL}|${RUNTIME_BASE_URL}|g" \
        | kctl -n "${RUNTIME_NAMESPACE}" apply -f - >/dev/null
    done <<< "${PROFILES}"
  fi
fi

echo "[11/11] print status"
RUNTIME_DEPLOYS="$(kctl get deploy -A | grep -E 'clawcolony-runtime|NAMESPACE' || true)"
if [[ -n "${RUNTIME_DEPLOYS}" ]]; then
  echo "${RUNTIME_DEPLOYS}"
fi

echo
echo "Clawcolony deployed."
echo "Check status:"
echo "  kubectl -n ${RUNTIME_NAMESPACE} get pods"
echo "Port forward:"
echo "  kubectl -n ${RUNTIME_NAMESPACE} port-forward svc/${RUNTIME_SERVICE_NAME} 8080:8080"
