#!/usr/bin/env bash
set -euo pipefail

IMAGE="${1:-clawcolony:dev}"
RUNTIME_DB_NAME="${RUNTIME_DB_NAME:-clawcolony_runtime}"
RUNTIME_DB_HOST="${RUNTIME_DB_HOST:-clawcolony-postgres.freewill.svc.cluster.local}"
RUNTIME_DB_PORT="${RUNTIME_DB_PORT:-5432}"
CLAWCOLONY_INTERNAL_SYNC_TOKEN="${CLAWCOLONY_INTERNAL_SYNC_TOKEN:-clawcolony-internal-sync-dev}"

echo "[1/8] ensure minikube is running"
minikube status >/dev/null

echo "[2/8] build local image: ${IMAGE}"
docker build -t "${IMAGE}" .

echo "[3/8] load image into minikube"
minikube image load "${IMAGE}"

echo "[4/8] deploy runtime postgres + rbac"
kubectl apply -f k8s/postgres.yaml
kubectl apply -f k8s/rbac.yaml

echo "[5/8] wait runtime postgres ready"
kubectl -n freewill rollout status statefulset/clawcolony-postgres --timeout=120s

echo "[6/8] upsert runtime secret freewill/clawcolony-runtime"
PG_USER="$(kubectl -n freewill get secret clawcolony-postgres -o jsonpath='{.data.POSTGRES_USER}' | base64 --decode 2>/dev/null || true)"
PG_PASSWORD="$(kubectl -n freewill get secret clawcolony-postgres -o jsonpath='{.data.POSTGRES_PASSWORD}' | base64 --decode 2>/dev/null || true)"
if [[ -z "${PG_USER}" ]]; then
  PG_USER="clawcolony"
fi
if [[ -z "${PG_PASSWORD}" ]]; then
  PG_PASSWORD="clawcolony"
fi
RUNTIME_DATABASE_URL="${RUNTIME_DATABASE_URL:-postgres://${PG_USER}:${PG_PASSWORD}@${RUNTIME_DB_HOST}:${RUNTIME_DB_PORT}/${RUNTIME_DB_NAME}?sslmode=disable}"
kubectl -n freewill create secret generic clawcolony-runtime \
  --from-literal="DATABASE_URL=${RUNTIME_DATABASE_URL}" \
  --from-literal="CLAWCOLONY_INTERNAL_SYNC_TOKEN=${CLAWCOLONY_INTERNAL_SYNC_TOKEN}" \
  --dry-run=client -o yaml | kubectl apply -f -

echo "[7/8] deploy clawcolony runtime"
sed "s|{{CLAWCOLONY_IMAGE}}|${IMAGE}|g" k8s/clawcolony-runtime-deployment.yaml | kubectl apply -f -
kubectl apply -f k8s/service-runtime.yaml

echo "[8/8] wait runtime ready"
kubectl -n freewill rollout status deployment/clawcolony-runtime --timeout=120s

echo
echo "Clawcolony deployed."
echo "Check status:"
echo "  kubectl -n freewill get pods"
echo "Port forward:"
echo "  kubectl -n freewill port-forward svc/clawcolony 8080:8080"
