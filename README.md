# ProbeWatch — 纯监控探针系统

ProbeWatch 是一个自建的轻量级服务器监控系统，定位于**只读纯监控**：节点资源采集、网络可用性检测、MTR 路由追踪、流媒体出口检测，仅此而已。

它的核心理念是**安全边界最小化**——明确不提供 SSH 终端、WebShell、文件管理、远程命令执行等任何运维通道。Agent 只做两件事：本地采集、主动上报。即使主控被攻破，攻击者也无法通过 ProbeWatch 触达你的节点。

## 功能边界

**允许 (设计目标):**

- CPU / 内存 / 磁盘 / 负载 / 网络流量采集 (原生读取 `/proc`，零 CGO 依赖)
- 系统信息 (内核、架构、IPv4/IPv6)
- TCP / HTTP / HTTPS / DNS 可用性检测
- MTR 路由追踪 (底层 Raw Socket ICMP 逐跳探测，非 shell 调用)
- 流媒体出口检测 (结构化检测器框架)
- **实时告警推送 (已支持 Telegram Bot / Webhook)**
- **单二进制运行 (React 前端通过 Go `embed.FS` 嵌入主控二进制)**
- **多模式登录 (支持 GitHub OAuth 与本地管理员密码登录)**
- 公开脱敏只读状态页 (Guest View)

**明确禁止 (硬性边界):**

- ❌ SSH 终端 / WebShell
- ❌ 文件管理 / 任意文件读写
- ❌ 远程命令执行 / 脚本下发
- ❌ Agent 监听端口 (只出站，不入站)
- ❌ 远程安装、升级、卸载 Agent
- ❌ AI 运维操作 (无任何群控后门)

## 架构

```text
┌────────────────────────┐         ┌───────────────────────────┐
│ React 前端 (内嵌单体)  │  HTTPS  │       Go 主控(单体)        │
│ (OAuth / 本地口令登录) │ ──────► │  API / 调度 / SQLite / 告警  │
└────────────────────────┘         └────────────┬──────────────┘
                                                │ 只读配置下发 (拉模式)
                                   ┌────────────┴──────────────┐
                                   │   Go Agent (纯监控探针)   │
                                   │  本地采集 → 主动上报，零监听 │
                                   └───────────────────────────┘
```

- **主控** `cmd/probewatch`: Go 单体服务，内嵌完整前端静态资源，持有 SQLite (WAL)，提供管理 API、口令与 OAuth 认证、Agent 注册/上报 API、Telegram/Webhook 告警推送。
- **Agent** `cmd/probewatch-agent`: 出站-only 探针，周期拉取检测配置、30 秒上报资源快照、执行网络检测，可配置 `cap_net_raw` 权限以普通用户运行 MTR。
- **前端** `frontend/`: React 18 + Vite 中文控制台与公开状态页，支持一键口令登录与 GitHub 授权。

## 安全设计

- **注册**: 一次性注册 Token (TTL 默认 15 分钟)，首次注册换取长期节点 Token，明文只在响应中出现一次。
- **认证**: 节点 Token 服务端只存 HMAC-SHA256 摘要 + pepper；支持轮换与吊销。
- **防重放**: 每个请求要求 `X-Probe-Timestamp` + `X-Probe-Request-ID`，窗口外或重复 ID 直接拒绝，与结果持久化同事务。
- **协议**: 严格 JSON (`DisallowUnknownFields` + 尾随数据拒绝 + 体积上限)，杜绝字段走私。
- **SSRF 防护**: 检测目标全量校验——拒绝回环/内网/链路本地/云元数据地址 (169.254.169.254 等)、DNS rebinding 二次解析、HTTP 重定向逐跳重新校验、禁用代理。
- **Web**: 服务端 Session + 轮换 CSRF + Origin 校验 + 安全 Cookie；支持本地强密码登录或 GitHub OAuth。
- **部署**: 主控默认只绑 loopback；systemd 加固 (`NoNewPrivileges` / `ProtectSystem=strict` / 只写数据目录)。

## 快速开始

### 1. 主控启动

```bash
# 复制配置模板
cp .env.example .env

# 设置管理员口令（无需配置 GitHub OAuth 即可直接登录）
export PROBEWATCH_ADMIN_PASSWORD="your-strong-password"

# 启动单体主控（前端界面已内置，直接访问 8080）
go run ./cmd/probewatch
```
浏览器打开 `http://127.0.0.1:8080` 即可直接看到控制台，点击右上角即可通过管理员密码登录。

### 2. 告警推送配置

支持在环境变量中设置 Telegram Bot 或 Webhook：
```bash
# Telegram 告警
export PROBEWATCH_TELEGRAM_BOT_TOKEN="123456:ABC-DEF..."
export PROBEWATCH_TELEGRAM_CHAT_ID="987654321"

# 或通用 Webhook 告警
export PROBEWATCH_WEBHOOK_URL="https://your-webhook-endpoint.com/alert"
```

### 3. 被控节点安装 (Linux 一键脚本)

在 Linux 被控节点上，可使用 `deploy/install.sh` 脚本一键安装并加固 systemd 服务：

```bash
sudo bash deploy/install.sh agent
```
根据提示输入主控地址、节点 UUID 与注册 Token 即可。脚本将自动：
- 创建非 root 专用运行账号 `probewatch`
- 赋予 `cap_net_raw+ep` 权限以实现免 root 执行底层 ICMP MTR 追踪
- 注册 `probewatch-agent.service` 并配置安全沙箱与开机自启

## 项目结构

```text
cmd/probewatch/         主控入口
cmd/probewatch-agent/   Agent 入口
internal/api/           HTTP 层 (管理/Agent/目标管理/内嵌前端静态分发)
internal/auth/          本地密码认证、GitHub OAuth、Session、CSRF、TOTP
internal/agent/         Agent 运行时 (拉配置、资源采集、离线上报队列)
internal/notify/        Telegram 与 Webhook 告警实时分发器
internal/monitor/       网络检测执行器 (TCP/HTTP/DNS/MTR/流媒体, SSRF 安全)
internal/protocol/      严格线协议类型与字段走私校验
internal/security/      目标校验、Token 哈希、UUID
internal/db/            SQLite 存储 (WAL、自动迁移、重放防护、降采样归档)
frontend/               React 控制台 (已配置 embed.FS 内嵌支持)
deploy/                 一键安装脚本 install.sh、Docker Compose
docs/                   设计稿、实施计划、本地验证手册
```

## 开发状态与版本

- 当前稳定版本：**v0.2.1**
- 详细迭代记录见：[版本更新日志 (CHANGELOG.md)](CHANGELOG.md)

**已就绪功能 (v0.2.1):**

- ✅ **平滑曲线时序监控与可调采样周期**：窗口流量与探测目标全量支持平滑流线图走势、30秒基准时钟、系统设置内自由配置 10s~300s 轮询周期
- ✅ **Linear.app 极简单层黑曜石设计**：深邃黑曜石质感、1px 细微光边框、4px 微型体质条与原生快捷键
- ✅ **高级流量分析与质量看板**：时序时段柱状图、峰值分析、多协议过滤与加权可用率 SLA 统计
- ✅ **深度节点诊断抽屉**：MTR 骨干网路由追踪、全球流媒体解锁能力矩阵与系统规格指纹
- ✅ **小鸡资产台账与估值**：多币种汇率换算、剩余价值折算、到期报警与论坛发帖生成器
- ✅ 前端静态资源完整内嵌，单二进制启动即用
- ✅ 管理员本地密码快速登录 + GitHub OAuth 登录
- ✅ 主控完整生命周期: 注册 Token → Agent 注册 → 配置下发 → 上报 → 吊销
- ✅ 资源采集: CPU / 内存 / 磁盘 / 负载 / 流量 (原生 `/proc`，无外部 C 依赖)
- ✅ TCP / HTTP / HTTPS / DNS 检测 (含 SSRF 全套防护)
- ✅ Linux 底层 Raw Socket MTR 路由追踪
- ✅ Telegram Bot / Webhook 告警实时推送
- ✅ Linux systemd 一键安装加固脚本

## License

私有项目，保留所有权利。
