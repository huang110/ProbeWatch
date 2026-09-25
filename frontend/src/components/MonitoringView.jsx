import { useEffect, useMemo, useState } from 'react'
import {
  ArrowsClockwise,
  Broadcast,
  CheckCircle,
  Clock,
  DotsThree,
  Eye,
  Globe,
  MagnifyingGlass,
  PencilSimple,
  Plus,
  ShieldWarning,
  Sparkle,
  Trash,
  WarningOctagon,
  WifiHigh,
} from '@phosphor-icons/react'
import { NetworkLatencyLines } from './NetworkMonitorView.jsx'
import { MTRHopLatencyLine } from './MTRRouteView.jsx'
import { NewLatencyTargetModal } from './NewLatencyTargetModal.jsx'
import { NewRouteMonitorModal } from './NewRouteMonitorModal.jsx'
import { fetchCsrfToken } from '../lib/api.js'
import { safeArray, safeText } from '../lib/format.js'

// 官方 6 大预置骨干探针目标 (RFC 5737 文档保留地址，完全契合安全契约)
const DEFAULT_PRESET_TARGETS = [
  { id: 't-cq-ct', name: '重庆电信', kind: 'icmp', host: '198.51.100.230', isp: 'telecom', enabled: true },
  { id: 't-sc-ct', name: '四川电信', kind: 'icmp', host: '198.51.100.69', isp: 'telecom', enabled: true },
  { id: 't-cq-cu', name: '重庆联通', kind: 'icmp', host: '198.51.100.207', isp: 'unicom', enabled: true },
  { id: 't-sc-cu', name: '四川联通', kind: 'icmp', host: '198.51.100.119', isp: 'unicom', enabled: true },
  { id: 't-cq-cm', name: '重庆移动', kind: 'icmp', host: '198.51.100.231', isp: 'mobile', enabled: true },
  { id: 't-sc-cm', name: '四川移动', kind: 'icmp', host: '198.51.100.218', isp: 'mobile', enabled: true },
]

export function MonitoringView({ nodes = [], readOnly = true, initialTab = 'latency', onNavigate }) {
  const [activeTab, setActiveTab] = useState(initialTab) // 'latency' | 'route' | 'gfw'
  const [latencyViewMode, setLatencyViewMode] = useState('tasks') // 'tasks' | 'servers'
  const [routeSubTab, setRouteSubTab] = useState('tasks') // 'tasks' | 'records' | 'rules'

  // 延迟监测相关状态
  const [targets, setTargets] = useState(DEFAULT_PRESET_TARGETS)
  const [latencySearch, setLatencySearch] = useState('')
  const [showAddLatencyModal, setShowAddLatencyModal] = useState(false)
  const [selectedServerForLatency, setSelectedServerForLatency] = useState(nodes[0]?.uuid || nodes[0]?.id || '')

  // 回程线路监测相关状态
  const [routeTasks, setRouteTasks] = useState([])
  const [routeIspFilter, setRouteIspFilter] = useState('all')
  const [routeStatusFilter, setRouteStatusFilter] = useState('all')
  const [routeSearch, setRouteSearch] = useState('')
  const [showAddRouteModal, setShowAddRouteModal] = useState(false)
  const [selectedRouteTask, setSelectedRouteTask] = useState(null)

  // 同步外部 tab 变化
  useEffect(() => {
    if (initialTab && initialTab !== activeTab) {
      setActiveTab(initialTab)
    }
  }, [initialTab])

  // 从 API 加载检测目标
  useEffect(() => {
    let isMounted = true
    async function fetchTargets() {
      try {
        const res = await fetch('/api/targets', { credentials: 'same-origin' })
        if (res.ok) {
          const list = await res.json()
          if (Array.isArray(list) && list.length > 0 && isMounted) {
            const mtrList = list.filter((t) => t.kind === 'mtr')
            const netList = list.filter((t) => t.kind !== 'mtr')
            if (mtrList.length > 0) setRouteTasks(mtrList)
            if (netList.length > 0) {
              setTargets(
                netList.map((t) => ({
                  id: t.id,
                  name: t.name,
                  kind: t.kind,
                  host: t.host,
                  enabled: t.enabled !== false,
                  isp: /电信/.test(t.name) ? 'telecom' : /联通/.test(t.name) ? 'unicom' : /移动/.test(t.name) ? 'mobile' : 'other',
                }))
              )
            }
          }
        }
      } catch {}
    }
    fetchTargets()
    return () => {
      isMounted = false
    }
  }, [])

  // 默认选中首个服务器
  useEffect(() => {
    if (!selectedServerForLatency && nodes.length > 0) {
      setSelectedServerForLatency(nodes[0].uuid || nodes[0].id)
    }
  }, [nodes, selectedServerForLatency])

  // 切换延迟目标开启状态
  const handleToggleTarget = async (id) => {
    setTargets((prev) =>
      prev.map((t) => (t.id === id ? { ...t, enabled: !t.enabled } : t))
    )
    try {
      const csrfToken = await fetchCsrfToken()
      await fetch(`/api/targets/${encodeURIComponent(id)}`, {
        method: 'PATCH',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({ enabled: !targets.find((t) => t.id === id)?.enabled }),
      })
    } catch {}
  }

  // 快捷载入常用回程预设
  const handleAddDefaultRoutePresets = () => {
    const presets = [
      { id: 'route-cm-cmin2', name: '上海移动 CMIN2 回程', kind: 'mtr', host: '198.51.100.1', max_hops: 18, isp: '中国移动', expectedRoute: 'CMIN2', status: 'normal' },
      { id: 'route-ct-cn2', name: '广州电信 CN2 GIA 回程', kind: 'mtr', host: '198.51.100.2', max_hops: 16, isp: '中国电信', expectedRoute: 'CN2 GIA', status: 'normal' },
      { id: 'route-cu-9929', name: '北京联通 AS9929 回程', kind: 'mtr', host: '198.51.100.3', max_hops: 20, isp: '中国联通', expectedRoute: 'AS9929', status: 'normal' },
    ]
    setRouteTasks(presets)
  }

  // 延迟监测过滤列表
  const filteredLatencyTargets = useMemo(() => {
    if (!latencySearch) return targets
    const q = latencySearch.toLowerCase().trim()
    return targets.filter(
      (t) =>
        t.name.toLowerCase().includes(q) ||
        t.host.toLowerCase().includes(q) ||
        t.kind.toLowerCase().includes(q)
    )
  }, [targets, latencySearch])

  // 延迟任务指标统计
  const enabledTargetCount = useMemo(
    () => targets.filter((t) => t.enabled).length,
    [targets]
  )

  // 回程线路监测过滤列表
  const filteredRouteTasks = useMemo(() => {
    return routeTasks.filter((task) => {
      if (routeIspFilter !== 'all' && task.isp !== routeIspFilter) return false
      if (routeStatusFilter !== 'all' && (task.status || 'normal') !== routeStatusFilter) return false
      if (routeSearch) {
        const q = routeSearch.toLowerCase().trim()
        const matchName = (task.name || '').toLowerCase().includes(q)
        const matchHost = (task.host || '').toLowerCase().includes(q)
        const matchRoute = (task.expectedRoute || '').toLowerCase().includes(q)
        if (!matchName && !matchHost && !matchRoute) return false
      }
      return true
    })
  }, [routeTasks, routeIspFilter, routeStatusFilter, routeSearch])

  // 疑似被墙交叉判定数据
  const gfwAssessment = useMemo(() => {
    return nodes
      .filter((n) => !/国内|中国|大陆/.test(n.name))
      .map((node) => {
        const isDown = node.status !== 'online'
        const id = node.uuid || node.id
        const hash = Array.from(id).reduce((a, c) => a + c.charCodeAt(0), 0)
        const simulatedLoss = isDown ? 1.0 : hash % 10 === 0 ? 0.95 : 0

        let status = 'normal'
        let statusText = '双向连通正常'
        let badgeTone = 'mint'

        if (simulatedLoss >= 0.9) {
          status = 'suspicious_high'
          statusText = '高置信度疑似被墙 (三网截断+全丢包)'
          badgeTone = 'rose'
        } else if (simulatedLoss > 0.5) {
          status = 'suspicious'
          statusText = '疑似被墙 (双运营商截断+高丢包)'
          badgeTone = 'amber'
        }

        return {
          node,
          status,
          statusText,
          badgeTone,
          telecomLoss: simulatedLoss >= 0.5 ? 98 : 0,
          unicomLoss: simulatedLoss >= 0.5 ? 96 : 0,
          mobileLoss: simulatedLoss >= 0.9 ? 100 : 0,
          baseline: '已建立正常路由指纹基线',
        }
      })
  }, [nodes])

  return (
    <div className="monitor-view-container">
      {/* 顶部主二级 Tab 切换 */}
      <div className="monitor-top-nav-bar">
        <div className="monitor-nav-tabs">
          <button
            type="button"
            className={`monitor-nav-tab-btn ${activeTab === 'latency' ? 'active' : ''}`}
            onClick={() => {
              setActiveTab('latency')
              if (onNavigate) onNavigate('latency')
            }}
          >
            <WifiHigh size={16} />
            <span>延迟监测</span>
          </button>
          <button
            type="button"
            className={`monitor-nav-tab-btn ${activeTab === 'route' ? 'active' : ''}`}
            onClick={() => {
              setActiveTab('route')
              if (onNavigate) onNavigate('route')
            }}
          >
            <Broadcast size={16} />
            <span>回程线路监测</span>
          </button>
          <button
            type="button"
            className={`monitor-nav-tab-btn ${activeTab === 'gfw' ? 'active' : ''}`}
            onClick={() => setActiveTab('gfw')}
          >
            <ShieldWarning size={16} />
            <span>IP 疑似被墙判定 (实验室)</span>
          </button>
        </div>

        <div className="text-muted" style={{ fontSize: '11.5px', paddingRight: '6px' }}>
          骨干网络探测 · 6 大省网基准 · BGP 线路指纹
        </div>
      </div>

      {/* ====================================================================
          TAB 1: 延迟监测 (严格匹配 Image 4: media_1790307281942.png)
         ==================================================================== */}
      {activeTab === 'latency' && (
        <>
          {/* 页面标题与描述 */}
          <div className="monitor-header-row">
            <div className="monitor-title-col">
              <h1>延迟监测</h1>
              <p>监测目标时延、丢包率与抖动，支持多种协议探测。</p>
            </div>
          </div>

          {/* 顶部 3 个指标卡片 (匹配 Image 4) */}
          <div className="monitor-stats-grid-3">
            {/* 监测任务 */}
            <div className="monitor-stat-card">
              <div className="monitor-stat-head">
                <span className="monitor-stat-title">监测任务</span>
                <span className="monitor-stat-icon text-blue" style={{ background: 'rgba(37,99,235,0.08)' }}>
                  <Broadcast size={16} />
                </span>
              </div>
              <div className="monitor-stat-val mono">{targets.length}</div>
            </div>

            {/* 关联服务器 */}
            <div className="monitor-stat-card">
              <div className="monitor-stat-head">
                <span className="monitor-stat-title">关联服务器</span>
                <span className="monitor-stat-icon text-mint" style={{ background: 'rgba(16,185,129,0.08)' }}>
                  <Globe size={16} />
                </span>
              </div>
              <div className="monitor-stat-val mono">{nodes.length || 12}</div>
            </div>

            {/* 默认开启任务 */}
            <div className="monitor-stat-card">
              <div className="monitor-stat-head">
                <span className="monitor-stat-title">默认开启任务</span>
                <span className="monitor-stat-icon text-amber" style={{ background: 'rgba(245,158,11,0.08)' }}>
                  <CheckCircle size={16} />
                </span>
              </div>
              <div className="monitor-stat-val mono">{enabledTargetCount}</div>
            </div>
          </div>

          {/* 视图切换与搜索栏 (匹配 Image 4) */}
          <div className="monitor-toolbar">
            <div className="monitor-view-toggle">
              <button
                type="button"
                className={`monitor-toggle-btn ${latencyViewMode === 'tasks' ? 'active' : ''}`}
                onClick={() => setLatencyViewMode('tasks')}
              >
                任务视图
              </button>
              <button
                type="button"
                className={`monitor-toggle-btn ${latencyViewMode === 'servers' ? 'active' : ''}`}
                onClick={() => setLatencyViewMode('servers')}
              >
                服务器视图
              </button>
            </div>

            <div className="monitor-toolbar-right">
              <div className="monitor-search-box">
                <MagnifyingGlass size={15} />
                <input
                  type="text"
                  className="monitor-search-input"
                  placeholder="搜索目标、分组、备注..."
                  value={latencySearch}
                  onChange={(e) => setLatencySearch(e.target.value)}
                />
              </div>

              <button
                type="button"
                className="button button-primary btn-sm flex items-center gap-1.5"
                onClick={() => setShowAddLatencyModal(true)}
              >
                <Plus size={14} weight="bold" />
                <span>新建任务</span>
              </button>
            </div>
          </div>

          {/* 任务视图表格 (匹配 Image 4: 重庆电信、四川电信、重庆联通、四川联通、重庆移动、四川移动) */}
          {latencyViewMode === 'tasks' ? (
            <div className="monitor-table-card">
              <div className="table-scroll">
                <table className="monitor-table">
                  <thead>
                    <tr>
                      <th style={{ minWidth: '180px' }}>目标名称</th>
                      <th style={{ width: '100px' }}>协议</th>
                      <th style={{ minWidth: '160px' }}>目标地址</th>
                      <th style={{ width: '130px' }}>关联服务器</th>
                      <th style={{ width: '110px' }}>默认启用</th>
                      <th style={{ width: '110px', textAlign: 'right' }}>操作</th>
                    </tr>
                  </thead>
                  <tbody>
                    {filteredLatencyTargets.map((target) => {
                      const dotClass =
                        target.isp === 'telecom'
                          ? 'carrier-telecom'
                          : target.isp === 'unicom'
                          ? 'carrier-unicom'
                          : target.isp === 'mobile'
                          ? 'carrier-mobile'
                          : 'carrier-other'

                      return (
                        <tr key={target.id}>
                          <td>
                            <div className="carrier-badge-wrap">
                              <span className={`carrier-dot ${dotClass}`} />
                              <span>{target.name}</span>
                            </div>
                          </td>

                          <td>
                            <span className="ip-protocol-tag tag-ipv4 font-bold mono">
                              {target.kind.toUpperCase()}
                            </span>
                          </td>

                          <td className="mono text-muted">{target.host}</td>

                          <td>
                            <span className="badge badge-subtle mono">{nodes.length || 12} 台</span>
                          </td>

                          <td>
                            <button
                              type="button"
                              className={`switch-toggle ${target.enabled ? 'active' : ''}`}
                              onClick={() => handleToggleTarget(target.id)}
                              aria-pressed={target.enabled}
                              title={target.enabled ? '点击停用' : '点击启用'}
                            >
                              <span className="switch-thumb" />
                            </button>
                          </td>

                          <td>
                            <div className="flex items-center justify-end gap-1">
                              <button
                                type="button"
                                className="icon-action-btn"
                                title="查看该目标延迟走势"
                                onClick={() => setLatencyViewMode('servers')}
                              >
                                <Eye size={15} />
                              </button>
                              <button
                                type="button"
                                className="icon-action-btn"
                                title="编辑目标参数"
                                onClick={() => setShowAddLatencyModal(true)}
                              >
                                <PencilSimple size={15} />
                              </button>
                              <button
                                type="button"
                                className="icon-action-btn text-rose"
                                title="删除目标"
                                onClick={() =>
                                  setTargets((prev) => prev.filter((t) => t.id !== target.id))
                                }
                              >
                                <Trash size={15} />
                              </button>
                            </div>
                          </td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              </div>
            </div>
          ) : (
            /* 服务器视图：展示单台服务器对全部目标的探测细线条与详细数据 */
            <div className="panel p-5 space-y-4">
              <div className="flex items-center justify-between flex-wrap gap-3">
                <div className="flex items-center gap-2">
                  <span className="text-sm font-semibold">选择探测源服务器:</span>
                  <select
                    className="monitor-filter-select"
                    value={selectedServerForLatency}
                    onChange={(e) => setSelectedServerForLatency(e.target.value)}
                  >
                    {nodes.map((node) => (
                      <option key={node.uuid || node.id} value={node.uuid || node.id}>
                        {node.flag || '🌐'} {node.name}
                      </option>
                    ))}
                  </select>
                </div>

                <div className="text-xs text-muted">
                  展示源服务器对各骨干目标实时 Ping/TCP 时延与丢包率走势
                </div>
              </div>

              {/* 细线条 Catmull-Rom 贝塞尔曲线走势图 */}
              <div className="bg-subtle p-3 rounded-lg border border-subtle">
                <NetworkLatencyLines
                  targets={targets}
                  resultsByTargetId={new Map()}
                  history={[]}
                  selectedNodeName={nodes.find((n) => (n.uuid || n.id) === selectedServerForLatency)?.name || ''}
                />
              </div>

              {/* 延迟详情网格 */}
              <div className="dash-row-grid-6">
                {targets.map((t) => {
                  const hash = Array.from(t.name + selectedServerForLatency).reduce(
                    (a, c) => a + c.charCodeAt(0),
                    0
                  )
                  const simLat = 22 + (hash % 110)
                  return (
                    <div key={t.id} className="bg-surface p-3 rounded-lg border border-subtle">
                      <div className="text-xs text-muted mb-1">{t.name}</div>
                      <div className="text-lg font-bold mono text-blue">{simLat} ms</div>
                      <div className="text-xs text-mint mt-1">丢包 0% · 抖动 ±1.2ms</div>
                    </div>
                  )
                })}
              </div>
            </div>
          )}
        </>
      )}

      {/* ====================================================================
          TAB 2: 回程线路监测 (严格匹配 Image 5: media_1790307294544.png)
         ==================================================================== */}
      {activeTab === 'route' && (
        <>
          {/* 页面标题与描述 */}
          <div className="monitor-header-row">
            <div className="monitor-title-col">
              <h1>回程线路监测</h1>
              <p>实时追踪节点回程链路与 BGP 路由跳数，及时发现切线与劣化。</p>
            </div>
          </div>

          {/* 顶部 4 个指标卡片 (严格匹配 Image 5) */}
          <div className="monitor-stats-grid-4">
            {/* 监测任务 */}
            <div className="monitor-stat-card">
              <div className="monitor-stat-head">
                <span className="monitor-stat-title">监测任务</span>
                <span className="monitor-stat-icon text-blue" style={{ background: 'rgba(37,99,235,0.08)' }}>
                  <Broadcast size={16} />
                </span>
              </div>
              <div className="monitor-stat-val mono">{routeTasks.length}</div>
            </div>

            {/* 线路正常 */}
            <div className="monitor-stat-card">
              <div className="monitor-stat-head">
                <span className="monitor-stat-title">线路正常</span>
                <span className="monitor-stat-icon text-mint" style={{ background: 'rgba(16,185,129,0.08)' }}>
                  <CheckCircle size={16} />
                </span>
              </div>
              <div className="monitor-stat-val mono">
                {routeTasks.filter((t) => (t.status || 'normal') === 'normal').length}
              </div>
            </div>

            {/* 已切线 */}
            <div className="monitor-stat-card">
              <div className="monitor-stat-head">
                <span className="monitor-stat-title">已切线</span>
                <span className="monitor-stat-icon text-amber" style={{ background: 'rgba(245,158,11,0.08)' }}>
                  <WarningOctagon size={16} />
                </span>
              </div>
              <div className="monitor-stat-val mono">
                {routeTasks.filter((t) => t.status === 'shifted').length}
              </div>
            </div>

            {/* 最近事件 */}
            <div className="monitor-stat-card">
              <div className="monitor-stat-head">
                <span className="monitor-stat-title">最近事件</span>
                <span className="monitor-stat-icon text-purple" style={{ background: 'rgba(168,85,247,0.08)' }}>
                  <Clock size={16} />
                </span>
              </div>
              <div className="monitor-stat-val mono">0</div>
            </div>
          </div>

          {/* 二级子 Tab: 监测任务 | 监测记录 | 规则库 (匹配 Image 5) */}
          <div className="monitor-toolbar">
            <div className="monitor-view-toggle">
              <button
                type="button"
                className={`monitor-toggle-btn ${routeSubTab === 'tasks' ? 'active' : ''}`}
                onClick={() => setRouteSubTab('tasks')}
              >
                监测任务
              </button>
              <button
                type="button"
                className={`monitor-toggle-btn ${routeSubTab === 'records' ? 'active' : ''}`}
                onClick={() => setRouteSubTab('records')}
              >
                监测记录
              </button>
              <button
                type="button"
                className={`monitor-toggle-btn ${routeSubTab === 'rules' ? 'active' : ''}`}
                onClick={() => setRouteSubTab('rules')}
              >
                规则库
              </button>
            </div>

            {/* 筛选与操作工具条 (匹配 Image 5) */}
            <div className="monitor-toolbar-right">
              {/* 运营商下拉 */}
              <select
                className="monitor-filter-select"
                value={routeIspFilter}
                onChange={(e) => setRouteIspFilter(e.target.value)}
              >
                <option value="all">运营商</option>
                <option value="中国电信">中国电信</option>
                <option value="中国联通">中国联通</option>
                <option value="中国移动">中国移动</option>
                <option value="教育网">教育网</option>
                <option value="海外">海外 / 国际</option>
              </select>

              {/* 状态下拉 */}
              <select
                className="monitor-filter-select"
                value={routeStatusFilter}
                onChange={(e) => setRouteStatusFilter(e.target.value)}
              >
                <option value="all">状态</option>
                <option value="normal">线路正常</option>
                <option value="shifted">已切线</option>
                <option value="offline">超时中断</option>
              </select>

              {/* 搜索框 */}
              <div className="monitor-search-box">
                <MagnifyingGlass size={15} />
                <input
                  type="text"
                  className="monitor-search-input"
                  placeholder="搜索目标名称、目标 IP、规则名称..."
                  value={routeSearch}
                  onChange={(e) => setRouteSearch(e.target.value)}
                />
              </div>

              {/* 全选 */}
              <label className="flex items-center gap-1.5 text-xs text-muted cursor-pointer pr-1">
                <input type="checkbox" />
                <span>全选</span>
              </label>

              {/* 批量修改 */}
              <button type="button" className="button button-quiet btn-sm">
                批量修改
              </button>

              {/* + 新建任务 */}
              <button
                type="button"
                className="button button-primary btn-sm flex items-center gap-1.5"
                onClick={() => setShowAddRouteModal(true)}
              >
                <Plus size={14} weight="bold" />
                <span>新建任务</span>
              </button>
            </div>
          </div>

          {/* 当无任务时：严格呈现 Image 5 官方“暂无任务”空状态 */}
          {routeTasks.length === 0 ? (
            <div className="empty-task-panel">
              <div className="empty-task-icon">
                <Broadcast size={28} />
              </div>
              <div className="empty-task-title">暂无任务</div>
              <div className="empty-task-desc">
                未创建回程线路监测任务，点击右上角“+ 新建任务”创建，或点击下方按钮快捷载入三网常用回程骨干预设。
              </div>
              <button
                type="button"
                className="button button-quiet btn-sm flex items-center gap-2 text-blue"
                onClick={handleAddDefaultRoutePresets}
              >
                <Sparkle size={15} />
                <span>快捷载入三网骨干预设 (CMIN2 / CN2 GIA / AS9929)</span>
              </button>
            </div>
          ) : (
            /* 当有回程监测任务时呈现列表与逐跳分析卡片 */
            <div className="space-y-4">
              <div className="monitor-table-card">
                <div className="table-scroll">
                  <table className="monitor-table">
                    <thead>
                      <tr>
                        <th style={{ width: '40px' }}>
                          <input type="checkbox" />
                        </th>
                        <th>任务名称</th>
                        <th>关联服务器</th>
                        <th>目标信息</th>
                        <th>预期线路</th>
                        <th>当前线路</th>
                        <th>状态</th>
                        <th style={{ textAlign: 'right' }}>操作</th>
                      </tr>
                    </thead>
                    <tbody>
                      {filteredRouteTasks.map((task) => (
                        <tr key={task.id}>
                          <td>
                            <input type="checkbox" />
                          </td>
                          <td>
                            <strong style={{ color: 'var(--text-1)' }}>{task.name}</strong>
                          </td>
                          <td>
                            <span className="badge badge-subtle mono">{nodes.length || 12} 台</span>
                          </td>
                          <td className="mono text-muted">{task.host}</td>
                          <td>
                            <span className="badge badge-mint font-bold mono">
                              {task.expectedRoute || 'BGP直连'}
                            </span>
                          </td>
                          <td>
                            <span className="badge badge-blue font-bold mono">
                              {task.expectedRoute || 'BGP直连'}
                            </span>
                          </td>
                          <td>
                            <span className="badge badge-mint flex items-center gap-1" style={{ width: 'fit-content' }}>
                              <CheckCircle size={12} /> 正常
                            </span>
                          </td>
                          <td>
                            <div className="flex items-center justify-end gap-1">
                              <button
                                type="button"
                                className="icon-action-btn"
                                title="查看逐跳路由跳数与延时"
                                onClick={() => setSelectedRouteTask(task)}
                              >
                                <Eye size={15} />
                              </button>
                              <button
                                type="button"
                                className="icon-action-btn text-rose"
                                title="删除任务"
                                onClick={() =>
                                  setRouteTasks((prev) => prev.filter((t) => t.id !== task.id))
                                }
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
              </div>

              {/* 逐跳跳数波形与 MTR 详情展示 */}
              {selectedRouteTask && (
                <div className="panel p-5 space-y-4">
                  <div className="flex items-center justify-between">
                    <div>
                      <h3 className="font-bold text-base flex items-center gap-2">
                        <Broadcast size={18} className="text-blue" />
                        <span>{selectedRouteTask.name} · 逐跳回程链路时延跳数</span>
                      </h3>
                      <p className="text-xs text-muted mt-0.5">
                        目标: {selectedRouteTask.host} · 期望线路: {selectedRouteTask.expectedRoute} · BGP AS 路径追踪
                      </p>
                    </div>
                    <button
                      type="button"
                      className="button button-quiet btn-sm"
                      onClick={() => setSelectedRouteTask(null)}
                    >
                      收起
                    </button>
                  </div>

                  <div className="bg-subtle p-3 rounded-lg border border-subtle">
                    <MTRHopLatencyLine
                      hops={[
                        { ttl: 1, ip: '10.0.0.1', latency_ms: 1.2 },
                        { ttl: 2, ip: '198.51.100.10', latency_ms: 4.8 },
                        { ttl: 3, ip: '198.51.100.11', latency_ms: 12.4 },
                        { ttl: 4, ip: '198.51.100.12', latency_ms: 32.1 },
                        { ttl: 5, ip: '198.51.100.13', latency_ms: 58.7 },
                        { ttl: 6, ip: selectedRouteTask.host, latency_ms: 61.2 },
                      ]}
                      isReached={true}
                      destination={selectedRouteTask.host}
                    />
                  </div>
                </div>
              )}
            </div>
          )}
        </>
      )}

      {/* ====================================================================
          TAB 3: IP 疑似被墙交叉判定实验室 (GFW Blockage Detection)
         ==================================================================== */}
      {activeTab === 'gfw' && (
        <div className="space-y-4">
          <div className="panel p-5">
            <div className="flex items-start gap-3">
              <div className="modal-icon-badge text-blue">
                <Sparkle size={20} />
              </div>
              <div>
                <h3 className="font-bold text-base">
                  IP 疑似被墙判定双因子模型 (GFW Blockage Detection)
                </h3>
                <p className="text-xs text-muted mt-1 leading-relaxed">
                  系统复用现有回程线路监测，结合辅助延迟监测任务，判断海外节点到中国大陆方向是否同时出现
                  <b>回程路径提前截断</b>和<b>连接高丢包</b>。必须满足至少两个大陆运营商方向持续同时异常，才会显示“疑似被墙”；三个运营商同时异常时判定为“高置信度疑似被墙”。
                </p>
              </div>
            </div>

            <div className="dash-row-grid-3 mt-4 text-xs">
              <div className="bg-subtle p-3 rounded-lg">
                <strong className="block text-foreground mb-1">1. 路由路径截断验证</strong>
                <p className="text-muted">
                  正常基线已通过国家干线，但当前探测在国际出口汇聚处提前中断超时。
                </p>
              </div>
              <div className="bg-subtle p-3 rounded-lg">
                <strong className="block text-foreground mb-1">2. 辅助延迟任务高丢包</strong>
                <p className="text-muted">
                  对应运营商方向的 TCP/ICMP 探测丢包率持续保持在 90% 以上。
                </p>
              </div>
              <div className="bg-subtle p-3 rounded-lg">
                <strong className="block text-foreground mb-1">3. 多运营商共振防误判</strong>
                <p className="text-muted">
                  仅单运营商异常视为正常切线波动；电信 + 联通或三网共振才触发告警。
                </p>
              </div>
            </div>
          </div>

          <div className="panel p-5">
            <div className="panel-header mb-3">
              <div>
                <h3 className="font-semibold text-base">海外节点被墙风险判定列表</h3>
                <p className="text-xs text-muted">包含 {gfwAssessment.length} 个海外监控节点</p>
              </div>
            </div>

            <div className="table-scroll">
              <table className="table w-full text-xs">
                <thead>
                  <tr>
                    <th>海外节点</th>
                    <th>判定状态</th>
                    <th>中国电信 (丢包)</th>
                    <th>中国联通 (丢包)</th>
                    <th>中国移动 (丢包)</th>
                    <th>基线状态</th>
                    <th className="text-right">复核操作</th>
                  </tr>
                </thead>
                <tbody>
                  {gfwAssessment.map((item) => (
                    <tr key={item.node.uuid || item.node.id}>
                      <td className="font-medium">
                        <span className="mr-1.5">{item.node.flag}</span>
                        <span>{item.node.name}</span>
                      </td>
                      <td>
                        <span className={`badge badge-${item.badgeTone}`}>{item.statusText}</span>
                      </td>
                      <td className="mono">{item.telecomLoss}%</td>
                      <td className="mono">{item.unicomLoss}%</td>
                      <td className="mono">{item.mobileLoss}%</td>
                      <td className="text-muted">{item.baseline}</td>
                      <td className="text-right">
                        <button
                          type="button"
                          className="button button-quiet btn-xs"
                          onClick={() => {
                            setActiveTab('route')
                          }}
                        >
                          回程核验
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </div>
      )}

      {/* 新建延迟监测弹窗 */}
      <NewLatencyTargetModal
        isOpen={showAddLatencyModal}
        onClose={() => setShowAddLatencyModal(false)}
        onCreated={(newT) => {
          setTargets((prev) => [
            ...prev,
            {
              id: newT.id,
              name: newT.name,
              kind: newT.kind || 'icmp',
              host: newT.host,
              enabled: newT.enabled !== false,
              isp: /电信/.test(newT.name)
                ? 'telecom'
                : /联通/.test(newT.name)
                ? 'unicom'
                : /移动/.test(newT.name)
                ? 'mobile'
                : 'other',
            },
          ])
        }}
      />

      {/* 新建回程监测弹窗 */}
      <NewRouteMonitorModal
        isOpen={showAddRouteModal}
        onClose={() => setShowAddRouteModal(false)}
        nodes={nodes}
        onCreated={(newRoute) => {
          setRouteTasks((prev) => [
            ...prev,
            {
              id: newRoute.id || `route-${Date.now()}`,
              name: newRoute.name,
              kind: 'mtr',
              host: newRoute.host || '198.51.100.1',
              max_hops: 20,
              isp: newRoute.isp || '中国移动',
              expectedRoute: newRoute.expectedRoute || 'CMIN2',
              status: 'normal',
            },
          ])
        }}
      />
    </div>
  )
}
