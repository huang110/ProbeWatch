#!/usr/bin/env bash
set -eu

# Run as root on a Linux VPS after verifying the host and service name.
# This script does not download or upgrade the agent. It only hardens an
# existing Lite-agent installation for monitoring-only use.

service_name="${LITE_AGENT_SERVICE:-lite-agent.service}"
unit="/etc/systemd/system/${service_name}"

if [ "$(id -u)" -ne 0 ]; then
  echo "run as root" >&2
  exit 1
fi

if ! command -v systemctl >/dev/null 2>&1; then
  echo "systemd is required by this baseline" >&2
  exit 1
fi

if [ ! -f "$unit" ]; then
  echo "service unit not found: $unit" >&2
  exit 1
fi

install -d -m 0755 /etc/systemd/system/${service_name}.d
cat > /etc/systemd/system/${service_name}.d/10-pure-monitor.conf <<'EOF'
[Service]
# The agent must not expose terminal, file, exec, or MCP control.
Environment=AGENT_REMOTE_CONTROL_ENABLED=false
# Prevent unattended replacement by a newly published binary.
Environment=AGENT_DISABLE_AUTO_UPDATE=true
# Run with the least privilege. MTR/ICMP may require a narrowly scoped
# capability; grant it to the binary only after reviewing the host policy.
User=lite-agent
Group=lite-agent
NoNewPrivileges=true
PrivateTmp=true
PrivateDevices=true
ProtectSystem=strict
ProtectHome=read-only
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictSUIDSGID=true
LockPersonality=true
RestrictRealtime=true
RestrictNamespaces=true
SystemCallArchitectures=native
CapabilityBoundingSet=CAP_NET_RAW
AmbientCapabilities=CAP_NET_RAW
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
ReadWritePaths=/opt/lite-agent
EOF

if ! id lite-agent >/dev/null 2>&1; then
  useradd --system --home-dir /opt/lite-agent --shell /usr/sbin/nologin lite-agent
fi

chown -R lite-agent:lite-agent /opt/lite-agent
find /opt/lite-agent -type f -name '*.json' -exec chmod 600 {} \;
chmod 0755 /opt/lite-agent/Lite-agent

systemctl daemon-reload
systemctl restart "$service_name"
systemctl is-active --quiet "$service_name"

echo "pure-monitor baseline applied: $service_name"
systemctl show "$service_name" --property=User,Group,NoNewPrivileges,PrivateTmp,ProtectSystem,ProtectHome,Environment
ss -lntup | grep -E 'lite-agent|:25774|:27777' || true
