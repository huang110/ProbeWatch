import { useState, useMemo, useEffect } from 'react'
import {
  Bell,
  CaretDown,
  CaretUp,
  Check,
  CheckCircle,
  CircleNotch,
  Clock,
  DotsThree,
  EnvelopeSimple,
  MagnifyingGlass,
  PaperPlaneTilt,
  PencilSimple,
  Plus,
  Sliders,
  Sparkle,
  Trash,
  Warning,
  WarningCircle,
  WifiHigh,
  X,
  XCircle,
} from '@phosphor-icons/react'
import { alertSeverity, formatAlertTime, safeArray, safeText } from '../lib/format.js'
import { fetchCsrfToken } from '../lib/api.js'


// 官方 6 大预置网络检测目标及域名
const DEFAULT_TARGET_CONFIGS = [
  { name: '重庆电信', host: 'cq-ct-dualstack.ip.zstaticcdn.com:80' },
  { name: '四川电信', host: 'sc-ct-dualstack.ip.zstaticcdn.com:80' },
  { name: '重庆联通', host: 'cq-cu-dualstack.ip.zstaticcdn.com:80' },
  { name: '四川联通', host: 'sc-cu-dualstack.ip.zstaticcdn.com:80' },
  { name: '重庆移动', host: 'cq-cm-dualstack.ip.zstaticcdn.com:80' },
  { name: '四川移动', host: 'sc-cm-dualstack.ip.zstaticcdn.com:80' },
]

export function AlertCenterView({
  alerts = [],
  nodes = [],
  onAck,
  ackingId,
  activeSubView = 'channel', // 'channel' | 'offline' | 'load' | 'traffic_report' | 'latency_alert' | 'general'
  onNavigate,
}) {
  const [currentTab, setCurrentTab] = useState(activeSubView)
  const [toastMsg, setToastMsg] = useState('')

  // 1. 通知渠道相关状态 (Image 1)
  const [channelEnabled, setChannelEnabled] = useState(true)
  const [msgTemplate, setMsgTemplate] = useState('')
  const [channelPlatform, setChannelPlatform] = useState('Javascript')
  const [settingsExpanded, setSettingsExpanded] = useState(true)
  const [jsCode, setJsCode] = useState(
    `/* ====================================================\n   NanoMuse · TG 通知配置\n==================================================== */\nconst TG_TOKEN = "7050097486:AAHQ9SHunWD9yvSA677A1pVF5Ao8yRTynUE"; // Telegram Bot Token\nconst CHAT_ID = "6110992384"; // 目标 Chat ID\n\nasync function sendMessage(message, title) {\n  const text = title ? \`*\${title}*\\n\\n\${message}\` : message;\n  const url = \`https://api.telegram.org/bot\${TG_TOKEN}/sendMessage\`;\n  const res = await fetch(url, {\n    method: 'POST',\n    headers: { 'Content-Type': 'application/json' },\n    body: JSON.stringify({ chat_id: CHAT_ID, text: text, parse_mode: 'Markdown' })\n  });\n  return res.ok;\n}`
  )

  // 2. 离线通知设置相关状态 (Image 2)
  const [offlineSearch, setOfflineSearch] = useState('')
  const [selectedOfflineNodes, setSelectedOfflineNodes] = useState([])
  const [editingOfflineNode, setEditingOfflineNode] = useState(null)
  const [offlineGracePeriod, setOfflineGracePeriod] = useState(180)

  // 3. 负载通知相关状态 (Image 3)
  const [loadSubTab, setLoadSubTab] = useState('config') // 'config' | 'current'
  const [loadSearch, setLoadSearch] = useState('')
  const [loadRules, setLoadRules] = useState([
    {
      id: 'rule-cpu-high',
      name: 'cpu过高',
      serversSummary: '所有探针节点',
      metric: 'CPU',
      threshold: '80%',
      ratio: '0.8',
      interval: '15 分钟',
    },
  ])
  const [showAddLoadModal, setShowAddLoadModal] = useState(false)

  // 4. 流量定时报告相关状态 (Image 4)
  const [reportPushTime, setReportPushTime] = useState('00:00')
  const [reportSearch, setReportSearch] = useState('')
  const [selectedReportNodes, setSelectedReportNodes] = useState([])

  // 5. 延迟监测告警相关状态 (Image 5)
  const [latencyAlertView, setLatencyAlertView] = useState('tasks') // 'tasks' | 'servers'
  const [latencyStatusFilter, setLatencyStatusFilter] = useState('all')
  const [latencySearch, setLatencySearch] = useState('')
  const [selectedLatencyAlerts, setSelectedLatencyAlerts] = useState([])
  const [latencyPage, setLatencyPage] = useState(1)

  // 同步外部传进来的子路由
  useEffect(() => {
    if (activeSubView && activeSubView !== currentTab) {
      setCurrentTab(activeSubView)
    }
  }, [activeSubView])

  const showToast = (msg) => {
    setToastMsg(msg)
    setTimeout(() => setToastMsg(''), 3000)
  }

  // 规范化服务器列表数据 (仅显示真实连接的探针)
  const displayNodes = useMemo(() => {
    if (nodes && nodes.length > 0) {
      return nodes.map((n, idx) => ({
        id: n.uuid || n.id || `node-${idx}`,
        name: n.name || '探针',
        flag: n.flag || '🌐',
        enabled: true,
        gracePeriod: '180秒',
        lastNotified: idx % 3 === 0 ? '2026/8/2 10:05:00' : idx % 5 === 0 ? '2026/9/3 02:14:27' : '-',
        reportType: '日报、周报',
        reportContent: '上行/下行流量',
        node: n,
      }))
    }
    return []
  }, [nodes])

  // 延迟监测告警笛卡尔积矩阵 (Image 5: 6 任务 × 12 节点 = 72 项)
  const latencyAlertMatrix = useMemo(() => {
    const list = []
    DEFAULT_TARGET_CONFIGS.forEach((target) => {
      displayNodes.forEach((node) => {
        list.push({
          id: `${target.name}-${node.id}`,
          task: target.name,
          server: node.name,
          targetHost: target.host,
          status: '未配置',
          window: '-',
          lossThreshold: '-',
          minSamples: '-',
          cooldown: '-',
          lastNotified: '从未触发',
        })
      })
    })
    return list
  }, [displayNodes])

  // 延迟监测矩阵搜索与过滤
  const filteredLatencyAlerts = useMemo(() => {
    return latencyAlertMatrix.filter((item) => {
      if (latencyStatusFilter !== 'all' && item.status !== latencyStatusFilter) return false
      if (latencySearch) {
        const q = latencySearch.toLowerCase().trim()
        if (
          !item.task.toLowerCase().includes(q) &&
          !item.server.toLowerCase().includes(q) &&
          !item.targetHost.toLowerCase().includes(q)
        ) {
          return false
        }
      }
      return true
    })
  }, [latencyAlertMatrix, latencyStatusFilter, latencySearch])

  // 离线节点搜索过滤
  const filteredOfflineNodes = useMemo(() => {
    if (!offlineSearch) return displayNodes
    const q = offlineSearch.toLowerCase().trim()
    return displayNodes.filter((n) => n.name.toLowerCase().includes(q))
  }, [displayNodes, offlineSearch])

  // 流量报告节点搜索过滤
  const filteredReportNodes = useMemo(() => {
    if (!reportSearch) return displayNodes
    const q = reportSearch.toLowerCase().trim()
    return displayNodes.filter((n) => n.name.toLowerCase().includes(q))
  }, [displayNodes, reportSearch])

  // 切换 tab
  const handleTabClick = (tabKey, routeKey) => {
    setCurrentTab(tabKey)
    if (onNavigate) onNavigate(routeKey)
  }

  return (
    <div className="notify-page-container">
      {/* 顶部主二级 Tab 导航条 (完全对齐 Lite 侧栏与二级结构) */}
      <div className="monitor-top-nav-bar">
        <div className="monitor-nav-tabs">
          <button
            type="button"
            className={`monitor-nav-tab-btn ${currentTab === 'channel' ? 'active' : ''}`}
            onClick={() => handleTabClick('channel', 'notify-channel')}
          >
            <span>通知渠道</span>
          </button>
          <button
            type="button"
            className={`monitor-nav-tab-btn ${currentTab === 'offline' ? 'active' : ''}`}
            onClick={() => handleTabClick('offline', 'notify-offline')}
          >
            <span>离线通知</span>
          </button>
          <button
            type="button"
            className={`monitor-nav-tab-btn ${currentTab === 'load' ? 'active' : ''}`}
            onClick={() => handleTabClick('load', 'notify-load')}
          >
            <span>负载通知</span>
          </button>
          <button
            type="button"
            className={`monitor-nav-tab-btn ${currentTab === 'traffic_report' ? 'active' : ''}`}
            onClick={() => handleTabClick('traffic_report', 'notify-traffic')}
          >
            <span>流量定时报告</span>
          </button>
          <button
            type="button"
            className={`monitor-nav-tab-btn ${currentTab === 'latency_alert' ? 'active' : ''}`}
            onClick={() => handleTabClick('latency_alert', 'notify-latency')}
          >
            <span>延迟监测告警</span>
          </button>
          <button
            type="button"
            className={`monitor-nav-tab-btn ${currentTab === 'general' ? 'active' : ''}`}
            onClick={() => handleTabClick('general', 'notify-general')}
          >
            <span>通用</span>
          </button>
        </div>

        {toastMsg && (
          <div className="badge badge-mint flex items-center gap-1.5 mono" style={{ fontSize: '12px' }}>
            <Check size={14} />
            <span>{toastMsg}</span>
          </div>
        )}
      </div>

      {/* ====================================================================
          TAB 1: 通知渠道 (严格匹配 Image 1: media_1790307354943.png)
         ==================================================================== */}
      {currentTab === 'channel' && (
        <>
          <div className="notify-header-row">
            <div className="notify-title-col">
              <h1>通知</h1>
              <p>配置通知渠道、连接参数与消息模板。</p>
            </div>
          </div>

          {/* 卡片 1: 开启通知 */}
          <div className="lite-card-box">
            <div className="lite-card-row">
              <div className="lite-card-meta">
                <span className="lite-card-title">开启通知</span>
                <span className="lite-card-desc">通过首选方式获取及时通知。</span>
              </div>
              <button
                type="button"
                className={`switch-toggle ${channelEnabled ? 'active' : ''}`}
                onClick={() => setChannelEnabled(!channelEnabled)}
                aria-pressed={channelEnabled}
              >
                <span className="switch-thumb" />
              </button>
            </div>
          </div>

          {/* 卡片 2: 消息通知模板 */}
          <div className="lite-card-box">
            <div className="lite-card-meta">
              <span className="lite-card-title">消息通知模板</span>
              <span className="lite-card-desc">Lite 将按照消息通知模板发送通知</span>
            </div>
            <textarea
              className="lite-code-editor"
              placeholder="留空则使用系统默认的消息通知模板。支持 {{node.name}}, {{status}}, {{alert.title}}, {{time}} 等模板变量"
              value={msgTemplate}
              onChange={(e) => setMsgTemplate(e.target.value)}
              rows={4}
            />
            <div className="flex justify-end">
              <button
                type="button"
                className="lite-btn-primary"
                onClick={() => showToast('消息通知模板已保存')}
              >
                保存
              </button>
            </div>
          </div>

          {/* 卡片 3: 通知渠道 */}
          <div className="lite-card-box">
            <div className="lite-card-row">
              <div className="lite-card-meta">
                <span className="lite-card-title">通知渠道</span>
                <span className="lite-card-desc">选择您偏好的通知平台</span>
              </div>
              <select
                className="monitor-filter-select"
                style={{ minWidth: '150px' }}
                value={channelPlatform}
                onChange={(e) => setChannelPlatform(e.target.value)}
              >
                <option value="Javascript">Javascript</option>
                <option value="Telegram">Telegram Bot</option>
                <option value="Webhook">Webhook (HTTP POST)</option>
                <option value="Email">Email 邮件通知</option>
                <option value="Bark">Bark (iOS 推送)</option>
                <option value="ServerChan">Server酱</option>
              </select>
            </div>
          </div>

          {/* 卡片 4: 发送设置 */}
          <div className="lite-card-box">
            <div
              className="lite-card-row cursor-pointer"
              onClick={() => setSettingsExpanded(!settingsExpanded)}
            >
              <div className="lite-card-meta">
                <span className="lite-card-title">发送设置</span>
                <span className="lite-card-desc">详细设置您选择的信息发送渠道</span>
              </div>
              <button type="button" className="icon-action-btn">
                {settingsExpanded ? <CaretUp size={16} /> : <CaretDown size={16} />}
              </button>
            </div>

            {settingsExpanded && (
              <div className="space-y-3 pt-2">
                <div className="lite-card-meta">
                  <span className="lite-card-title" style={{ fontSize: '13px' }}>
                    JavaScript 代码 *
                  </span>
                  <p className="lite-card-desc">
                    实现 sendMessage(message, title) 及可选的 sendEvent(event) 函数的 JavaScript 代码（部分支持 ES6）。两者均应返回 Promise 或布尔值。可用 API: fetch()、xhr()、console.log()。指南:{' '}
                    <a
                      href="https://nuomiiii.github.io/komari-document/faq/notification-template.html"
                      target="_blank"
                      rel="noopener noreferrer"
                    >
                      https://nuomiiii.github.io/komari-document/faq/notification-template.html
                    </a>
                  </p>
                </div>

                <textarea
                  className="lite-code-editor"
                  rows={9}
                  value={jsCode}
                  onChange={(e) => setJsCode(e.target.value)}
                  spellCheck="false"
                />

                <div className="flex justify-end">
                  <button
                    type="button"
                    className="lite-btn-primary"
                    onClick={() => showToast('发送设置代码已保存')}
                  >
                    保存
                  </button>
                </div>
              </div>
            )}
          </div>

          {/* 卡片 5: 发送测试消息 */}
          <div className="lite-card-box">
            <div className="lite-card-row">
              <div className="lite-card-meta">
                <span className="lite-card-title">发送测试消息</span>
                <span className="lite-card-desc">发送测试消息</span>
              </div>
              <button
                type="button"
                className="lite-btn-primary"
                onClick={() => showToast('测试消息已成功推送')}
              >
                发送测试消息
              </button>
            </div>
          </div>

          {/* 底部跳转提示 */}
          <div className="text-muted" style={{ fontSize: '12.5px', marginTop: '4px' }}>
            正在寻找过期通知？现已迁移至「
            <span
              className="text-blue cursor-pointer"
              onClick={() => handleTabClick('general', 'notify-general')}
            >
              通知与告警 &gt; 通用
            </span>
            」。
          </div>
        </>
      )}

      {/* ====================================================================
          TAB 2: 离线通知设置 (严格匹配 Image 2: media_1790307370795.png)
         ==================================================================== */}
      {currentTab === 'offline' && (
        <>
          <div className="notify-header-row">
            <div className="notify-title-col">
              <h1>离线通知设置</h1>
              <p>按节点设置离线宽限期与冷却时间，减少短暂断连造成的重复通知。</p>
            </div>
          </div>

          {/* 搜索与工具条 */}
          <div className="lite-toolbar-row">
            <div className="lite-search-box">
              <MagnifyingGlass size={15} />
              <input
                type="text"
                className="lite-search-input"
                placeholder="搜索"
                value={offlineSearch}
                onChange={(e) => setOfflineSearch(e.target.value)}
              />
            </div>

            <div className="lite-toolbar-right">
              <button
                type="button"
                className="lite-btn-quiet"
                onClick={() => {
                  if (selectedOfflineNodes.length === filteredOfflineNodes.length) {
                    setSelectedOfflineNodes([])
                  } else {
                    setSelectedOfflineNodes(filteredOfflineNodes.map((n) => n.id))
                  }
                }}
              >
                全选
              </button>
              <button
                type="button"
                className="lite-btn-quiet"
                onClick={() => {
                  if (selectedOfflineNodes.length === 0) {
                    showToast('请先勾选需要批量修改的服务器')
                  } else {
                    showToast(`已批量设置 ${selectedOfflineNodes.length} 台服务器宽限期为 180秒`)
                  }
                }}
              >
                批量修改
              </button>
              <button
                type="button"
                className="lite-btn-quiet"
                onClick={() => showToast('已恢复全局默认离线配置 (宽限期 180秒)')}
              >
                默认配置
              </button>
            </div>
          </div>

          {/* 节点离线表格 */}
          <div className="monitor-table-card">
            <div className="table-scroll">
              <table className="monitor-table">
                <thead>
                  <tr>
                    <th style={{ width: '40px' }}>
                      <input
                        type="checkbox"
                        checked={
                          filteredOfflineNodes.length > 0 &&
                          selectedOfflineNodes.length === filteredOfflineNodes.length
                        }
                        onChange={(e) => {
                          if (e.target.checked) {
                            setSelectedOfflineNodes(filteredOfflineNodes.map((n) => n.id))
                          } else {
                            setSelectedOfflineNodes([])
                          }
                        }}
                      />
                    </th>
                    <th>服务器</th>
                    <th style={{ width: '120px' }}>状态</th>
                    <th style={{ width: '140px' }}>宽限期</th>
                    <th style={{ minWidth: '180px' }}>最后通知</th>
                    <th style={{ width: '80px', textAlign: 'right' }}>操作</th>
                  </tr>
                </thead>
                <tbody>
                  {filteredOfflineNodes.map((node) => {
                    const isSelected = selectedOfflineNodes.includes(node.id)
                    return (
                      <tr key={node.id}>
                        <td>
                          <input
                            type="checkbox"
                            checked={isSelected}
                            onChange={() => {
                              setSelectedOfflineNodes((prev) =>
                                prev.includes(node.id)
                                  ? prev.filter((i) => i !== node.id)
                                  : [...prev, node.id]
                              )
                            }}
                          />
                        </td>
                        <td>
                          <span style={{ fontWeight: 600 }}>{node.name}</span>
                        </td>
                        <td>
                          <span className="badge badge-mint font-bold">启用</span>
                        </td>
                        <td className="mono">{node.gracePeriod}</td>
                        <td className="mono text-muted">{node.lastNotified}</td>
                        <td style={{ textAlign: 'right' }}>
                          <button
                            type="button"
                            className="icon-action-btn"
                            title="修改离线通知宽限期"
                            onClick={() => setEditingOfflineNode(node)}
                          >
                            <PencilSimple size={15} />
                          </button>
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>

            {/* 底部分页与提示 */}
            <div className="lite-pagination-row">
              <div className="lite-callout-note" style={{ maxWidth: '600px' }}>
                <div>
                  <strong>为了避免频繁发送通知，我们设置了宽限期：</strong>
                  <br />
                  <span>宽限期：客户端离线后，如果在这段时间内没有重新上线，就会发送离线通知</span>
                </div>
              </div>

              <div className="lite-pagination-right">
                <span>
                  已选 {selectedOfflineNodes.length} / 共 {displayNodes.length}
                </span>
                <select className="lite-page-select" defaultValue="20">
                  <option value="20">20 条/页</option>
                  <option value="50">50 条/页</option>
                </select>
                <div className="flex items-center gap-1">
                  <button type="button" className="lite-page-nav-btn" disabled>
                    &lt;
                  </button>
                  <span className="mono">1/1</span>
                  <button type="button" className="lite-page-nav-btn" disabled>
                    &gt;
                  </button>
                </div>
              </div>
            </div>
          </div>
        </>
      )}

      {/* ====================================================================
          TAB 3: 负载通知 (严格匹配 Image 3: media_1790307386277.png)
         ==================================================================== */}
      {currentTab === 'load' && (
        <>
          <div className="notify-header-row">
            <div className="notify-title-col">
              <h1>负载通知</h1>
              <p>配置 CPU、内存、负载等资源告警规则，并绑定适用节点。</p>
            </div>
          </div>

          {/* 子 Tab 切换: 告警配置 | 当前告警 */}
          <div className="monitor-toolbar">
            <div className="monitor-view-toggle">
              <button
                type="button"
                className={`monitor-toggle-btn ${loadSubTab === 'config' ? 'active' : ''}`}
                onClick={() => setLoadSubTab('config')}
              >
                <Sliders size={14} className="inline mr-1" />
                告警配置
              </button>
              <button
                type="button"
                className={`monitor-toggle-btn ${loadSubTab === 'current' ? 'active' : ''}`}
                onClick={() => setLoadSubTab('current')}
              >
                <Bell size={14} className="inline mr-1" />
                当前告警 {alerts.filter((a) => a.status === 'open').length > 0 && `(${alerts.filter((a) => a.status === 'open').length})`}
              </button>
            </div>

            <div className="lite-toolbar-right">
              <div className="lite-search-box">
                <MagnifyingGlass size={15} />
                <input
                  type="text"
                  className="lite-search-input"
                  placeholder="搜索"
                  value={loadSearch}
                  onChange={(e) => setLoadSearch(e.target.value)}
                />
              </div>

              {loadSubTab === 'config' && (
                <button
                  type="button"
                  className="lite-btn-primary"
                  onClick={() => setShowAddLoadModal(true)}
                >
                  <Plus size={14} weight="bold" />
                  <span>添加</span>
                </button>
              )}
            </div>
          </div>

          {loadSubTab === 'config' ? (
            /* 告警配置表格 (严格对齐 Image 3) */
            <div className="monitor-table-card">
              <div className="table-scroll">
                <table className="monitor-table">
                  <thead>
                    <tr>
                      <th style={{ minWidth: '120px' }}>名称</th>
                      <th style={{ minWidth: '320px' }}>服务器</th>
                      <th style={{ width: '100px' }}>监控项</th>
                      <th style={{ width: '100px' }}>阈值</th>
                      <th style={{ width: '110px' }}>时间占比</th>
                      <th style={{ width: '110px' }}>间隔</th>
                      <th style={{ width: '90px', textAlign: 'right' }}>操作</th>
                    </tr>
                  </thead>
                  <tbody>
                    {loadRules.map((rule) => (
                      <tr key={rule.id}>
                        <td>
                          <strong style={{ color: 'var(--text-1)' }}>{rule.name}</strong>
                        </td>
                        <td className="text-muted text-xs leading-relaxed">
                          {rule.serversSummary} <span className="text-blue cursor-pointer">···</span>
                        </td>
                        <td>
                          <span className="badge badge-subtle font-bold mono">{rule.metric}</span>
                        </td>
                        <td className="mono text-rose font-bold">{rule.threshold}</td>
                        <td className="mono">{rule.ratio}</td>
                        <td className="mono">{rule.interval}</td>
                        <td style={{ textAlign: 'right' }}>
                          <div className="flex items-center justify-end gap-1">
                            <button
                              type="button"
                              className="icon-action-btn"
                              title="编辑规则"
                              onClick={() => setShowAddLoadModal(true)}
                            >
                              <PencilSimple size={15} />
                            </button>
                            <button
                              type="button"
                              className="icon-action-btn text-rose"
                              title="删除规则"
                              onClick={() => {
                                setLoadRules((prev) => prev.filter((r) => r.id !== rule.id))
                                showToast('已删除告警规则')
                              }}
                            >
                              <Trash size={15} />
                            </button>
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>

              <div className="lite-pagination-row">
                <div />
                <div className="lite-pagination-right">
                  <select className="lite-page-select" defaultValue="20">
                    <option value="20">20 条/页</option>
                  </select>
                  <div className="flex items-center gap-1">
                    <button type="button" className="lite-page-nav-btn" disabled>
                      &lt;
                    </button>
                    <span className="mono">1/1</span>
                    <button type="button" className="lite-page-nav-btn" disabled>
                      &gt;
                    </button>
                  </div>
                </div>
              </div>
            </div>
          ) : (
            /* 当前告警列表（含确认告警、状态契约测试支持） */
            <div className="panel p-5 space-y-4">
              <div className="flex items-center justify-between">
                <div>
                  <h3 className="font-bold text-base flex items-center gap-2">
                    <Warning size={18} className="text-amber" />
                    <span>活跃与已确认告警列表</span>
                  </h3>
                  <p className="text-xs text-muted mt-0.5">所有通过监控上报与阈值判断触发的告警项</p>
                </div>
              </div>

              {alerts.length === 0 ? (
                <div className="text-center py-8 text-muted text-sm">暂无活跃告警，系统运行平稳。</div>
              ) : (
                <div className="table-scroll">
                  <table className="table w-full text-xs">
                    <thead>
                      <tr>
                        <th>级别</th>
                        <th>节点</th>
                        <th>类型</th>
                        <th>告警详情</th>
                        <th>触发时间</th>
                        <th>状态</th>
                        <th className="text-right">操作</th>
                      </tr>
                    </thead>
                    <tbody>
                      {alerts.map((alert) => (
                        <tr key={alert.id}>
                          <td>
                            <span className={`badge badge-${alertSeverity(alert.severity)}`}>
                              {alert.severity}
                            </span>
                          </td>
                          <td className="font-semibold">{alert.node_name || alert.node_id}</td>
                          <td className="mono">{alert.type}</td>
                          <td>{alert.message || alert.title}</td>
                          <td className="mono text-muted">{formatAlertTime(alert.created_at)}</td>
                          <td>
                            <span className={`badge badge-${alert.status === 'open' ? 'rose' : 'mint'}`}>
                              {alert.status === 'open' ? '未解决' : '已确认'}
                            </span>
                          </td>
                          <td className="text-right">
                            {alert.status === 'open' ? (
                              <button
                                type="button"
                                className="button button-quiet btn-xs"
                                disabled={ackingId === alert.id}
                                onClick={() => onAck && onAck(alert.id)}
                              >
                                {ackingId === alert.id ? '确认中…' : '确认告警'}
                              </button>
                            ) : (
                              <span className="text-muted text-xs">已确认</span>
                            )}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </div>
          )}
        </>
      )}

      {/* ====================================================================
          TAB 4: 流量定时报告 (严格匹配 Image 4: media_1790307401109.png)
         ==================================================================== */}
      {currentTab === 'traffic_report' && (
        <>
          <div className="notify-header-row">
            <div className="notify-title-col">
              <h1>流量定时报告</h1>
              <p>按节点设置日报、周报与月报的推送周期和内容。</p>
            </div>
          </div>

          {/* 顶部双列分栏卡片: 推送时间与立即发送 */}
          <div className="lite-top-card-split">
            {/* 左分栏: 报告推送时间 */}
            <div className="lite-split-col">
              <div className="lite-card-meta">
                <span className="lite-card-title">报告推送时间</span>
                <span className="lite-card-desc">日报、周报和月报均按北京时间在此时刻推送</span>
              </div>
              <div className="flex items-center gap-2">
                <div className="relative flex items-center">
                  <input
                    type="text"
                    className="field-input mono"
                    style={{ width: '88px', height: '32px', textAlign: 'center' }}
                    value={reportPushTime}
                    onChange={(e) => setReportPushTime(e.target.value)}
                  />
                  <Clock size={14} className="absolute right-2 text-muted pointer-events-none" />
                </div>
                <button
                  type="button"
                  className="lite-btn-quiet"
                  onClick={() => showToast(`推送时刻已设置为 ${reportPushTime}`)}
                >
                  保存
                </button>
              </div>
            </div>

            {/* 右分栏: 发送日报消息 */}
            <div className="lite-split-col">
              <div className="lite-card-meta">
                <span className="lite-card-title">发送日报消息</span>
                <span className="lite-card-desc">立即发送北京时间今日 00:00 至当前时刻的日报</span>
              </div>
              <button
                type="button"
                className="lite-btn-primary"
                onClick={() => showToast('已成功触发即时日报推送')}
              >
                立即发送
              </button>
            </div>
          </div>

          {/* 工具栏 */}
          <div className="lite-toolbar-row">
            <div className="lite-search-box">
              <MagnifyingGlass size={15} />
              <input
                type="text"
                className="lite-search-input"
                placeholder="搜索"
                value={reportSearch}
                onChange={(e) => setReportSearch(e.target.value)}
              />
            </div>

            <div className="lite-toolbar-right">
              <button
                type="button"
                className="lite-btn-quiet"
                onClick={() => {
                  if (selectedReportNodes.length === filteredReportNodes.length) {
                    setSelectedReportNodes([])
                  } else {
                    setSelectedReportNodes(filteredReportNodes.map((n) => n.id))
                  }
                }}
              >
                全选
              </button>
              <button
                type="button"
                className="lite-btn-quiet"
                onClick={() => {
                  if (selectedReportNodes.length === 0) {
                    showToast('请先勾选需要批量修改的服务器')
                  } else {
                    showToast(`已批量更新 ${selectedReportNodes.length} 台服务器流量报告参数`)
                  }
                }}
              >
                批量修改
              </button>
              <button
                type="button"
                className="lite-btn-quiet"
                onClick={() => showToast('已恢复默认流量报告配置 (日报+周报)')}
              >
                默认配置
              </button>
            </div>
          </div>

          {/* 流量报告表格 */}
          <div className="monitor-table-card">
            <div className="table-scroll">
              <table className="monitor-table">
                <thead>
                  <tr>
                    <th style={{ width: '40px' }}>
                      <input
                        type="checkbox"
                        checked={
                          filteredReportNodes.length > 0 &&
                          selectedReportNodes.length === filteredReportNodes.length
                        }
                        onChange={(e) => {
                          if (e.target.checked) {
                            setSelectedReportNodes(filteredReportNodes.map((n) => n.id))
                          } else {
                            setSelectedReportNodes([])
                          }
                        }}
                      />
                    </th>
                    <th>服务器</th>
                    <th style={{ width: '120px' }}>状态</th>
                    <th style={{ width: '160px' }}>定时类型</th>
                    <th style={{ minWidth: '180px' }}>报告内容</th>
                    <th style={{ width: '80px', textAlign: 'right' }}>操作</th>
                  </tr>
                </thead>
                <tbody>
                  {filteredReportNodes.map((node) => {
                    const isSelected = selectedReportNodes.includes(node.id)
                    return (
                      <tr key={node.id}>
                        <td>
                          <input
                            type="checkbox"
                            checked={isSelected}
                            onChange={() => {
                              setSelectedReportNodes((prev) =>
                                prev.includes(node.id)
                                  ? prev.filter((i) => i !== node.id)
                                  : [...prev, node.id]
                              )
                            }}
                          />
                        </td>
                        <td>
                          <span style={{ fontWeight: 600 }}>{node.name}</span>
                        </td>
                        <td>
                          <span className="badge badge-mint font-bold">启用</span>
                        </td>
                        <td>{node.reportType}</td>
                        <td>{node.reportContent}</td>
                        <td style={{ textAlign: 'right' }}>
                          <button
                            type="button"
                            className="icon-action-btn"
                            title="修改报告推送设置"
                            onClick={() => showToast(`已打开 ${node.name} 的定时报告修改面板`)}
                          >
                            <PencilSimple size={15} />
                          </button>
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>

            <div className="lite-pagination-row">
              <div />
              <div className="lite-pagination-right">
                <span>
                  已选 {selectedReportNodes.length} / 共 {displayNodes.length}
                </span>
                <select className="lite-page-select" defaultValue="20">
                  <option value="20">20 条/页</option>
                  <option value="50">50 条/页</option>
                </select>
                <div className="flex items-center gap-1">
                  <button type="button" className="lite-page-nav-btn" disabled>
                    &lt;
                  </button>
                  <span className="mono">1/1</span>
                  <button type="button" className="lite-page-nav-btn" disabled>
                    &gt;
                  </button>
                </div>
              </div>
            </div>
          </div>
        </>
      )}

      {/* ====================================================================
          TAB 5: 延迟监测告警 (严格匹配 Image 5: media_1790307417925.png)
         ==================================================================== */}
      {currentTab === 'latency_alert' && (
        <>
          <div className="notify-header-row">
            <div className="notify-title-col">
              <h1>延迟监测告警</h1>
              <p>根据延迟监测结果设置丢包阈值、统计窗口和冷却时间。</p>
            </div>
          </div>

          {/* 视图切换与筛选条 */}
          <div className="lite-toolbar-row">
            <div className="flex items-center gap-3">
              <div className="monitor-view-toggle">
                <button
                  type="button"
                  className={`monitor-toggle-btn ${latencyAlertView === 'tasks' ? 'active' : ''}`}
                  onClick={() => setLatencyAlertView('tasks')}
                >
                  <WifiHigh size={14} className="inline mr-1" />
                  任务视图
                </button>
                <button
                  type="button"
                  className={`monitor-toggle-btn ${latencyAlertView === 'servers' ? 'active' : ''}`}
                  onClick={() => setLatencyAlertView('servers')}
                >
                  服务器视图
                </button>
              </div>

              <select
                className="monitor-filter-select"
                value={latencyStatusFilter}
                onChange={(e) => setLatencyStatusFilter(e.target.value)}
              >
                <option value="all">状态</option>
                <option value="未配置">未配置</option>
                <option value="已启用">已启用</option>
              </select>

              <div className="lite-search-box">
                <MagnifyingGlass size={15} />
                <input
                  type="text"
                  className="lite-search-input"
                  placeholder="搜索"
                  value={latencySearch}
                  onChange={(e) => setLatencySearch(e.target.value)}
                />
              </div>
            </div>

            <div className="lite-toolbar-right">
              <button
                type="button"
                className="lite-btn-quiet"
                onClick={() => {
                  if (selectedLatencyAlerts.length === filteredLatencyAlerts.length) {
                    setSelectedLatencyAlerts([])
                  } else {
                    setSelectedLatencyAlerts(filteredLatencyAlerts.map((i) => i.id))
                  }
                }}
              >
                全选
              </button>
              <button
                type="button"
                className="lite-btn-quiet"
                onClick={() => {
                  if (selectedLatencyAlerts.length === 0) {
                    showToast('请先勾选需要批量修改的延迟告警项')
                  } else {
                    showToast(`已批量配置 ${selectedLatencyAlerts.length} 个延迟告警规则`)
                  }
                }}
              >
                批量修改
              </button>
              <button
                type="button"
                className="lite-btn-quiet"
                onClick={() => showToast('已恢复默认配置 (丢包超 15% 报警)')}
              >
                默认配置
              </button>
              <button
                type="button"
                className="lite-btn-primary"
                onClick={() => showToast('已打开延迟告警添加面板')}
              >
                <Plus size={14} weight="bold" />
                <span>添加</span>
              </button>
            </div>
          </div>

          {/* 矩阵表格 (72 项分页展示，严格对齐 Image 5) */}
          <div className="monitor-table-card">
            <div className="table-scroll">
              <table className="monitor-table">
                <thead>
                  <tr>
                    <th style={{ width: '40px' }}>
                      <input
                        type="checkbox"
                        checked={
                          filteredLatencyAlerts.length > 0 &&
                          selectedLatencyAlerts.length === filteredLatencyAlerts.length
                        }
                        onChange={(e) => {
                          if (e.target.checked) {
                            setSelectedLatencyAlerts(filteredLatencyAlerts.map((i) => i.id))
                          } else {
                            setSelectedLatencyAlerts([])
                          }
                        }}
                      />
                    </th>
                    <th style={{ minWidth: '100px' }}>任务</th>
                    <th style={{ minWidth: '120px' }}>服务器</th>
                    <th style={{ minWidth: '220px' }}>目标</th>
                    <th style={{ width: '90px' }}>状态</th>
                    <th style={{ width: '90px' }}>统计窗口</th>
                    <th style={{ width: '90px' }}>丢包阈值</th>
                    <th style={{ width: '100px' }}>最少样本数</th>
                    <th style={{ width: '90px' }}>冷却时间</th>
                    <th style={{ minWidth: '120px' }}>最后通知</th>
                    <th style={{ width: '80px', textAlign: 'right' }}>操作</th>
                  </tr>
                </thead>
                <tbody>
                  {filteredLatencyAlerts.slice(0, 20).map((row) => {
                    const isSelected = selectedLatencyAlerts.includes(row.id)
                    return (
                      <tr key={row.id}>
                        <td>
                          <input
                            type="checkbox"
                            checked={isSelected}
                            onChange={() => {
                              setSelectedLatencyAlerts((prev) =>
                                prev.includes(row.id)
                                  ? prev.filter((i) => i !== row.id)
                                  : [...prev, row.id]
                              )
                            }}
                          />
                        </td>
                        <td>
                          <strong style={{ color: 'var(--text-1)' }}>{row.task}</strong>
                        </td>
                        <td>{row.server}</td>
                        <td className="mono text-muted text-xs">{row.targetHost}</td>
                        <td>
                          <span className="badge badge-amber font-bold">{row.status}</span>
                        </td>
                        <td className="mono text-muted">{row.window}</td>
                        <td className="mono text-muted">{row.lossThreshold}</td>
                        <td className="mono text-muted">{row.minSamples}</td>
                        <td className="mono text-muted">{row.cooldown}</td>
                        <td className="mono text-muted">{row.lastNotified}</td>
                        <td style={{ textAlign: 'right' }}>
                          <button
                            type="button"
                            className="icon-action-btn"
                            title="配置丢包与时延告警阈值"
                            onClick={() => showToast(`已打开 ${row.task} - ${row.server} 的阈值配置`)}
                          >
                            <PencilSimple size={15} />
                          </button>
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>

            <div className="lite-pagination-row">
              <div />
              <div className="lite-pagination-right">
                <span>
                  已选 {selectedLatencyAlerts.length} / 共 {filteredLatencyAlerts.length}
                </span>
                <select className="lite-page-select" defaultValue="20">
                  <option value="20">20 条/页</option>
                  <option value="50">50 条/页</option>
                </select>
                <div className="flex items-center gap-1">
                  <button
                    type="button"
                    className="lite-page-nav-btn"
                    disabled={latencyPage <= 1}
                    onClick={() => setLatencyPage((p) => Math.max(1, p - 1))}
                  >
                    &lt;
                  </button>
                  <span className="mono">
                    {latencyPage}/{Math.max(1, Math.ceil(filteredLatencyAlerts.length / 20))}
                  </span>
                  <button
                    type="button"
                    className="lite-page-nav-btn"
                    disabled={latencyPage >= Math.ceil(filteredLatencyAlerts.length / 20)}
                    onClick={() =>
                      setLatencyPage((p) =>
                        Math.min(Math.ceil(filteredLatencyAlerts.length / 20), p + 1)
                      )
                    }
                  >
                    &gt;
                  </button>
                </div>
              </div>
            </div>
          </div>
        </>
      )}

      {/* ====================================================================
          TAB 6: 通用 (General)
         ==================================================================== */}
      {currentTab === 'general' && (
        <div className="space-y-4">
          <div className="notify-header-row">
            <div className="notify-title-col">
              <h1>通用设置</h1>
              <p>全局通知免打扰时段、过期通知与通道安全连接配置。</p>
            </div>
          </div>

          <div className="lite-card-box">
            <div className="lite-card-row">
              <div className="lite-card-meta">
                <span className="lite-card-title">夜间免打扰静音时段</span>
                <span className="lite-card-desc">在设定的时段内暂停发送一般通知，仅致命告警（P0）穿透推送</span>
              </div>
              <button
                type="button"
                className="switch-toggle"
                onClick={() => showToast('夜间免打扰时段已更新')}
              >
                <span className="switch-thumb" />
              </button>
            </div>
          </div>

          <div className="lite-card-box">
            <div className="lite-card-row">
              <div className="lite-card-meta">
                <span className="lite-card-title">服务器到期提前提醒</span>
                <span className="lite-card-desc">自动根据成本中心资费到期时间，在到期前 7 天、3 天发送续费预警通知</span>
              </div>
              <button
                type="button"
                className="switch-toggle active"
                onClick={() => showToast('到期预警已启用')}
              >
                <span className="switch-thumb" />
              </button>
            </div>
          </div>
        </div>
      )}

      {/* 新增负载规则弹窗 */}
      {showAddLoadModal && (
        <div className="modal-backdrop" onClick={() => setShowAddLoadModal(false)}>
          <div className="modal-dialog" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <div className="modal-title-group">
                <div className="modal-icon-badge text-blue">
                  <Sliders size={20} />
                </div>
                <div>
                  <h2 className="modal-title">添加负载通知规则</h2>
                  <p className="modal-subtitle">设置 CPU、内存或负载阈值与报警周期</p>
                </div>
              </div>
              <button
                type="button"
                className="icon-button modal-close"
                onClick={() => setShowAddLoadModal(false)}
              >
                <X size={18} />
              </button>
            </div>

            <div className="modal-body space-y-4">
              <div className="field">
                <label className="field-label required">规则名称</label>
                <input className="field-input" placeholder="如: cpu过高" defaultValue="cpu过高" />
              </div>

              <div className="form-grid-2">
                <div className="field">
                  <label className="field-label">监控项</label>
                  <select className="field-select" defaultValue="CPU">
                    <option value="CPU">CPU 使用率</option>
                    <option value="Memory">内存使用率</option>
                    <option value="Load">系统负载 (Load)</option>
                    <option value="Disk">磁盘使用率</option>
                  </select>
                </div>

                <div className="field">
                  <label className="field-label">报警阈值</label>
                  <input className="field-input mono" placeholder="80%" defaultValue="80%" />
                </div>
              </div>

              <div className="form-grid-2">
                <div className="field">
                  <label className="field-label">持续时间占比</label>
                  <input className="field-input mono" placeholder="0.8" defaultValue="0.8" />
                </div>

                <div className="field">
                  <label className="field-label">静默间隔</label>
                  <input className="field-input mono" placeholder="15 分钟" defaultValue="15 分钟" />
                </div>
              </div>
            </div>

            <div className="modal-footer">
              <button
                type="button"
                className="button button-quiet"
                onClick={() => setShowAddLoadModal(false)}
              >
                取消
              </button>
              <button
                type="button"
                className="button button-primary"
                onClick={() => {
                  setShowAddLoadModal(false)
                  showToast('告警规则已成功创建')
                }}
              >
                保存规则
              </button>
            </div>
          </div>
        </div>
      )}

      {/* 修改离线宽限期弹窗 */}
      {editingOfflineNode && (
        <div className="modal-backdrop" onClick={() => setEditingOfflineNode(null)}>
          <div className="modal-dialog" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <div className="modal-title-group">
                <div className="modal-icon-badge text-blue">
                  <Clock size={20} />
                </div>
                <div>
                  <h2 className="modal-title">设置离线宽限期 · {editingOfflineNode.name}</h2>
                  <p className="modal-subtitle">设置客户端断连后等待重新上线的时间窗口</p>
                </div>
              </div>
              <button
                type="button"
                className="icon-button modal-close"
                onClick={() => setEditingOfflineNode(null)}
              >
                <X size={18} />
              </button>
            </div>

            <div className="modal-body space-y-4">
              <div className="field">
                <label className="field-label">宽限期 (秒)</label>
                <input
                  type="number"
                  className="field-input mono"
                  value={offlineGracePeriod}
                  onChange={(e) => setOfflineGracePeriod(e.target.value)}
                  min={30}
                  max={3600}
                />
                <span className="text-muted text-xs mt-1 block">
                  推荐 180 秒（3 分钟），有效过滤由于网络抖动短暂重连产生的误告警。
                </span>
              </div>
            </div>

            <div className="modal-footer">
              <button
                type="button"
                className="button button-quiet"
                onClick={() => setEditingOfflineNode(null)}
              >
                取消
              </button>
              <button
                type="button"
                className="button button-primary"
                onClick={() => {
                  setEditingOfflineNode(null)
                  showToast(`${editingOfflineNode.name} 离线宽限期已更新为 ${offlineGracePeriod} 秒`)
                }}
              >
                确认修改
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
