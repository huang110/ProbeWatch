# ProbeWatch 前端安全说明

`frontend/` 控制台区分两类视图：

- **游客视图（未登录）**：`/api/me` 返回 401 时进入只读公开状态页，仅读取 `GET /api/public/status` 的脱敏聚合数据（在线/总数、平均延迟、检测通过率、最近更新时间、脱敏节点名列表），并提供 GitHub 登录入口；不渲染管理导航、节点明细、检测目标或告警。
- **管理控制台（已登录）**：GitHub OAuth 会话建立后显示完整管理视图；节点、告警、检测目标等数据全部来自受保护 API。

## 公开状态接口边界（/api/public/status）

- `GET /api/public/status` 无需认证、只读、按客户端限速（429），响应由固定白名单 struct 定义：`nodes.online` / `nodes.total` / `nodes.names`、`checks.success_rate` / `checks.avg_latency_ms`、`last_updated_at`、`generated_at`。
- 节点名发布前做脱敏处理：名称中出现的 IPv4 与 UUID 子串会被遮蔽。
- 该接口明确不暴露节点 IP、UUID、内部节点 ID、注册/节点 Token、资源明细、检测目标与告警详情。
- 响应与其余 `/api/` 路径一致携带 `Cache-Control: no-store`。
- 前端不使用 localStorage/sessionStorage/token，所有状态仅保存在内存中。

## 部署前必须完成

- 由 Go 主控或反向代理提供登录认证和授权。
- 管理后台使用 HttpOnly、Secure、SameSite Cookie。
- 写操作启用 CSRF 防护和服务端权限校验。
- 节点数据从受保护 API 获取，不从前端构建产物读取真实节点信息。
- 私密状态页使用独立高熵 Token，并返回脱敏数据模型。
- API 实现请求限速、审计日志、超时和错误状态处理。
- OpenResty 加载 `frontend/deploy/openresty-security.conf` 中的响应头。
- 轮换仓库、日志、备份和聊天记录中出现过的代理密码与 Secret。
- 不把 `node_modules/`、`dist/`、`.env` 或凭据文件提交到仓库。

## 当前保护

- 演示节点使用 RFC 5737 文档保留地址。
- 页面明确显示演示数据，不宣称实时同步。
- 静态入口包含 CSP 元标签作为安全头缺失时的兜底。
- 生产反向代理安全头片段位于 `frontend/deploy/openresty-security.conf`。
