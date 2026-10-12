# 许可证声明 (License Status: Pending Confirmation)

> [!IMPORTANT]
> **开源许可证尚未由项目所有者最终选定与签署。**
> 
> 当前版本处于 **Release Candidate (`v0.9.3-rc`)** 状态。在项目所有者明确选定并签署开源许可证（例如 MIT、Apache 2.0、AGPL 等）前，本项目**严禁**任何维护者或自动化工具擅自预设、伪造或变更开源许可协议。

---

## 一、授权意图与发布准入规范

1. **项目所有者主权**：开源许可证的选择权唯一归属于项目所有者。
2. **发布准入阻塞 (Release Blocker)**：
   - 许可证确认是发布 `v1.0.0` 正式版本的**硬性前置阻塞条件**；
   - 未在根目录添加由所有者正式确认的 `LICENSE` 文本前，禁止发布 `v1.0.0` 稳定标签；
   - 当前版本标签严格保持为 `v0.9.3-rc`。
3. **当前使用范围**：
   - 仅限个人自建部署、技术评估、安全审计与代码审查；
   - 不得用于未经所有者许可的二次商业分发或闭源再授权。

---

## 二、第三方组件许可证说明

本项目所引用的第三方开源组件均严格遵循其原作者的开源许可证协议：

### 1. 前端组件与库
- **React / React DOM** (`^18.3.1`): MIT License (Meta Platforms, Inc.)
- **Vite** (`^6.0.5`): MIT License (Yuxi Evan You & Vite Contributors)
- **@phosphor-icons/react** (`^2.1.7`): MIT License (Phosphor Icons)
- **canvas-confetti** (`^1.9.4`): ISC License (Kiril Vatev)

### 2. 后端核心依赖
- **modernc.org/sqlite** (`v1.34.5`): BSD-3-Clause License
- **golang.org/x/sys**, **golang.org/x/crypto**, **golang.org/x/net**: BSD-3-Clause License (The Go Authors)
- **github.com/google/uuid** (`v1.6.0`): BSD-3-Clause License
- **github.com/dustin/go-humanize** (`v1.0.1`): MIT License

### 3. 数据与探针适配层
- **IPQA 插件 / 归档适配模块**：作为独立外挂与归档导入适配层，仅读取并呈现标准化归档数据，保持原插件/模块各自的代码许可与归属。
