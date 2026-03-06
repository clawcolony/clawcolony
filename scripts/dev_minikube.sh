#!/usr/bin/env bash
set -euo pipefail

IMAGE="${1:-clawcolony:dev}"

echo "[1/4] ensure minikube is running"
minikube status >/dev/null

echo "[2/4] build local image: ${IMAGE}"
docker build -t "${IMAGE}" .

echo "[3/4] load image into minikube"
minikube image load "${IMAGE}"

echo "[4/4] deploy clawcolony runtime"
kubectl apply -f k8s/rbac.yaml
sed "s|{{CLAWCOLONY_IMAGE}}|${IMAGE}|g" k8s/clawcolony-runtime-deployment.yaml | kubectl apply -f -
kubectl apply -f k8s/service-runtime.yaml

echo
echo "Clawcolony deployed."
echo "Check status:"
echo "  kubectl -n freewill get pods"
echo "Port forward:"
echo "  kubectl -n freewill port-forward svc/clawcolony 8080:8080"
