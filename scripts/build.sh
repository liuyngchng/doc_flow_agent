#!/usr/bin/env bash
# ================================================================
# build.sh - 一键编译脚本 (Linux)
#   ./scripts/build.sh            # 编译 Linux 二进制 (Docker)
#   ./scripts/build.sh local      # 编译 Linux 二进制 (本地 Go)
#   ./scripts/build.sh windows    # 交叉编译 Windows .exe
#   ./scripts/build.sh docker     # 构建 Docker 生产镜像
#
# 代理参数（可选，不带 = 时继承环境变量）:
#   http_proxy=<url>        设置 HTTP 代理
#   https_proxy=<url>       设置 HTTPS 代理
#   no_proxy=<url>          设置不走代理的地址列表
#
# 示例:
#   ./scripts/build.sh windows http_proxy=http://127.0.0.1:7890 https_proxy=http://127.0.0.1:7890
# ================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$SCRIPT_DIR"

IMAGE="doc_flow_agent-build:latest"

# ── Usage ───────────────────────────────────────────────────────
usage() {
  cat <<'EOF'
用法: ./scripts/build.sh [模式] [代理参数...]

模式:
  (默认)                  编译 Linux 二进制 (Docker)
  local                   编译 Linux 二进制 (本地 Go)
  windows                 交叉编译 Windows .exe (本地 Go)
  docker                  构建 Docker 生产镜像 (需先有 dist/doc_flow_agent)
  -h | --help             显示本帮助并退出

代理参数（可选，不带 = 时继承环境变量）:
  http_proxy=<url>        设置 HTTP 代理
  https_proxy=<url>       设置 HTTPS 代理
  no_proxy=<url>          设置不走代理的地址列表

示例:
  ./scripts/build.sh windows
  ./scripts/build.sh windows http_proxy=http://127.0.0.1:7890 https_proxy=http://127.0.0.1:7890
EOF
}

# ── 参数解析 ────────────────────────────────────────────────────
MODE=""
HTTP_PROXY_VAL=""
HTTPS_PROXY_VAL=""
NO_PROXY_VAL=""

for arg in "$@"; do
  case "$arg" in
    http_proxy=*|HTTP_PROXY=*)   HTTP_PROXY_VAL="${arg#*=}" ;;
    https_proxy=*|HTTPS_PROXY=*) HTTPS_PROXY_VAL="${arg#*=}" ;;
    no_proxy=*|NO_PROXY=*)       NO_PROXY_VAL="${arg#*=}" ;;
    -h|--help|help)              usage; exit 0 ;;
    *)
      if [[ -z "$MODE" ]]; then
        MODE="$arg"
      else
        echo "WARNING: 未知参数 '$arg'，已忽略（用 -h 查看用法）"
      fi
      ;;
  esac
done

# 不带 = 时继承环境变量
HTTP_PROXY_VAL="${HTTP_PROXY_VAL:-${HTTP_PROXY:-${http_proxy:-}}}"
HTTPS_PROXY_VAL="${HTTPS_PROXY_VAL:-${HTTPS_PROXY:-${https_proxy:-}}}"
NO_PROXY_VAL="${NO_PROXY_VAL:-${NO_PROXY:-${no_proxy:-}}}"

add_scheme() { local v="$1"; [[ -z "$v" || "$v" == *"://"* ]] && { printf '%s' "$v"; return; }; printf 'http://%s' "$v"; }
HTTP_PROXY_VAL="$(add_scheme "$HTTP_PROXY_VAL")"
HTTPS_PROXY_VAL="$(add_scheme "$HTTPS_PROXY_VAL")"

if [[ -n "$HTTP_PROXY_VAL" || -n "$HTTPS_PROXY_VAL" ]]; then
  export HTTP_PROXY="$HTTP_PROXY_VAL" HTTPS_PROXY="$HTTPS_PROXY_VAL"
  export http_proxy="$HTTP_PROXY_VAL" https_proxy="$HTTPS_PROXY_VAL"
  export NO_PROXY="$NO_PROXY_VAL"     no_proxy="$NO_PROXY_VAL"
  echo "代理: http=$HTTP_PROXY_VAL https=$HTTPS_PROXY_VAL no_proxy=$NO_PROXY_VAL"
fi

# ── Docker build arg / run env ───────────────────────────────────
DOCKER_BUILD_ARGS=()
DOCKER_RUN_ENV=()
if [[ -n "$HTTP_PROXY_VAL" || -n "$HTTPS_PROXY_VAL" ]]; then
  DOCKER_BUILD_ARGS=(--build-arg "HTTP_PROXY=$HTTP_PROXY_VAL" --build-arg "HTTPS_PROXY=$HTTPS_PROXY_VAL" --build-arg "NO_PROXY=$NO_PROXY_VAL")
  DOCKER_RUN_ENV=(
    -e "HTTP_PROXY=$HTTP_PROXY_VAL" -e "HTTPS_PROXY=$HTTPS_PROXY_VAL" -e "NO_PROXY=$NO_PROXY_VAL"
    -e "http_proxy=$HTTP_PROXY_VAL" -e "https_proxy=$HTTPS_PROXY_VAL" -e "no_proxy=$NO_PROXY_VAL"
  )
fi

# ── HTTPS 自签证书 ────────────────────────────────────────────────
CERT_DIR="$SCRIPT_DIR/dist"
CERT_FILE="$CERT_DIR/cert.pem"
KEY_FILE="$CERT_DIR/key.pem"

ensure_cert() {
  if [[ -f "$CERT_FILE" && -f "$KEY_FILE" ]]; then
    echo "HTTPS 证书已存在: $CERT_FILE / $KEY_FILE"
    return 0
  fi
  mkdir -p "$CERT_DIR"
  echo "=== 生成 HTTPS 自签证书 ==="
  openssl req -x509 -newkey rsa:2048 -keyout "$KEY_FILE" -out "$CERT_FILE" \
    -days 3650 -nodes -subj "/CN=localhost/O=doc_flow_agent"
  echo "  证书: $CERT_FILE"
  echo "  私钥: $KEY_FILE"
  echo "=== 证书生成完成 ==="
}

# ================================================================
# Go toolchain (host-cached tarball, used by all Docker builds)
# ================================================================
GO_VERSION="1.24.13"
GO_TARBALL="$SCRIPT_DIR/build/deps/go${GO_VERSION}.linux-amd64.tar.gz"
GO_DOWNLOAD_URL="https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz"

ensure_go_tarball() {
  if [[ -f "$GO_TARBALL" ]]; then
    return 0
  fi
  mkdir -p "$(dirname "$GO_TARBALL")"
  echo "Downloading Go ${GO_VERSION} toolchain ..."
  if command -v curl &>/dev/null; then
    curl -fL --retry 3 -o "$GO_TARBALL" "$GO_DOWNLOAD_URL"
  elif command -v wget &>/dev/null; then
    wget -O "$GO_TARBALL" "$GO_DOWNLOAD_URL"
  else
    echo "ERROR: 需要 curl 或 wget 下载 Go toolchain"
    exit 1
  fi
  echo "Go toolchain saved to $GO_TARBALL"
}

# ================================================================
# build_windows — 本地交叉编译 Windows .exe + 打包 zip
# ================================================================
build_windows() {
    echo "=== Building doc_flow_agent.exe (Windows amd64) ==="
    ensure_cert
    rm -rf dist/doc_flow_agent.exe dist/doc_flow_agent-windows-amd64*

    CGO_ENABLED=0 GOOS=windows GOARCH=amd64 \
        go build -mod=mod -buildvcs=false -ldflags="-s -w -H windowsgui" -o dist/doc_flow_agent.exe .

    echo "  dist/doc_flow_agent.exe ($(du -h dist/doc_flow_agent.exe | cut -f1))"

    # 打包目录
    local pkg="dist/doc_flow_agent-windows-amd64"
    rm -rf "$pkg" && mkdir -p "$pkg"

    cp dist/doc_flow_agent.exe "$pkg/"
    cp cfg.yml.template "$pkg/cfg.yml"
    cp scripts/windows-readme.txt "$pkg/README.txt"

    # 打包 zip
    cd dist && rm -f doc_flow_agent-windows-amd64.zip && \
        zip -rq "doc_flow_agent-windows-amd64.zip" "doc_flow_agent-windows-amd64"
    cd "$SCRIPT_DIR"

    echo "  dist/doc_flow_agent-windows-amd64.zip ($(du -h dist/doc_flow_agent-windows-amd64.zip | cut -f1))"

    # 清理中间文件
    rm -rf "$pkg" dist/doc_flow_agent.exe
    echo "=== Done ==="
}

# ================================================================
# build_linux_local — 本地 Go 编译 Linux 二进制 + 打包 tar
# ================================================================
build_linux_local() {
    echo "=== Building doc_flow_agent (Linux amd64, local Go) ==="
    ensure_cert
    rm -rf dist/doc_flow_agent dist/doc_flow_agent-linux-amd64*

    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
        go build -mod=mod -buildvcs=false -ldflags="-s -w" -o dist/doc_flow_agent .

    echo "  dist/doc_flow_agent ($(du -h dist/doc_flow_agent | cut -f1))"

    # 打包目录
    local pkg="dist/doc_flow_agent-linux-amd64"
    rm -rf "$pkg" && mkdir -p "$pkg"

    cp dist/doc_flow_agent "$pkg/"
    cp cfg.yml.template "$pkg/cfg.yml"
    cp scripts/windows-readme.txt "$pkg/README.txt"

    # 打包 tar
    tar -cf "dist/doc_flow_agent-linux-amd64.tar" -C dist "doc_flow_agent-linux-amd64"

    echo "  dist/doc_flow_agent-linux-amd64.tar ($(du -h dist/doc_flow_agent-linux-amd64.tar | cut -f1))"

    # 清理中间文件
    rm -rf "$pkg" dist/doc_flow_agent
    echo "=== Done ==="
}

# ================================================================
# build_linux_docker — Docker 编译 Linux 二进制 + 生产镜像
# ================================================================
build_linux_docker() {
    ensure_go_tarball
    ensure_cert

    # 构建编译镜像
    if ! docker image inspect "$IMAGE" &>/dev/null; then
        echo "=== Building build image $IMAGE ==="
        docker build "${DOCKER_BUILD_ARGS[@]+"${DOCKER_BUILD_ARGS[@]}"}" \
            --build-arg "GO_VERSION=$GO_VERSION" \
            -t "$IMAGE" -f scripts/Dockerfile .
        echo "Done."
    fi

    # Go 模块缓存目录（映射到宿主机，加速后续编译）
    local gocache="$SCRIPT_DIR/build/gocache"
    local gomodcache="$SCRIPT_DIR/build/gomodcache"
    mkdir -p "$gocache" "$gomodcache" dist

    echo ""
    echo "=== Building doc_flow_agent (Linux amd64, Docker) ==="

    docker run --rm \
        -v "$SCRIPT_DIR":/workspace \
        -v "$gocache":/go/cache \
        -v "$gomodcache":/go/pkg/mod \
        -w /workspace \
        "${DOCKER_RUN_ENV[@]+"${DOCKER_RUN_ENV[@]}"}" \
        -e GOCACHE=/go/cache \
        -e GOMODCACHE=/go/pkg/mod \
        -e GOPROXY="https://goproxy.cn,direct" \
        -e CGO_ENABLED=0 \
        -e GOOS=linux \
        -e GOARCH=amd64 \
        "$IMAGE" \
        go build -mod=mod -buildvcs=false -ldflags="-s -w" -o dist/doc_flow_agent .

    echo "  dist/doc_flow_agent ($(du -h "$SCRIPT_DIR/dist/doc_flow_agent" | cut -f1))"

    # 构建生产镜像
    echo ""
    echo "=== Building production Docker image doc_flow_agent:latest ==="
    docker build "${DOCKER_BUILD_ARGS[@]+"${DOCKER_BUILD_ARGS[@]}"}" \
        -t doc_flow_agent:latest -f scripts/Dockerfile.server .

    echo ""
    echo "=== Done ==="
    echo "  Docker image: doc_flow_agent:latest"
    echo "  Run with:"
    echo "    # data/ 包含数据库 cfg.db、上传文件、日志，容器停后数据不丢"
    echo "    docker run -d --rm --name doc_flow_agent -p 20001:20001 \\"
    echo "      -v \$(pwd)/data:/app/data \\"
    echo "      doc_flow_agent:latest"
    echo "    # 查看运行日志"
    echo "    docker logs -f doc_flow_agent"
}

case "${MODE:-docker}" in
    windows)  build_windows ;;
    local)    build_linux_local ;;
    docker)   build_linux_docker ;;
    *)        echo "ERROR: 未知模式 '$MODE'（用 -h 查看用法）"; exit 1 ;;
esac