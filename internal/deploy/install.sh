#!/bin/sh
# ==============================================================================
# ProbeWatch Linux / OpenWrt / Alpine 一键安装与服务配置脚本 (v0.5.9)
# 支持环境:
#   - Linux (systemd: Debian, Ubuntu, CentOS, Rocky, Arch, Fedora)
#   - OpenWrt / iStoreOS / ImmortalWrt (procd: x86_64, aarch64, arm, mips, mipsle)
#   - Alpine Linux (OpenRC: rc-service, rc-update)
# 安全加固:
#   - 优先专用低权限账户 (Linux systemd 下使用 probewatch 用户)
#   - 兼容 MTR raw socket 网络探测能力
# ==============================================================================

set -eu

# 终端着色支持 (兼容缺少 tput 的 BusyBox)
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
PLAIN='\033[0m'

info()  { printf "%b[INFO]%b %s\n" "${BLUE}" "${PLAIN}" "$*"; }
ok()    { printf "%b[OK]%b %s\n" "${GREEN}" "${PLAIN}" "$*"; }
warn()  { printf "%b[WARN]%b %s\n" "${YELLOW}" "${PLAIN}" "$*"; }
error() { printf "%b[ERROR]%b %s\n" "${RED}" "${PLAIN}" "$*" >&2; }

# 必须以 root 或 sudo 执行
if [ "$(id -u 2>/dev/null || echo 1)" -ne 0 ]; then
    error "本脚本必须以 root 权限运行，请使用 sudo 或 root 终端执行"
    exit 1
fi

# 1. 架构检测 (支持 PC 与主流软路由芯片架构)
ARCH=$(uname -m 2>/dev/null || echo "unknown")
case "$ARCH" in
    x86_64|amd64)
        TARGET_ARCH="amd64"
        ;;
    aarch64|arm64|armv8*)
        TARGET_ARCH="arm64"
        ;;
    armv7*|armhf)
        TARGET_ARCH="arm"
        ;;
    armv6*|armv5*|arm)
        TARGET_ARCH="arm"
        ;;
    mips)
        TARGET_ARCH="mips"
        ;;
    mipsel|mipsle)
        TARGET_ARCH="mipsle"
        ;;
    i386|i686)
        TARGET_ARCH="386"
        ;;
    riscv64)
        TARGET_ARCH="riscv64"
        ;;
    *)
        TARGET_ARCH="$ARCH"
        warn "未明确匹配的硬件架构: $ARCH，尝试默认回退"
        ;;
esac

info "检测到系统硬件架构: $ARCH (目标二进制架构: $TARGET_ARCH)"

# 2. 服务管理系统判定 (OpenWrt procd / OpenRC / systemd)
INIT_SYSTEM="systemd"
if [ -f /etc/openwrt_release ] || [ -f /etc/rc.common ] || [ -x /sbin/procd ]; then
    INIT_SYSTEM="openwrt"
elif [ -x /sbin/openrc-run ] || ([ -x /sbin/rc-service ] && [ ! -d /run/systemd/system ]); then
    INIT_SYSTEM="openrc"
elif [ -d /run/systemd/system ] || command -v systemctl >/dev/null 2>&1; then
    INIT_SYSTEM="systemd"
else
    INIT_SYSTEM="systemd"
fi

info "检测到系统服务管理器: $INIT_SYSTEM"

# 3. 运行角色选择 (agent 或 uninstall-agent)
ACTION="${1:-agent}"

if [ "$ACTION" = "uninstall-agent" ] || [ "$ACTION" = "uninstall" ]; then
    info "准备卸载 ProbeWatch Agent..."
    if [ "$INIT_SYSTEM" = "openwrt" ]; then
        if [ -x /etc/init.d/probewatch-agent ]; then
            /etc/init.d/probewatch-agent stop 2>/dev/null || true
            /etc/init.d/probewatch-agent disable 2>/dev/null || true
            rm -f /etc/init.d/probewatch-agent
        fi
    elif [ "$INIT_SYSTEM" = "openrc" ]; then
        rc-service probewatch-agent stop 2>/dev/null || true
        rc-update del probewatch-agent default 2>/dev/null || true
        rm -f /etc/init.d/probewatch-agent
    else
        if command -v systemctl >/dev/null 2>&1; then
            systemctl disable --now probewatch-agent.service 2>/dev/null || true
            rm -f /etc/systemd/system/probewatch-agent.service
            systemctl daemon-reload 2>/dev/null || true
        fi
    fi
    rm -f /usr/local/bin/probewatch-agent /usr/bin/probewatch-agent
    rm -rf /etc/probewatch
    ok "ProbeWatch Agent 服务与配置已完全清理卸载！"
    exit 0
fi

# 4. Agent 接入配置处理
ENDPOINT="${PROBEWATCH_AGENT_ENDPOINT:-}"
NODE_UUID="${PROBEWATCH_AGENT_NODE_UUID:-}"
NODE_TOKEN="${PROBEWATCH_AGENT_NODE_TOKEN:-}"
REG_TOKEN="${PROBEWATCH_AGENT_REGISTRATION_TOKEN:-}"

if [ -z "$ENDPOINT" ]; then
    printf "请输入主控上报地址 (例如 https://monitor.example.com/api/agent/v1): "
    read -r ENDPOINT
fi
if [ -z "$NODE_UUID" ]; then
    printf "请输入节点 UUID: "
    read -r NODE_UUID
fi

# 如果未提供长期 Token，但提供了 15 分钟临时 Registration Token，自动向主控换取长期 Node Token
if [ -z "$NODE_TOKEN" ] && [ -n "$REG_TOKEN" ]; then
    info "检测到临时注册凭据，正在自动向主控注册并换取长期凭证..."
    NODE_NAME="$(hostname 2>/dev/null || cat /etc/hostname 2>/dev/null || echo "ProbeWatch-Node")"
    REG_BODY="{\"registration_token\":\"${REG_TOKEN}\",\"node_uuid\":\"${NODE_UUID}\",\"name\":\"${NODE_NAME}\"}"

    REG_RESP=""
    if command -v curl >/dev/null 2>&1; then
        REG_RESP=$(curl -sS -k -X POST -H "Content-Type: application/json" -d "$REG_BODY" "${ENDPOINT}/register" 2>/dev/null || true)
    elif command -v wget >/dev/null 2>&1; then
        REG_RESP=$(wget -qO- --post-data="$REG_BODY" --header="Content-Type: application/json" "${ENDPOINT}/register" 2>/dev/null || true)
    fi

    # 从 JSON 响应提取 node_token
    NODE_TOKEN=$(printf "%s" "$REG_RESP" | sed -n 's/.*"node_token":"\([^"]*\)".*/\1/p' || true)
    if [ -n "$NODE_TOKEN" ]; then
        ok "节点注册成功！已安全换取长期 Node Token"
    else
        error "向主控自动注册失败，主控响应: $REG_RESP"
        info "请检查主控网络连通性或重新在后台生成临时 Token。"
        exit 1
    fi
fi

if [ -z "$NODE_TOKEN" ]; then
    printf "请输入节点长期 Token: "
    read -r NODE_TOKEN
fi

if [ -z "$ENDPOINT" ] || [ -z "$NODE_UUID" ] || [ -z "$NODE_TOKEN" ]; then
    error "缺少必要的安装参数 (Endpoint, UUID, Token)，安装中止"
    exit 1
fi

# 5. 配置目录准备
mkdir -p /etc/probewatch /var/lib/probewatch
DATA_DIR="/var/lib/probewatch"

# 6. 二进制程序下载与放置
BIN_PATH="/usr/local/bin/probewatch-agent"
if [ "$INIT_SYSTEM" = "openwrt" ]; then
    # OpenWrt 规范常用 /usr/bin
    BIN_PATH="/usr/bin/probewatch-agent"
fi

if [ ! -f "$BIN_PATH" ] && [ ! -f "./probewatch-agent" ]; then
    # 尝试从主控下载对应架构的二进制
    SERVER_BASE="${ENDPOINT%/api/agent/v1}"
    DOWNLOAD_URL="${SERVER_BASE}/api/agent/v1/update/download?os=linux&arch=${TARGET_ARCH}"
    info "正在从主控下载 ProbeWatch Agent 二进制 (${TARGET_ARCH})..."
    
    DOWNLOAD_SUCCESS=0
    if command -v curl >/dev/null 2>&1; then
        if curl -sSL -k -H "Authorization: Bearer ${NODE_TOKEN}" -o "$BIN_PATH" "$DOWNLOAD_URL"; then
            DOWNLOAD_SUCCESS=1
        fi
    elif command -v wget >/dev/null 2>&1; then
        if wget -qO "$BIN_PATH" --header="Authorization: Bearer ${NODE_TOKEN}" "$DOWNLOAD_URL"; then
            DOWNLOAD_SUCCESS=1
        fi
    fi

    if [ "$DOWNLOAD_SUCCESS" -eq 1 ] && [ -s "$BIN_PATH" ]; then
        chmod 0755 "$BIN_PATH"
        ok "Agent 二进制下载成功: $BIN_PATH"
    else
        rm -f "$BIN_PATH"
        warn "从主控自动下载二进制未完成，请确认网络或手动放置 agent 到 $BIN_PATH"
    fi
elif [ -f "./probewatch-agent" ]; then
    cp ./probewatch-agent "$BIN_PATH"
    chmod 0755 "$BIN_PATH"
    ok "已使用当前目录的 probewatch-agent 二进制"
fi

# 为二进制授予 ICMP 探测权限
if [ -f "$BIN_PATH" ]; then
    chmod 0755 "$BIN_PATH"
    if command -v setcap >/dev/null 2>&1; then
        setcap cap_net_raw+ep "$BIN_PATH" 2>/dev/null || true
    fi
fi

# 7. 写入配置文件
cat > /etc/probewatch/agent.env <<EOF
PROBEWATCH_ENV=production
PROBEWATCH_AGENT_ENDPOINT=${ENDPOINT}
PROBEWATCH_AGENT_NODE_UUID=${NODE_UUID}
PROBEWATCH_AGENT_NODE_TOKEN=${NODE_TOKEN}
PROBEWATCH_AGENT_DATA=${DATA_DIR}
EOF
chmod 0600 /etc/probewatch/agent.env

# 8. 根据不同服务管理器安装与启动守护
if [ "$INIT_SYSTEM" = "openwrt" ]; then
    info "配置 OpenWrt procd 守护服务 /etc/init.d/probewatch-agent..."
    cat > /etc/init.d/probewatch-agent <<'EOF'
#!/bin/sh /etc/rc.common
# ProbeWatch Agent procd init script for OpenWrt / iStoreOS / ImmortalWrt

START=99
STOP=10
USE_PROCD=1

PROG=/usr/bin/probewatch-agent
ENV_FILE=/etc/probewatch/agent.env

start_service() {
    [ -x "$PROG" ] || return 1
    [ -f "$ENV_FILE" ] || return 1

    procd_open_instance
    procd_set_param command "$PROG"
    procd_set_param respawn 3600 5 0
    procd_set_param stdout 1
    procd_set_param stderr 1

    # 加载环境变量
    while IFS='=' read -r key val || [ -n "$key" ]; do
        case "$key" in
            \#*|"") continue ;;
            *)
                clean_val=$(printf "%s" "$val" | sed -e 's/^"//' -e 's/"$//')
                procd_set_param env "$key=$clean_val"
                ;;
        esac
    done < "$ENV_FILE"

    procd_close_instance
}

service_triggers() {
    procd_add_reload_trigger "probewatch"
}
EOF
    chmod 0755 /etc/init.d/probewatch-agent
    /etc/init.d/probewatch-agent enable
    /etc/init.d/probewatch-agent restart

    ok "OpenWrt ProbeWatch Agent 已配置并随系统开机自启！"
    info "常用运维命令:"
    echo "  查看日志: logread -e probewatch-agent -f"
    echo "  服务状态: /etc/init.d/probewatch-agent status"
    echo "  重启服务: /etc/init.d/probewatch-agent restart"

elif [ "$INIT_SYSTEM" = "openrc" ]; then
    info "配置 Alpine OpenRC 服务 /etc/init.d/probewatch-agent..."
    cat > /etc/init.d/probewatch-agent <<'EOF'
#!/sbin/openrc-run
description="ProbeWatch Monitoring Agent"

command="/usr/local/bin/probewatch-agent"
command_background="yes"
pidfile="/run/probewatch-agent.pid"
output_log="/var/log/probewatch-agent.log"
error_log="/var/log/probewatch-agent.log"

depend() {
    need net
    after firewall
}

start_pre() {
    if [ -f /etc/probewatch/agent.env ]; then
        set -a
        . /etc/probewatch/agent.env
        set +a
    fi
}
EOF
    chmod 0755 /etc/init.d/probewatch-agent
    rc-update add probewatch-agent default 2>/dev/null || true
    rc-service probewatch-agent restart 2>/dev/null || true
    ok "Alpine OpenRC ProbeWatch Agent 服务已启动！"

else
    # 默认为 Linux systemd
    info "配置 Linux systemd 服务 /etc/systemd/system/probewatch-agent.service..."

    # 创建专用 probewatch 用户
    if ! id -u probewatch >/dev/null 2>&1; then
        info "创建系统专用非 root 账户 probewatch..."
        useradd -r -s /usr/sbin/nologin -d /var/lib/probewatch -m probewatch 2>/dev/null || true
    fi
    chown -R probewatch:probewatch /var/lib/probewatch 2>/dev/null || true
    chown probewatch:probewatch /etc/probewatch/agent.env 2>/dev/null || true

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
RestartSec=8s

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
    if [ -f "$BIN_PATH" ]; then
        systemctl enable --now probewatch-agent.service
        ok "ProbeWatch Agent 服务已成功启动并启用开机自启！"
        info "常用命令:"
        echo "  systemctl status probewatch-agent"
        echo "  journalctl -u probewatch-agent -f"
    else
        ok "系统服务已注册完成！请放置二进制文件后启动:"
        echo "  systemctl enable --now probewatch-agent"
    fi
fi
