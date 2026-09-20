# ProbeWatch — 纯监控探针系统

ProbeWatch 是一个自建的轻量级服务器监控系统,定位于**只读纯监控**:节点资源采集、网络可用性检测、MTR 路由追踪、流媒体出口检测,仅此而已。

它的核心理念是**安全边界最小化**——明确不提供 SSH 终端、WebShell、文件管理、远程命令执行等任何运维通道。Agent 只做两件事:本地采集、主动上报。即使主控被攻破,攻击者也无法通过 ProbeWatch 触达你的节点。

## 功能边界

**允许(设计目标):**

- CPU / 内存 / 磁盘 / 负载 / 网络流量采集
- 系统信息(内核、架构、IPv4/IPv6)
- TCP / HTTP / HTTPS / DNS 可用性检测
- MTR 路由追踪(路径指纹、丢包、跳变)
- 流媒体出口检测(结构化检测器框架)
- 告警(Telegram / Webhook,规划中)
- 私密只读状态页(规划中)

**明确禁止(硬性边界):**

- ❌ SSH 终端 / WebShell
- ❌ 文件管理 / 任意文件读写
- ❌ 远程命令执行 / 脚本下发
- ❌ Agent 监听端口(只出站,不入站)
- ❌ 远程安装、升级、卸载 Agent
- ❌ AI 运维操作

## 架构

```text
┌──────────────────┐         ┌───────────────────────────┐
│  React 管理控制台  │  HTTPS  │       Go 主控(单体)        │
│  (GitHub OAuth)  │ ──────► │  API / 调度 / SQLite / 告警  │
└──────────────────┘         └────────────┬──────────────┘
                                          │ 只读配置下发(拉模式)
                             ┌────────────┴──────────────┐
                             │   Go Agent(纯监控探针)      │
                             │  本地采集 → 主动上报,零监听   │
                             └───────────────────────────┘
```

- **主控** `cmd/probewatch`:Go 单体服务,持有 SQLite(WAL),提供 OAuth 登录、管理 API、Agent 注册/上报 API、检测目标管理
- **Agent** `cmd/probewatch-agent`:出站-only 探针,周期拉取检测配置、30 秒上报资源快照、执行网络检测
- **前端** `frontend/`:React 18 + Vite 中文控制台(总览/节点/MTR/流媒体/告警)

## 安全设计

- **注册**:一次性注册 Token(TTL 默认 15 分钟),首次注册换取长期节点 Token,明文只在响应中出现一次
- **认证**:节点 Token 服务端只存 HMAC-SHA256 摘要 + pepper;支持轮换与吊销
- **防重放**:每个请求要求 `X-Probe-Timestamp` + `X-Probe-Request-ID`,窗口外或重复 ID 直接拒绝,与结果持久化同事务
- **协议**:严格 JSON(`DisallowUnknownFields` + 尾随数据拒绝 + 体积上限),杜绝字段走私
- **SSRF 防护**:检测目标全量校验——拒绝回环/内网/链路本地/云元数据地址、DNS rebinding 二次解析、HTTP 重定向逐跳重新校验、禁用代理
- **Web**:服务端 Session + 轮换 CSRF + Origin 校验 + 安全 Cookie;生产环境强制 HTTPS 与 32 字节以上密钥
- **部署**:主控默认只绑 loopback;systemd 加固(NoNewPrivileges / ProtectSystem=strict / 只写 data 目录)

## 快速开始

### 主控(本地开发)

```bash
cp .env.example .env   # 按需修改
go run ./cmd/probewatch
curl http://127.0.0.1:8080/healthz
```

生产环境必须提供 `GITHUB_CLIENT_ID/SECRET`、`SESSION_SECRET`、`PROBEWATCH_TOKEN_PEPPER`、`AGENT_NODE_TOKEN_TTL` 与 GitHub 用户/组织白名单——缺失时进程会拒绝启动(有意为之)。

### Agent(节点上)

```bash
PROBEWATCH_ENV=development \
PROBEWATCH_AGENT_ENDPOINT=https://<主控>/api/agent/v1 \
PROBEWATCH_AGENT_NODE_UUID=<RFC4122 UUID> \
PROBEWATCH_AGENT_NODE_TOKEN=<注册下发的节点 Token> \
./probewatch-agent
```

Agent 启动后拉取启用的检测目标,每 30 秒上报一次;不支持也不会监听任何端口。

### 前端

```bash
cd frontend && npm install && npm run dev   # 开发:127.0.0.1:5173,代理 /api
npm run build                                # 生产构建
```

## 测试

```bash
go test ./...                # 后端全量(协议/认证/SSRF/Agent/集成)
cd frontend && python -m pytest tests/ -q   # 前端安全契约 + 浏览器回归
```

## 项目结构

```text
cmd/probewatch/         主控入口
cmd/probewatch-agent/   Agent 入口
internal/api/           HTTP 层(管理/Agent/目标管理/配置下发)
internal/auth/          GitHub OAuth、Session、CSRF
internal/agent/         Agent 运行时(拉配置、采集、上报)
internal/monitor/       网络检测执行器(TCP/HTTP/DNS,SSRF 安全)
internal/protocol/      严格线协议类型与校验
internal/security/      目标校验、Token、UUID
internal/db/            SQLite 存储(WAL、迁移、重放防护)
frontend/               React 控制台(中文)
deploy/                 Docker Compose、本地部署
docs/                   设计稿、实施计划、本地验证手册
```

## 开发状态

**可用:**

- 主控完整生命周期:OAuth 登录 → 创建注册 Token → Agent 注册 → 配置下发 → 上报 → 管理/吊销
- TCP / HTTP / HTTPS / DNS 检测(含 SSRF 全套防护)
- Agent 资源上报与检测执行闭环
- 中文演示控制台(等待接入真实 API)

**进行中 / 规划:**

- MTR 执行器(Go ICMP 实现,协议与存储已就绪,Agent 暂时安全跳过该类任务)
- 流媒体检测器(结构化 HTTP 检测框架)
- 真实 CPU/内存/磁盘/流量采集(当前上报系统身份信息)
- 历史数据与降采样(7 天原始 + 90 天聚合)
- Telegram / Webhook 告警
- 前端接入真实 API、私密状态页

## License

私有项目,保留所有权利。
