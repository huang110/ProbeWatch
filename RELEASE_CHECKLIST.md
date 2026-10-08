# ProbeWatch 正式版发布准入审查清单 (Release Checklist)

> **当前版本**：`v0.9.3-rc` (Release Candidate)  
> **许可证状态**：**待确认 (License: Pending Confirmation)**  
> **重要说明**：在项目明确选定并签署开源许可证（如 MIT、Apache-2.0 等）前，严禁擅自选择或伪造许可证文件，**不建议亦不应当作为正式开源项目对外发布 v1.0.0 稳定标签**。当前维持候选版 (Candidate) 规范。

---

## 一、准入硬性门槛 (Must-Pass Criteria)

在发布任何更高版本前，以下各项必须 100% 验证通过：

- [ ] **许可证已确认**：项目作者完成最终开源协议选定并添加根目录 `LICENSE`。
- [ ] **Go 全量测试与静态检查**：
  - `go test -count=1 ./...` 全包无报错通过；
  - `go vet ./...` 零告警通过。
- [ ] **前端构建与漏洞审计**：
  - `npm ci` 安装干净锁版本依赖；
  - `npm run build` 构建零报错；
  - `npm run test:smoke` 核心资产完整性断言通过；
  - `npm audit` 与 `npm audit --omit=dev` 零高危及以上漏洞。
- [ ] **生产轮询逻辑一致性**：
  - `node frontend/tests/use_live_polling.test.mjs` 测试直接基于生产控制器 `LivePollingController.js`，全部 24 项断言通过。
- [ ] **部署与自动回滚沙箱测试**：
  - `bash deploy/test_safe_deploy_rollback.sh` 7 个独立场景（服务启动失败、本地健康失败、公网健康失败、回滚失败指引、静态资源复制失败注入、并发部署排他锁、正常发布流程）全部通过。
- [ ] **数据库隔离备份恢复闭环**：
  - `go test -v -run TestIsolatedBackupRestore ./tests` 验证隔离创建、结构校验、损坏拦截、路径防穿越、覆盖前安全快照、恢复后数据一致性与再次读写能力，且生产数据库保持 100% 未修改。
- [ ] **RBAC 角色权限矩阵服务侧拒绝**：
  - `admin`、`viewer`、`disabled`、未鉴权及 CSRF 校验全部在服务端严格生效并返回 401/403，非仅前端隐藏按钮。
- [ ] **真实业务端到端回归**：
  - `python frontend/tests/browser_real_regression.py` 覆盖登录、目标全生命周期、Token 创建与 401 吊销拦截、测速任务增删、审计日志查询、节点详情 IPQA 归档渲染、远程终端 echo 回显、独有视图选择器、390px/375px 移动端无横向滚动，全流程零 `pageerror`，`finally` 严格清理且残留为 0。
- [ ] **通知渠道安全验证**：
  - 未配置专用测试渠道时严格输出 `SKIP`，杜绝打扰真实运维人员；若配置则验证隔离测试通道且在 `finally` 中清理。
- [ ] **五位一体基线版本一致性**：
  - 本地 Git HEAD == GitHub `origin/main` == 服务器仓库 HEAD == `/opt/probewatch/CURRENT_COMMIT` == 运行二进制 SHA256。
  - 公网 `/api/public/version` 返回的版本号与代码中一致。
- [ ] **多样本资源采样稳定性**：
  - 运行 `scripts/inspect_runtime.sh` 证明内存 RSS、VSZ、打开文件描述符及 WAL 大小在多采样周期内平稳，无内存泄漏与进程异常重启。

---

## 二、第三方依赖与授权溯源核查 (Third-Party Licenses)

ProbeWatch 引用的主要第三方开源组件及其授权说明（严禁修改或删除其代码内原有版权声明）：

1. **前端核心组件**：
   - `react` / `react-dom`：MIT License (Meta Platforms, Inc.)
   - `vite`：MIT License (Yuxi Evan You & Vite Contributors)
   - `@phosphor-icons/react`：MIT License (Phosphor Icons)
   - `canvas-confetti`：ISC License
2. **后端核心组件**：
   - `github.com/mattn/go-sqlite3` / modernc sqlite：MIT License
   - `golang.org/x/crypto`：BSD-3-Clause License (The Go Authors)
   - `golang.org/x/net`：BSD-3-Clause License (The Go Authors)
3. **数据与探针整合组件**：
   - **IPQA 插件 / 归档模块**：作为独立外挂与归档导入适配层，仅读取和渲染标准化归档数据，保持外部模块原有许可声明。
