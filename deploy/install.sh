#!/usr/bin/env bash
# ==============================================================================
# ProbeWatch Linux 一键部署与服务配置脚本
# 支持架构: x86_64 (amd64), aarch64 (arm64)
# 默认加固: 专用非 root 用户、NoNewPrivileges、MTR cap_net_raw capability
# ==============================================================================

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
PLAIN='\033[0m'

info()  { echo -e "${BLUE}[INFO]${PLAIN} $*"; }
ok()    { echo -e "${GREEN}[OK]${PLAIN} $*"; }
warn()  { echo -e "${YELLOW}[WARN]${PLAIN} $*"; }
error() { echo -e "${RED}[ERROR]${PLAIN} $*" >&2; }

if [[ $EUID -ne 0 ]]; then
    error "本脚本必须以 root 权限运行，请使用 sudo bash $0"
    exit 1
fi

# 1. 架构检测
ARCH=$(uname -m)
case "$ARCH" in
    x86_64|amd64)
        TARGET_ARCH="amd64"
        ;;
    aarch64|arm64)
        TARGET_ARCH="arm64"
        ;;
    *)
        error "不支持的 CPU 架构: $ARCH (仅支持 x86_64/amd64 与 aarch64/arm64)"
        exit 1
        ;;
esac

info "检测到系统架构: $ARCH ($TARGET_ARCH)"

# 2. 安装目标选择
ROLE="${1:-agent}"

if [[ "$ROLE" == "agent" ]]; then
    info "准备安装: ProbeWatch Agent (纯监控被控端)"

    # 检查交互式参数或环境变量
    ENDPOINT="${PROBEWATCH_AGENT_ENDPOINT:-}"
    NODE_UUID="${PROBEWATCH_AGENT_NODE_UUID:-}"
    NODE_TOKEN="${PROBEWATCH_AGENT_NODE_TOKEN:-}"

    if [[ -z "$ENDPOINT" ]]; then
        read -r -p "请输入主控上报地址 (例如 https://monitor.example.com/api/agent/v1): " ENDPOINT
    fi
    if [[ -z "$NODE_UUID" ]]; then
        read -r -p "请输入节点 UUID: " NODE_UUID
    fi
    if [[ -z "$NODE_TOKEN" ]]; then
        read -r -p "请输入节点长期 Token: " NODE_TOKEN
    fi

    if [[ -z "$ENDPOINT" || -z "$NODE_UUID" || -z "$NODE_TOKEN" ]]; then
        error "Endpoint、UUID、Token 为必填项，安装中止"
        exit 1
    fi

    # 创建专用系统账号 (非 root)
    if ! id -u probewatch >/dev/null 2>&1; then
        info "创建系统专用用户 probewatch..."
        useradd -r -s /usr/sbin/nologin -d /var/lib/probewatch -m probewatch || true
    fi

    # 配置存储目录
    mkdir -p /etc/probewatch /var/lib/probewatch
    chown -R probewatch:probewatch /var/lib/probewatch
    chmod 0700 /var/lib/probewatch

    # 写入配置文件
    cat > /etc/probewatch/agent.env <<EOF
PROBEWATCH_ENV=production
PROBEWATCH_AGENT_ENDPOINT=${ENDPOINT}
PROBEWATCH_AGENT_NODE_UUID=${NODE_UUID}
PROBEWATCH_AGENT_NODE_TOKEN=${NODE_TOKEN}
PROBEWATCH_AGENT_DATA=/var/lib/probewatch
EOF
    chmod 0600 /etc/probewatch/agent.env
    chown probewatch:probewatch /etc/probewatch/agent.env

    # 检查本地是否有构建好的二进制
    BIN_PATH="/usr/local/bin/probewatch-agent"
    if [[ -f "./probewatch-agent" ]]; then
        cp ./probewatch-agent "$BIN_PATH"
    elif [[ -f "./cmd/probewatch-agent/probewatch-agent" ]]; then
        cp ./cmd/probewatch-agent/probewatch-agent "$BIN_PATH"
    else
        warn "未检测到本地现成二进制，请将 probewatch-agent 二进制放置在 $BIN_PATH"
    fi

    if [[ -f "$BIN_PATH" ]]; then
        chmod 0755 "$BIN_PATH"
        # 授予普通用户 raw socket 权限执行底层 ICMP MTR 追踪
        if command -v setcap >/dev/null 2>&1; then
            setcap cap_net_raw+ep "$BIN_PATH" || true
            ok "已成功为 probewatch-agent 配置 cap_net_raw+ep 权限 (支持免 root 执行 MTR)"
        else
            warn "未找到 setcap 命令，如需普通用户权限运行 MTR，请安装 libcap2-bin"
        fi
    fi

    # 注册 systemd 服务
    info "配置 systemd 服务 /etc/systemd/system/probewatch-agent.service..."
    cat > /etc/systemd/system/probewatch-agent.service <<EOF
[Unit]
Description=ProbeWatch Monitoring Agent
After=network.target network-online.target
Wants=network-online.target

[Service]
Type=simple
User=probewatch
Group=probewatch
EnvironmentFile=/etc/probewatch/agent.env
ExecStart=${BIN_PATH}
Restart=always
RestartSec=10s

# 安全基线沙箱隔离
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/var/lib/probewatch
AmbientCapabilities=CAP_NET_RAW
CapabilityBoundingSet=CAP_NET_RAW

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
    if [[ -f "$BIN_PATH" ]]; then
        systemctl enable --now probewatch-agent.service
        ok "ProbeWatch Agent 服务已启动并设置开机自启！"
        info "可使用以下命令查看状态:"
        echo "  systemctl status probewatch-agent"
        echo "  journalctl -u probewatch-agent -f"
    else
        ok "服务配置完成！在放置 $BIN_PATH 二进制后运行:"
        echo "  systemctl enable --now probewatch-agent"
    fi

elif [[ "$ROLE" == "server" ]]; then
    info "准备安装: ProbeWatch Server (主控端)"
    mkdir -p /etc/probewatch /var/lib/probewatch/data

    if ! id -u probewatch >/dev/null 2>&1; then
        useradd -r -s /usr/sbin/nologin -d /var/lib/probewatch -m probewatch || true
    fi
    chown -R probewatch:probewatch /var/lib/probewatch

    BIN_PATH="/usr/local/bin/probewatch"
    if [[ -f "./probewatch" ]]; then
        cp ./probewatch "$BIN_PATH"
    elif [[ -f "./cmd/probewatch/probewatch" ]]; then
        cp ./cmd/probewatch/probewatch "$BIN_PATH"
    fi

    if [[ -f "$BIN_PATH" ]]; then
        chmod 0755 "$BIN_PATH"
    fi

    cat > /etc/systemd/system/probewatch.service <<EOF
[Unit]
Description=ProbeWatch Control Plane Server
After=network.target

[Service]
Type=simple
User=probewatch
Group=probewatch
EnvironmentFile=-/etc/probewatch/server.env
ExecStart=${BIN_PATH}
Restart=always
RestartSec=5s

NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/var/lib/probewatch

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
    ok "ProbeWatch Server 服务已注册，请在 /etc/probewatch/server.env 配置环境变量后启动"
fi
