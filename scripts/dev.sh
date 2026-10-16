#!/usr/bin/env bash
# 进入开发容器
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$SCRIPT_DIR"
IMAGE="doc-ai-gateway-build:latest"

if ! docker image inspect "$IMAGE" &>/dev/null; then
    echo "Building dev image $IMAGE ..."
    docker build -t "$IMAGE" -f scripts/Dockerfile .
    echo "Done."
fi

docker run --rm -it \
    -v "$SCRIPT_DIR":/workspace \
    -w /workspace \
    -e GOPROXY=https://goproxy.cn,direct \
    -e CGO_ENABLED=0 \
    "$IMAGE" \
    sh