# ProbeWatch 可用版设计

## 目标

将当前仅有演示数据的 React 控制台，扩展为一个可以在本机验证的纯监控系统。第一阶段必须跑通真实闭环：管理员登录、节点注册、Agent 上报、资源展示、网络检测、MTR、流媒体 HTTP 检测器和节点安全管理。

第一阶段不替换现有 Lite，不部署到甲骨文，不修改现有 Lite 数据。主控先以本机进程运行，Agent 连接本机主控或通过可配置 HTTPS 地址连接测试环境。

## 非目标

第一阶段不实现：

- SSH、WebShell、终端、文件管理、远程命令、MCP
- Agent 入站 HTTP/WebSocket 监听
- Agent 自动更新
- Telegram/Webhook 告警
- 历史曲线、降采样和长期数据清理
- 私密状态页
- Netflix、Disney+ 等平台专用解锁判断
- 账单、成本中心和服务器生命周期管理

告警、历史和状态页作为第二阶段，不在第一阶段验收范围内。

## 架构

```text
React SPA
    │ same-origin HTTP API + Session Cookie
    ▼
Go 主控服务
    ├── GitHub OAuth / Session / CSRF
    ├── Admin API
    ├── Agent 注册与上报 API
    ├── 结构化检测配置 API
    ├── SQLite
    └── 静态资源服务
             ▲ HTTPS 主动上报
             │ Bearer 节点 Token
        Go 纯监控 Agent
```

主控采用单体服务，前端静态资源由 Go 服务同源提供，避免本机验证时额外配置跨域。SQLite 使用 WAL，第一阶段只保存当前状态和最近检测结果，不承诺长期历史能力。

Agent 只主动建立出站 HTTPS 连接，绝不监听入站端口。主控下发的内容仅限结构化检测配置，不能包含 shell、命令、脚本、可执行文件、任意路径或任意请求头。

## 技术栈

- 主控：Go 1.23+，标准库 HTTP，SQLite 驱动
- Agent：Go 1.23+，Linux amd64/arm64
- 前端：现有 React/Vite/Phosphor UI
- 数据库：SQLite
- 管理认证：GitHub OAuth
- 密码/Token 哈希：Argon2id 或 SHA-256 HMAC，长期节点 Token 仅保存哈希
- 传输：HTTPS；开发环境允许 `http://127.0.0.1`，生产默认要求 HTTPS

## 管理员认证

环境变量：

```text
GITHUB_CLIENT_ID
GITHUB_CLIENT_SECRET
GITHUB_REDIRECT_URL
GITHUB_ALLOWED_USERS
GITHUB_ALLOWED_ORG
SESSION_SECRET
```

登录流程：

1. 前端请求 `/auth/github`。
2. 主控生成随机 OAuth state 并写入短期 HttpOnly Cookie。
3. 浏览器跳转 GitHub。
4. 回调校验 state、code 和 redirect URI。
5. 主控请求 GitHub 用户身份。
6. 校验用户白名单或组织成员资格。
7. 创建服务端 Session。
8. 返回同源 HttpOnly、Secure、SameSite=Lax Cookie。

要求：

- OAuth state 一次性使用并限时。
- Session 不把 GitHub access token 放到浏览器可读存储。
- 不使用 localStorage 保存认证信息。
- 所有写 API 校验 CSRF token 或 Origin/Referer。
- 未授权用户只能得到 401/403，不返回节点数据。
- OAuth 错误不把 provider 原始响应回显给用户。

本机开发允许非 Secure Cookie，仅绑定 `127.0.0.1`；生产环境强制 HTTPS 和 Secure Cookie。

## Agent 注册与节点 Token

管理员在后台请求一次性注册 Token，主控返回安装参数：

```text
endpoint
registration_token
expires_at
```

注册 Token 要求：

- 随机生成，至少 32 字节熵。
- 仅保存哈希。
- 默认 15 分钟过期。
- 成功注册后立即消费。
- 只能注册一个节点。
- 不能读取已有节点 Token。

注册请求包含：

```json
{
  "registration_token": "...",
  "node_uuid": "...",
  "name": "...",
  "system": {
    "os": "linux",
    "arch": "amd64",
    "hostname": "...",
    "agent_version": "..."
  }
}
```

主控成功后返回长期节点 Token。长期 Token 只在 Agent 本地配置文件保存一次，文件权限 `0600`；主控只保存哈希和 token 前缀用于识别。

节点上报使用：

```text
Authorization: Bearer <node-token>
X-Probe-Timestamp: <unix-seconds>
X-Probe-Request-ID: <random-id>
```

主控拒绝：

- 过期时间戳。
- 重复 request ID。
- 超过请求体大小。
- 被吊销的 Token。
- 频率超限。
- 不匹配的节点 UUID。

## Agent 运行边界

Agent 服务单元必须：

- 使用专用非 root 用户。
- `NoNewPrivileges=true`。
- `PrivateTmp=true`。
- `ProtectSystem=strict`。
- `ProtectHome=read-only`。
- 不监听 TCP/UDP 入站端口。
- 禁用自动更新。
- 不包含 SSH、终端、文件管理、exec、MCP 代码路径。
- MTR 只使用受限 ICMP capability；优先 `CAP_NET_RAW`，不以 root 运行整个进程。

Agent 不接受命令。主控发送的任务只有以下结构：

```json
{
  "target_id": "...",
  "kind": "tcp|http|https|dns|mtr|media_http",
  "host": "example.com",
  "port": 443,
  "path": "/health",
  "timeout_ms": 3000,
  "max_hops": 20
}
```

字段白名单、长度、端口、超时、并发和频率均在 Agent 与主控双侧校验。

## 资源上报

默认上报间隔 30 秒。上报字段：

- CPU 使用率和 load 1/5/15。
- 内存总量、已用、可用、Swap。
- 主要文件系统容量和使用率。
- 网卡累计上下行字节数。
- 操作系统、内核、架构、主机名。
- Agent 版本和启动时间。
- 当前检测任务摘要。

接口：

```text
POST /api/agent/v1/report
```

主控返回配置版本和下一轮允许执行的结构化检测任务。返回内容不能包含任意命令或路径。

## 网络检测

目标模板由主控管理，每个节点单独启用或禁用。

支持：

- TCP Connect
- HTTP GET
- HTTPS GET
- DNS A/AAAA/CNAME

目标字段：

```text
name
kind
host
port
path
expected_status
dns_type
timeout_ms
interval_seconds
enabled
```

安全要求：

- 默认阻断 `127.0.0.0/8`、`::1`、RFC1918、链路本地、云元数据地址和 Unix socket。
- DNS 解析结果重新校验，防止 DNS rebinding 绕过地址过滤。
- HTTP 只允许 GET，禁止任意请求头和请求体。
- 限制重定向次数，并对每次重定向重新进行目标校验。
- 响应正文只读取有限字节，不保存完整正文。
- 单节点检测并发有上限。
- 所有目标和结果记录执行超时。

## MTR

Agent 使用内置受限 MTR/ICMP 实现，避免拼接 shell 命令调用系统 `mtr`。

参数限制：

- 目标必须通过域名/IP校验。
- 最大跳数 1-30。
- 单跳超时不超过 1 秒。
- 单次执行总时长不超过 45 秒。
- 单节点 MTR 并发为 1。
- IPv4/IPv6 显式选择。

结果保存：

- 目标。
- 解析后的目标 IP。
- 每跳 TTL、IP、延迟、超时。
- 末端是否到达。
- 路径指纹。
- 错误分类。

中间跳丢包但末端可达时，不判定为真实线路丢包；末端连续失败才判定为目标不可达。

## 流媒体检测框架

第一阶段只实现自定义 HTTP 检测器框架，不实现平台专用解锁判断。

检测器只能声明：

- 固定 URL 模板。
- 固定 GET 方法。
- 有限请求头白名单。
- 状态码判断。
- 重定向判断。
- 有限正文匹配规则。
- 区域结果映射。

禁止：

- 用户 Cookie。
- 密码和登录凭据。
- 验证码绕过。
- 任意脚本。
- 任意请求头。
- 任意代理跳转。
- 保存完整响应正文。

标准结果：

```json
{
  "detector": "custom-http",
  "status": "available|blocked|region_limited|unknown|timeout|error",
  "region": "SG",
  "latency_ms": 120,
  "reason": "...",
  "checked_at": "..."
}
```

## SQLite 模型

第一阶段表：

```text
admin_users
sessions
oauth_states
registration_tokens
nodes
node_tokens
resource_latest
network_targets
network_results_latest
mtr_targets
mtr_results_latest
media_detectors
media_results_latest
audit_events
```

所有 Token 使用哈希存储。节点删除和 Token 吊销写审计事件。SQLite 文件和备份文件权限为 `0600`，数据目录为 `0700`。

## API

管理 API：

```text
GET  /api/me
GET  /api/nodes
POST /api/registration-tokens
POST /api/nodes/:id/rotate-token
POST /api/nodes/:id/revoke
GET  /api/targets
POST /api/targets
PATCH /api/targets/:id
DELETE /api/targets/:id
GET  /api/nodes/:id/mtr
GET  /api/nodes/:id/media
```

Agent API：

```text
POST /api/agent/v1/register
POST /api/agent/v1/report
POST /api/agent/v1/network-result
POST /api/agent/v1/mtr-result
POST /api/agent/v1/media-result
```

所有管理写操作需要管理员 Session + CSRF；所有 Agent API 需要节点 Token + 时间戳 + request ID。

## 前端接入

现有中文控制台保留视觉结构，但删除演示数据默认值，改为：

- 初始加载显示“正在连接主控”。
- API 成功后显示真实数据。
- API 401 跳转 GitHub OAuth。
- API 超时显示“主控不可达”，不能显示在线状态。
- 没有数据时显示空状态，不伪造 0/12 或检测通过率。
- 前端不保存 Agent Token。
- 节点详情从 API 获取，状态页数据模型与后台模型分开。

## 错误处理

- Agent 网络失败：指数退避并保留本地最近一次资源采样，不伪造成功。
- 主控返回 401：清理 Session 并跳转登录。
- 主控返回 429：遵守 Retry-After。
- 检测超时：记录 timeout，不把超时转换为成功。
- MTR 缺少权限：返回 `dependency_missing`，不自动以 root 重启。
- SQLite 写入失败：返回 503，记录服务端错误，不丢弃认证失败原因。

## 第一阶段验收

1. GitHub OAuth 登录成功，未授权 GitHub 用户被拒绝。
2. OAuth state 不能重复使用。
3. 管理 API 未登录返回 401。
4. 可以生成一次性注册 Token，15 分钟后失效。
5. Agent 注册后显示在线。
6. 节点 Token 仅保存哈希，吊销后上报返回 401。
7. Agent 上报 CPU、内存、磁盘、流量和系统信息。
8. TCP/HTTP/HTTPS/DNS 目标可以返回结构化结果。
9. 内网、回环和云元数据目标被拒绝。
10. MTR 能返回受限 hop 结果，不能执行 shell。
11. 自定义 HTTP 流媒体检测器可以返回标准化结果。
12. Agent 无监听端口，systemd 用户不是 root。
13. Agent 不包含 SSH、终端、文件、exec、MCP 功能入口。
14. 前端真实显示加载、未连接、401、403、429、空数据和错误状态。

## 第二阶段

- 资源和检测历史曲线。
- 7 天原始数据、90 天聚合数据。
- 离线、阈值、路径变化和流媒体状态变化告警。
- Telegram/Webhook 通知。
- 私密只读状态页。
- 平台专用流媒体适配器。
