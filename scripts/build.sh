#!/usr/bin/env bash
# ================================================================
# build.sh - 一键编译脚本 (Linux)
#   ./scripts/build.sh            # 编译 Linux 二进制
#   ./scripts/build.sh windows    # 交叉编译 Windows .exe
#   ./scripts/build.sh docker     # 构建 Docker 生产镜像
# ================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$SCRIPT_DIR"

IMAGE="doc-ai-gateway-build:latest"

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

build_linux() {
    if [[ "${1:-}" == "local" ]]; then
        echo "=== Building doc-ai-gateway (Linux amd64, local Go) ==="
        mkdir -p dist
        CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
            go build -mod=mod -ldflags="-s -w" -o dist/doc-ai-gateway .
        echo "  dist/doc-ai-gateway ($(du -h dist/doc-ai-gateway | cut -f1))"
        echo "=== Done ==="
        return
    fi

    # Docker 编译
    if ! docker image inspect "$IMAGE" &>/dev/null; then
        echo "=== Building build image $IMAGE ==="
        docker build -t "$IMAGE" -f scripts/Dockerfile .
        echo "Done."
    fi

    echo ""
    echo "=== Building doc-ai-gateway (Linux amd64, Docker) ==="
    mkdir -p dist

    docker run --rm \
        -v "$SCRIPT_DIR":/workspace \
        -w /workspace \
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

case "${1:-}" in
    windows)  build_windows ;;
    local)    build_linux local ;;
    docker)   build_linux docker ;;
    *)        build_linux docker ;;
esac