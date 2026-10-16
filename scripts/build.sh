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
  docker                  构建 Docker 生产镜像
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

# ── 参数解析：第一个非代理参数作为模式，其余解析代理 ──────────────
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

# 自动补全 scheme（如 127.0.0.1:7890 -> http://127.0.0.1:7890）
add_scheme() { local v="$1"; [[ -z "$v" || "$v" == *"://"* ]] && { printf '%s' "$v"; return; }; printf 'http://%s' "$v"; }
HTTP_PROXY_VAL="$(add_scheme "$HTTP_PROXY_VAL")"
HTTPS_PROXY_VAL="$(add_scheme "$HTTPS_PROXY_VAL")"

# ── 统一导出代理环境变量（本地 go build 会读取） ─────────────────
if [[ -n "$HTTP_PROXY_VAL" || -n "$HTTPS_PROXY_VAL" ]]; then
  export HTTP_PROXY="$HTTP_PROXY_VAL" HTTPS_PROXY="$HTTPS_PROXY_VAL"
  export http_proxy="$HTTP_PROXY_VAL" https_proxy="$HTTPS_PROXY_VAL"
  export NO_PROXY="$NO_PROXY_VAL"     no_proxy="$NO_PROXY_VAL"
  echo "代理: http=$HTTP_PROXY_VAL https=$HTTPS_PROXY_VAL no_proxy=$NO_PROXY_VAL"
fi

# ── Docker 运行时代理 env（供 docker run 传 -e） ─────────────────
DOCKER_RUN_ENV=()
if [[ -n "$HTTP_PROXY_VAL" || -n "$HTTPS_PROXY_VAL" ]]; then
  DOCKER_RUN_ENV=(
    -e "HTTP_PROXY=$HTTP_PROXY_VAL" -e "HTTPS_PROXY=$HTTPS_PROXY_VAL" -e "NO_PROXY=$NO_PROXY_VAL"
    -e "http_proxy=$HTTP_PROXY_VAL" -e "https_proxy=$HTTPS_PROXY_VAL" -e "no_proxy=$NO_PROXY_VAL"
  )
fi

build_windows() {
    echo "=== Building doc-ai-gateway.exe (Windows amd64) ==="
    mkdir -p dist

    CGO_ENABLED=0 GOOS=windows GOARCH=amd64 \
        go build -mod=mod -ldflags="-s -w -H windowsgui" -o dist/doc-ai-gateway.exe .

    echo "  dist/doc-ai-gateway.exe ($(du -h dist/doc-ai-gateway.exe | cut -f1))"

    # 打包发行版
    local pkg="dist/doc-ai-gateway-windows-amd64"
    rm -rf "$pkg" && mkdir -p "$pkg"

    cp dist/doc-ai-gateway.exe "$pkg/"
    if [[ -f cfg.yml ]]; then
        cp cfg.yml "$pkg/"
    else
        cp cfg.yml.template "$pkg/cfg.yml"
    fi

    # 创建 README
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

build_linux_local() {
    echo "=== Building doc-ai-gateway (Linux amd64, local Go) ==="
    mkdir -p dist
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
        go build -mod=mod -ldflags="-s -w" -o dist/doc-ai-gateway .
    echo "  dist/doc-ai-gateway ($(du -h dist/doc-ai-gateway | cut -f1))"
    echo "=== Done ==="
}

build_linux_docker() {
    # 构建编译镜像
    if ! docker image inspect "$IMAGE" &>/dev/null; then
        echo "=== Building build image $IMAGE ==="
        docker build -t "$IMAGE" -f scripts/Dockerfile .
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
    echo "=== Building production Docker image ==="
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