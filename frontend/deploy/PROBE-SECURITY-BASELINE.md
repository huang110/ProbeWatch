# Lite-agent 纯监控基线

当前线上节点使用的 Lite-agent 是第三方二进制，不是本项目源码。纯监控场景必须额外核对这些条件：

- 固定版本，不使用 `latest`。
- 显式设置 `--enable-remote-control=false`。
- 显式设置 `--disable-auto-update`。
- 不使用 `2.3.3.0` 及更高版本，除非确认 MCP/远程控制功能被彻底禁用并完成审计。
- Agent 不应监听入站 TCP 端口。
- Agent 不应使用 SSH、WebShell、文件管理、exec 或 MCP。
- systemd 服务不应以 root 运行，除非经过单独权限审计。
- Agent 配置和 Token 文件权限应为 `0600`。
- Agent 目录应只允许专用系统用户写入。
- HTTPS 证书校验必须开启，不得使用 `--ignore-unsafe-cert`。
- 服务单元应使用 `NoNewPrivileges`、`ProtectSystem`、`PrivateTmp` 等限制。
- MTR/ICMP 如需额外权限，应只授予二进制所需 capability，不要恢复 root 服务。

## 当前已知上游风险

Lite-agent `2.3.1.1` 的源码显示：

- 默认 systemd/OpenRC/procd 安装以 root 运行。
- 安装脚本下载发布物时未看到独立 SHA-256/签名校验。
- 自动更新默认开启。
- v2 pull capability 列表包含 `exec`、`remote`、`files`，即使运行时关闭远程控制也会声明这些能力。
- 远程事件会先尝试建立远程 WebSocket，再在会话层判断远程控制是否关闭。

因此“关闭远程控制”是必要条件，但不能替代服务权限隔离和固定版本。

## 使用

先审计，不要直接执行修改：

```bash
sudo bash lite-agent-audit.sh
```

确认节点、服务名和备份后，再应用基线：

```bash
sudo bash lite-agent-pure-monitor-baseline.sh
```

应用后重新执行审计脚本，并检查 Lite 主控节点是否恢复上报。
