#!/usr/bin/env bash
# ================================================================
# dev.sh - 进入开发容器 (ubuntu:24.04 + Go toolchain)
# ================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$SCRIPT_DIR"

IMAGE="doc_flow_agent-build:latest"

ensure_go_tarball() {
  GO_VERSION="1.24.13"
  GO_TARBALL="$SCRIPT_DIR/build/deps/go${GO_VERSION}.linux-amd64.tar.gz"
  if [[ -f "$GO_TARBALL" ]]; then
    return 0
  fi
  mkdir -p "$(dirname "$GO_TARBALL")"
  echo "Downloading Go ${GO_VERSION} toolchain..."
  if command -v curl &>/dev/null; then
    curl -fL --retry 3 -o "$GO_TARBALL" "https://golang.google.cn/dl/go${GO_VERSION}.linux-amd64.tar.gz"
  elif command -v wget &>/dev/null; then
    wget -O "$GO_TARBALL" "https://golang.google.cn/dl/go${GO_VERSION}.linux-amd64.tar.gz"
  else
    echo "ERROR: 需要 curl 或 wget 下载 Go toolchain"
    exit 1
  fi
  echo "Go toolchain saved."
}

ensure_go_tarball

# 构建编译镜像
if ! docker image inspect "$IMAGE" &>/dev/null; then
    echo "Building dev image $IMAGE ..."
    GO_VERSION="1.24.13"
    docker build --build-arg "GO_VERSION=$GO_VERSION" -t "$IMAGE" -f scripts/Dockerfile .
    echo "Done."
fi

# Go 模块缓存
GOCACHE_DIR="$SCRIPT_DIR/build/gocache"
GOMODCACHE_DIR="$SCRIPT_DIR/build/gomodcache"
mkdir -p "$GOCACHE_DIR" "$GOMODCACHE_DIR"

echo ""
echo "=== Entering dev container ==="
docker run --rm -it \
    -v "$SCRIPT_DIR":/workspace \
    -v "$GOCACHE_DIR":/go/cache \
    -v "$GOMODCACHE_DIR":/go/pkg/mod \
    -w /workspace \
    -e GOCACHE=/go/cache \
    -e GOMODCACHE=/go/pkg/mod \
    -e GOPROXY=https://goproxy.cn,direct \
    -e CGO_ENABLED=0 \
    "$IMAGE" \
    bash