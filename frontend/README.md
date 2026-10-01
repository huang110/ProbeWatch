# ProbeWatch 前端

ProbeWatch 的简体中文实时运维监控控制台。页面通过同源 Go 主控 API 获取真实节点、探测、告警、日志和终端状态，并使用统一实时刷新机制保持数据更新。

## 运行

```bash
npm install
npm run dev
```

## 浏览器回归测试

构建后启动预览服务，再使用已安装的 Chromium 运行当前页面结构回归：

```powershell
npm run build
npm run preview -- --host 127.0.0.1 --port 4173
$env:PROBEWATCH_CHROMIUM="C:\Users\黄\AppData\Local\Chromium\Application\chrome.exe"
$env:PROBEWATCH_BASE_URL="http://127.0.0.1:4173/"
npm run test:browser:live
```

## 当前页面

- 中文监控总览
- 节点搜索与状态筛选
- 节点详情抽屉
- MTR 路由证据
- 网络与流媒体检测摘要
- 告警事件列表
- 实时日志流、审计日志与受控 Web 终端
- 测速、合成探测、网络监测、证书 DNS、容器进程和全球互联矩阵
- 响应式移动端布局
- 加载、空结果、离线和告警状态
- 页面隐藏时暂停请求，回到前台或网络恢复后立即刷新
- 接口异常时保留最近有效数据并进行退避重试
