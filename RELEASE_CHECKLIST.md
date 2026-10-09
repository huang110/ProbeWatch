# ProbeWatch 正式版发布准入审查清单 (Release Checklist)

> **当前候选版本**：`v0.9.3-rc` (Release Candidate)<br>
> **生产二进制构建提交**：`48537dee39849d4fc6214c0eb3fd0e9f4aff35c0`<br>
> **生产 CURRENT_COMMIT 标记**：`dcc2d740cfcb401c089bc1519862ae94b9d48f97`<br>
> **当前源码仓库 HEAD**：`550dbb09c9352972dc740636715d0a06c5424437`（及后续文档提交）<br>
> **提交与版本说明**：当前源码仓库 HEAD 仅包含发布准入清单及说明文档的修订，相关提交未进入生产二进制，亦未重启线上服务；生产环境实际运行的二进制构建自 `48537dee`，线上部署标记为 `dcc2d740`（两者差异仅为外部测试脚本，应用源码完全一致）。按规范不改写生产部署标记，不重新部署或重启。<br>
> **技术适用范围**：当前架构与功能在技术实现上适用于自建轻量监控、探针出站资源采集、ICMP/MTR 探测与 IPQA 归档渲染。因开源许可证处于待确认状态，本清单不作任何形式的开源或商业授权结论，公开开源发布前必须由项目所有者最终确认许可证。<br>
> **许可证状态**：**待确认 (License: Pending Confirmation - PENDING)**

---

## 一、最终发布准入验收总表 (Final Acceptance Matrix)

| 审查项 | 状态 | 测试类别 | 验证范围与依据 | 测试脚本 / 记录 |
| :--- | :---: | :---: | :--- | :--- |
| **1. Go 全量测试** | **PASS** | 模拟与单元测试 | 全包 19 个模块单测及 API 模拟通过，覆盖 RBAC、路由、会话、Agent 协议 | `go test -count=1 ./...` (耗时 60.7s) |
| **2. Go 静态检查** | **PASS** | 静态代码分析 | go vet ./... 未发现 vet 规则覆盖范围内的问题 | `go vet ./...` (0 告警) |
| **3. 前端生产构建** | **PASS** | 构建产物验证 | Vite 生产构建 4,621 模块转换，CSS 与 JS 哈希生成无报错 | `npm run build` |
| **4. 前端 Smoke 测试** | **PASS** | 静态断言测试 | 核心资产完整性、HTML 引用、IPQA 四态映射规则校验 | `node tests/smoke.mjs` (24 项断言) |
| **5. 前端依赖审计** | **PASS** | 依赖安全扫描 | 前端锁版本依赖漏洞扫描零高危与零中危 | `npm audit && npm audit --omit=dev` (0 漏洞) |
| **6. 生产轮询控制器** | **PASS** | 单元逻辑测试 | `LivePollingController.js` 状态转换、休眠防抖与倒计时逻辑 | `node tests/use_live_polling.test.mjs` (24 项断言) |
| **7. 部署与回滚演练** | **PASS** | 隔离沙箱测试 | 独立沙箱模拟服务异常、探活失败、并发排他锁与正常发布 7 场景 | `bash deploy/test_safe_deploy_rollback.sh` (26.6s) |
| **8. 数据库备份与恢复** | **PASS** | 隔离沙箱测试 | 独立临时库验证备份创建、路径防穿越、损坏拒绝与覆盖恢复，生产库未受改动 | `go test -run TestIsolatedBackupRestore` (8.01s) |
| **9. 线上管理认证会话** | **PASS** | 真实线上测试 | 管理员登录认证成功，页面刷新状态无损保持 | `frontend/tests/browser_real_regression.py` (步骤 1-2) |
| **10. 线上 RBAC 与 CSRF** | **PASS** | 真实线上测试 | 服务端对未鉴权(401)、无效CSRF(403)、只读角色写操作(403)及禁用用户(401/403)严格拦截 | `frontend/tests/browser_real_regression.py` (步骤 3-5) |
| **11. 目标配置生命周期** | **PASS** | 真实线上测试 | 真实网络目标创建、重命名、启用、停用及安全删除全流程通过 | `frontend/tests/browser_real_regression.py` (步骤 6) |
| **12. API Token 全生命周期** | **PASS** | 真实线上测试 | Token 签发、携带认证调用、吊销及吊销后服务端严格 401 拦截通过 | `frontend/tests/browser_real_regression.py` (步骤 8) |
| **13. 审计日志查询** | **PASS** | 真实线上测试 | 真实查询 `/api/audit-logs` 返回合法日志列表与字段结构 | `frontend/tests/browser_real_regression.py` (步骤 10) |
| **14. 节点详情 IPQA 渲染** | **PASS** | 真实线上测试 | 真实节点详情页渲染，挂载真实 IPQA 归档状态卡片 | `frontend/tests/browser_real_regression.py` (步骤 11) |
| **15. 远程终端 Echo 探活** | **PASS** | 真实线上测试 | 线上对真实节点下发无害 `echo ci-terminal-probe-ok` 并正确回显 | `frontend/tests/browser_real_regression.py` (步骤 12) |
| **16. 独有视图选择器等待** | **PASS** | 真实线上测试 | 日志、终端、测速专属视图选择器渲染断言通过 | `frontend/tests/browser_real_regression.py` (步骤 13) |
| **17. 移动端视口适配** | **PASS** | 真实线上测试 | 390px 与 375px 视口无横向滚动，抽屉导航自适应 | `frontend/tests/browser_real_regression.py` (步骤 14) |
| **18. 专用通知通道测试** | **SKIP** | 线上安全规范 | 未配置 `PROBEWATCH_TEST_WEBHOOK_URL` 专用测试端，规范跳过以防骚扰真实用户 | 环境变量未配置 (SKIP 输出) |
| **19. 测速实际打流执行** | **待验证** | 实际业务执行 | 现有自动化测试仅验证了测速任务配置增删管理流程，未触发实际打流执行完成 | 待后续专项验证 |
| **20. 回程监测实际追踪** | **待验证** | 实际业务执行 | 现有自动化测试仅验证了回程监测任务配置增删管理流程，未触发实际路由追踪完成 | 待后续专项验证 |
| **21. 运行时资源短时采样** | **短时采样完成** | 运行时观测 | 6 样本采样平均 RSS 29.99 MB，WAL 4.8 MB，NRestarts 0，Delta 2.85 MB；**不据此宣称长期稳定性通过** | `bash scripts/inspect_runtime.sh` (6 样本, 2s 间隔) |
| **22. 持续运行稳定性 (72h)** | **待验证** | 长期负载观察 | 短时采样不能替代长期运行与高负载压力观察，项目建议以 72 小时为验收观察窗口 | 待长期监控观测 |
| **23. 开源许可证确认流程** | **PENDING** | 协议与法务合规 | 仓库无 `LICENSE` 文件，待所有者选定协议，阻止打上正式 v1.0.0 标签 | 待所有者确认 |

---

## 二、开源许可证确认流程 (License Confirmation Process)

根据正式版准入规范，严禁私自代选协议。开源发布前必须由所有者逐一复核并签署：

- [ ] 项目所有者已确认开源许可证
- [ ] LICENSE 文件已加入仓库
- [ ] README 授权说明与 LICENSE 一致
- [ ] 第三方依赖许可证已检查
- [ ] IPQA 插件和第三方代码授权来源已确认

---

## 三、第三方依赖与授权溯源核查 (Third-Party Dependencies)

依据 `go.mod`、`go.sum`、`frontend/package.json`、`frontend/package-lock.json` 真实清单核实（许可证待项目所有者或法务逐项复核）：

1. **前端依赖 (`frontend/package.json`)**：
   - `react` (^18.3.1)：待逐项核验
   - `react-dom` (^18.3.1)：待逐项核验
   - `@phosphor-icons/react` (^2.1.7)：待逐项核验
   - `vite` (^6.0.5, devDependencies)：待逐项核验
   - `@vitejs/plugin-react` (^4.3.4, devDependencies)：待逐项核验
2. **后端依赖 (`go.mod`)**：
   - `modernc.org/sqlite` (v1.34.5, 直接依赖)：待逐项核验
   - `github.com/google/uuid` (v1.6.0, 间接依赖)：待逐项核验
   - `github.com/dustin/go-humanize` (v1.0.1, 间接依赖)：待逐项核验
   - `github.com/mattn/go-isatty` (v0.0.20, 间接依赖)：待逐项核验
   - `github.com/ncruces/go-strftime` (v0.1.9, 间接依赖)：待逐项核验
   - `github.com/remyoudompheng/bigfft` (v0.0.0-20230129092748-24d4a6f8daec, 间接依赖)：待逐项核验
   - `golang.org/x/sys` (v0.22.0, 间接依赖)：待逐项核验
   - `modernc.org/libc` (v1.55.3, 间接依赖)：待逐项核验
   - `modernc.org/mathutil` (v1.6.0, 间接依赖)：待逐项核验
   - `modernc.org/memory` (v1.8.0, 间接依赖)：待逐项核验
3. **数据与探针整合组件**：
   - **IPQA 插件 / 归档模块**：作为独立外挂与归档导入适配层，仅读取和渲染标准化归档数据，保持外部模块原有许可声明。
