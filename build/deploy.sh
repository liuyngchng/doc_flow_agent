#!/usr/bin/env bash
# ================================================================
# deploy.sh - 启动/重启/停止 doc_flow_agent 容器
#   ./build/deploy.sh              # 启动（默认）
#   ./build/deploy.sh stop         # 停止并删除容器
#   ./build/deploy.sh restart      # 重启
#   ./build/deploy.sh logs         # 查看容器日志
#
# 容器加入 llm_net 虚拟网桥，可通过容器名访问同网络内的其他服务。
# 示例：在 cfg.yml 中配置 doc_parser.url = "http://doc-parser:9090/parse"
# 同理，其他服务也可通过 https://doc_flow_agent:20001 访问本服务。
# ================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$SCRIPT_DIR"

IMAGE="doc_flow_agent:latest"
NETWORK="llm_net"
CONTAINER="${CONTAINER:-doc_flow_agent}"
HOST_PORT="${HOST_PORT:-20001}"
DATA_DIR="${DATA_DIR:-$SCRIPT_DIR/data}"

# ── 探测本机 LAN IP（跳过 lo 和 docker 网桥地址）──────────────────
detect_lan_ip() {
  ip -4 addr show 2>/dev/null \
    | grep -oP '(?<=inet\s)\d+(\.\d+){3}(?=/)' \
    | grep -vE '^127\.|^172\.(1[7-9]|2[0-9]|3[0-1])\.' \
    | head -1
}

# ── 网络 ──────────────────────────────────────────────────────────
ensure_network() {
  if ! docker network inspect "$NETWORK" &>/dev/null; then
    echo "=== 创建网络 $NETWORK ==="
    docker network create "$NETWORK"
  fi
}

# ── 启动 ──────────────────────────────────────────────────────────
start() {
  ensure_network

  # 如果容器已存在，先删
  if docker ps -a --format '{{.Names}}' | grep -qx "$CONTAINER"; then
    echo "容器 $CONTAINER 已存在，正在删除..."
    docker rm -f "$CONTAINER" >/dev/null
  fi

  mkdir -p "$DATA_DIR"

  LAN_IP="${LAN_IP:-$(detect_lan_ip)}"

  echo "=== 启动容器 $CONTAINER ==="
  docker run -dit \
    --name "$CONTAINER" \
    --network "$NETWORK" \
    --security-opt seccomp=unconfined \
    -p "$HOST_PORT":20001 \
    -v "$DATA_DIR":/app/data \
    "$IMAGE"

  # 获取容器在 llm_net 上的内部 IP
  CONTAINER_IP=$(docker inspect "$CONTAINER" \
    --format "{{(index .NetworkSettings.Networks \"$NETWORK\").IPAddress}}" 2>/dev/null || echo "")

  echo ""
  echo "=== 容器已启动 ==="
  echo ""
  echo "  ── 宿主机访问 (外部) ──"
  echo "    https://localhost:$HOST_PORT"
  if [ -n "$LAN_IP" ]; then
    echo "    https://$LAN_IP:$HOST_PORT"
  fi
  echo ""
  echo "  ── 容器间访问 (llm_net 内部) ──"
  echo "    https://$CONTAINER:20001"
  if [ -n "$CONTAINER_IP" ]; then
    echo "    https://$CONTAINER_IP:20001"
  fi
  echo ""
  echo "  注意: 全站仅 HTTPS（自签证书），无 HTTP。"
  echo "        浏览器首次访问自签证书需点击"高级 → 继续访问"。"
  echo "        容器间 HTTP 客户端需跳过 TLS 校验（InsecureSkipVerify）。"
  echo ""
  echo "  网络:  $NETWORK (可通过容器名访问同网络内其他服务)"
  echo "  数据:  $DATA_DIR/"
  echo ""
  echo "  查看日志: ./build/deploy.sh logs"
  echo "  停止容器: ./build/deploy.sh stop"
}

# ── 停止 ──────────────────────────────────────────────────────────
stop() {
  if docker ps -a --format '{{.Names}}' | grep -qx "$CONTAINER"; then
    echo "=== 停止并删除容器 $CONTAINER ==="
    docker rm -f "$CONTAINER"
    echo "已删除。"
  else
    echo "容器 $CONTAINER 不存在。"
  fi
}

# ── 重启 ──────────────────────────────────────────────────────────
restart() {
  stop
  start
}

# ── 日志 ──────────────────────────────────────────────────────────
logs() {
  docker logs -f "$CONTAINER"
}

# ── 入口 ──────────────────────────────────────────────────────────
case "${1:-start}" in
  start)   start ;;
  stop)    stop ;;
  restart) restart ;;
  logs)    logs ;;
  -h|--help|help)
    cat <<'EOF'
用法: ./build/deploy.sh [命令]

命令:
  start       启动容器（默认）
  stop        停止并删除容器
  restart     重启容器
  logs        查看容器日志（实时）

环境变量（均可选）:
  CONTAINER   容器名称（默认 doc_flow_agent）
  HOST_PORT   宿主机端口（默认 20001）
  DATA_DIR    数据目录（默认 $SCRIPT_DIR/data）

示例:
  ./build/deploy.sh start
  CONTAINER=doc-agent HOST_PORT=20002 ./build/deploy.sh start
  ./build/deploy.sh restart
EOF
    ;;
  *) echo "未知命令: $1 (支持: start/stop/restart/logs)"; exit 1 ;;
esac