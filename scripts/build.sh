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

IMAGE="doc-ai-gateway-build:latest"

# ── Usage ───────────────────────────────────────────────────────
usage() {
  cat <<'EOF'
用法: ./scripts/build.sh [模式] [代理参数...]

模式:
  (默认)                  编译 Linux 二进制 (Docker)
  local                   编译 Linux 二进制 (本地 Go)
  windows                 交叉编译 Windows .exe (本地 Go)
  docker                  构建 Docker 生产镜像 (需先有 dist/doc-ai-gateway)
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
# build_windows — 本地交叉编译 Windows .exe
# ================================================================
build_windows() {
    echo "=== Building doc-ai-gateway.exe (Windows amd64) ==="
    mkdir -p dist

    CGO_ENABLED=0 GOOS=windows GOARCH=amd64 \
        go build -mod=mod -ldflags="-s -w -H windowsgui" -o dist/doc-ai-gateway.exe .

    echo "  dist/doc-ai-gateway.exe ($(du -h dist/doc-ai-gateway.exe | cut -f1))"

    local pkg="dist/doc-ai-gateway-windows-amd64"
    rm -rf "$pkg" && mkdir -p "$pkg"

    cp dist/doc-ai-gateway.exe "$pkg/"
    if [[ -f cfg.yml ]]; then
        cp cfg.yml "$pkg/"
    else
        cp cfg.yml.template "$pkg/cfg.yml"
    fi

    cat > "$pkg/README.txt" << 'EOF'
DocAI Gateway - 文档 AI 智能网关
====================================

快速开始:
  1. 双击 doc-ai-gateway.exe 启动服务
  2. 打开浏览器访问 http://localhost:8080
  3. 使用默认账号 admin/admin 登录

注意:
  admin 首次登录必须修改密码，2小时内未修改则初始密码作废
  程序运行日志见 logs/ 目录，按小时滚动，保留7天

目录说明:
  doc-ai-gateway.exe  主程序
  cfg.yml             配置文件
  data\                文件存储目录 (自动创建)

对外接口:
  POST /api/files/upload       上传文件
  GET  /api/files/status/:id   查询文件状态
  POST /api/ai/call            调用 AI 接口

管理后台: http://localhost:8080/admin
EOF

    cd dist && zip -rq "doc-ai-gateway-windows-amd64.zip" "doc-ai-gateway-windows-amd64"
    rm -rf "doc-ai-gateway-windows-amd64"
    echo "  dist/doc-ai-gateway-windows-amd64.zip ($(du -h dist/doc-ai-gateway-windows-amd64.zip | cut -f1))"
    echo "=== Done ==="
}

# ================================================================
# build_linux_local — 本地 Go 编译 Linux 二进制
# ================================================================
build_linux_local() {
    echo "=== Building doc-ai-gateway (Linux amd64, local Go) ==="
    mkdir -p dist
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
        go build -mod=mod -ldflags="-s -w" -o dist/doc-ai-gateway .
    echo "  dist/doc-ai-gateway ($(du -h dist/doc-ai-gateway | cut -f1))"
    echo "=== Done ==="
}

# ================================================================
# build_linux_docker — Docker 编译 Linux 二进制 + 生产镜像
# ================================================================
build_linux_docker() {
    ensure_go_tarball

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
    echo "=== Building doc-ai-gateway (Linux amd64, Docker) ==="

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
        go build -mod=mod -ldflags="-s -w" -o dist/doc-ai-gateway .

    echo "  dist/doc-ai-gateway ($(du -h "$SCRIPT_DIR/dist/doc-ai-gateway" | cut -f1))"

    # 构建生产镜像
    echo ""
    echo "=== Building production Docker image doc-ai-gateway:latest ==="
    docker build -t doc-ai-gateway:latest -f scripts/Dockerfile.server .

    echo ""
    echo "=== Done ==="
    echo "  Docker image: doc-ai-gateway:latest"
    echo "  Run with: docker run -p 8080:8080 -v \$(pwd)/data:/app/data doc-ai-gateway:latest"
}

case "${MODE:-docker}" in
    windows)  build_windows ;;
    local)    build_linux_local ;;
    docker)   build_linux_docker ;;
    *)        echo "ERROR: 未知模式 '$MODE'（用 -h 查看用法）"; exit 1 ;;
esac