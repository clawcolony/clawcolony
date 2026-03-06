#!/usr/bin/env bash
set -euo pipefail

# Build a bot image for the active Minikube node architecture and load it.
#
# Usage:
#   ./scripts/build_bot_image_for_minikube.sh \
#     --context /Users/waken/workspace/containers/openclaw \
#     --dockerfile Dockerfile.onepod \
#     --image openclaw:onepod-dev

CONTEXT=""
DOCKERFILE="Dockerfile"
IMAGE=""
LOAD_IMAGE="true"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --context)
      CONTEXT="$2"
      shift 2
      ;;
    --dockerfile)
      DOCKERFILE="$2"
      shift 2
      ;;
    --image)
      IMAGE="$2"
      shift 2
      ;;
    --no-load)
      LOAD_IMAGE="false"
      shift
      ;;
    *)
      echo "unknown argument: $1"
      exit 1
      ;;
  esac
done

if [[ -z "$CONTEXT" || -z "$IMAGE" ]]; then
  echo "missing required args"
  echo "usage: $0 --context <path> --dockerfile <file> --image <name[:tag]> [--no-load]"
  exit 1
fi

if ! command -v kubectl >/dev/null 2>&1; then
  echo "kubectl not found"
  exit 1
fi
if ! command -v minikube >/dev/null 2>&1; then
  echo "minikube not found"
  exit 1
fi
if ! command -v docker >/dev/null 2>&1; then
  echo "docker not found"
  exit 1
fi

# Prefer Minikube node architecture as the deployment target.
NODE_ARCH="$(kubectl get nodes -o jsonpath='{.items[0].status.nodeInfo.architecture}' 2>/dev/null || true)"
if [[ -z "$NODE_ARCH" ]]; then
  HOST_ARCH="$(uname -m)"
  case "$HOST_ARCH" in
    x86_64) NODE_ARCH="amd64" ;;
    aarch64|arm64) NODE_ARCH="arm64" ;;
    *)
      echo "unsupported host architecture fallback: $HOST_ARCH"
      exit 1
      ;;
  esac
fi

case "$NODE_ARCH" in
  amd64|x86_64) PLATFORM="linux/amd64" ;;
  arm64|aarch64) PLATFORM="linux/arm64" ;;
  *)
    echo "unsupported target architecture from cluster: $NODE_ARCH"
    exit 1
    ;;
esac

echo "target node architecture: $NODE_ARCH"
echo "docker build platform: $PLATFORM"
echo "building image: $IMAGE"

docker build --platform "$PLATFORM" -f "$CONTEXT/$DOCKERFILE" -t "$IMAGE" "$CONTEXT"

if [[ "$LOAD_IMAGE" == "true" ]]; then
  echo "loading image into minikube: $IMAGE"
  minikube image load "$IMAGE"
fi

echo "done"
