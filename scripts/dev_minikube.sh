#!/usr/bin/env bash
set -euo pipefail

IMAGE="${1:-clawcolony:dev}"

echo "[1/7] ensure minikube is running"
minikube status >/dev/null

echo "[2/7] build local image: ${IMAGE}"
docker build -t "${IMAGE}" .

echo "[3/7] load image into minikube"
minikube image load "${IMAGE}"

echo "[4/7] apply namespaces, nats, postgres and rbac"
kubectl apply -f k8s/namespaces.yaml
kubectl apply -f k8s/nats.yaml
kubectl apply -f k8s/postgres.yaml
kubectl apply -f k8s/rbac.yaml

echo "[5/7] wait for nats ready"
kubectl -n clawcolony rollout status statefulset/clawcolony-nats --timeout=120s

echo "[6/7] wait for postgres ready"
kubectl -n clawcolony rollout status statefulset/clawcolony-postgres --timeout=120s

echo "[7/7] deploy clawcolony runtime"
sed "s|{{CLAWCOLONY_IMAGE}}|${IMAGE}|g" k8s/clawcolony-runtime-deployment.yaml | kubectl apply -f -
kubectl apply -f k8s/service-runtime.yaml

echo
echo "Clawcolony deployed."
echo "Check status:"
echo "  kubectl -n clawcolony get pods"
echo "Port forward:"
echo "  kubectl -n clawcolony port-forward svc/clawcolony 8080:8080"
