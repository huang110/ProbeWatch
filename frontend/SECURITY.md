# ProbeWatch 前端安全说明

当前 `frontend/` 是演示控制台，不包含登录、Session、API 鉴权或 Agent 通信。

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
