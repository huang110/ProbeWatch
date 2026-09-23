import { useCallback, useEffect, useRef, useState } from 'react'
import { Bell, CaretLineLeft, CaretLineRight, Clock, DotsThree, Eye, List, Pulse, SignOut } from '@phosphor-icons/react'
import { normalizeAlert, normalizeNode, numeric, safeText, formatTimeOfDay } from './lib/format.js'
import { fetchCsrfToken, fetchGuestStatus, performLogout } from './lib/api.js'
import { NodeDrawer } from './components/NodeDrawer.jsx'
import { NodeDetailPage } from './components/NodeDetailPage.jsx'
import { OverviewPage } from './components/OverviewPage.jsx'
import { SubPage } from './components/SubPage.jsx'
import { GuestView } from './components/GuestView.jsx'
import { TOTPVerifyPage } from './components/TOTPVerifyPage.jsx'
import { BillingCenter } from './components/BillingCenter.jsx'
import { GlobeHemisphereWest, WifiHigh, Broadcast, CloudArrowDown, SquaresFour, SlidersHorizontal, Database, Coins } from '@phosphor-icons/react'

const navItems = [
  { id: 'overview', label: '总览', icon: SquaresFour },
  { id: 'nodes', label: '节点', icon: GlobeHemisphereWest },
  { id: 'billing', label: '账单与价值', icon: Coins },
  { id: 'network', label: '网络检测', icon: WifiHigh },
  { id: 'mtr', label: 'MTR 路由', icon: Broadcast },
  { id: 'media', label: '流媒体', icon: CloudArrowDown },
  { id: 'alerts', label: '告警事件', icon: Bell },
]
const REFRESH_OPTIONS = [10, 30, 60]
const OVERVIEW_INTERVAL_MS = 300000
const pageTitleFor = (page) => page === 'node-detail' ? '节点详情' : navItems.find((item) => item.id === page)?.label || ({ targets: '检测目标', settings: '系统设置' }[page] || '总览')

function Topbar({ activeNav, clockText, autoRefresh, refreshInterval, onToggleRefresh, onIntervalChange, lastSyncText, apiState, me, onNavigate, onOpenMobileNav, onSwitchToGuest, onLogout }) {
  return <header className="topbar">
    <div className="topbar-left">
      <button className="icon-button mobile-menu" aria-label="打开导航菜单" onClick={onOpenMobileNav}><List size={19} /></button>
      <span className="topbar-clock" title="当前时间"><Clock size={14} /><b className="mono">{clockText}</b></span>
      <div className="breadcrumb"><span>监控</span><span className="breadcrumb-slash">/</span><strong>{pageTitleFor(activeNav)}</strong></div>
    </div>
    <div className="top-actions">
      <button type="button" className="button button-quiet btn-sm top-guest-btn" onClick={onSwitchToGuest} title="切换至访客视角的只读监控大屏">
        <Eye size={15} />
        <span>游客模式</span>
      </button>
      <div className="refresh-controls" role="group" aria-label="全局自动刷新">
        <button type="button" className={`refresh-toggle ${autoRefresh ? 'refresh-on' : ''}`} aria-pressed={autoRefresh} onClick={onToggleRefresh}><span className="refresh-toggle-dot" aria-hidden="true" />自动刷新</button>
        <select className="refresh-interval" value={refreshInterval} onChange={(event) => onIntervalChange(Number(event.target.value))} aria-label="自动刷新间隔">
          {REFRESH_OPTIONS.map((seconds) => <option key={seconds} value={seconds}>{seconds}s</option>)}
        </select>
      </div>
      <span className="last-sync">最后同步 <b>{lastSyncText}</b></span>
      <span className="sync-state"><span className={`status-dot status-${apiState.kind === 'ok' ? 'online' : 'attention'}`} />{apiState.kind === 'ok' ? 'API 已连接' : 'API 状态未知'}</span>
      <button className="icon-button" aria-label="查看告警" onClick={() => onNavigate('alerts')}><Bell size={19} /></button>
      <button type="button" className="button button-quiet btn-sm text-rose top-logout-btn" onClick={onLogout} title="退出当前管理员登录">
        <SignOut size={15} />
        <span>退出</span>
      </button>
      <div className="profile-avatar profile-avatar-top" title={`当前登录: ${me?.login || 'Admin'}`}>
        {me ? String(me.login || me.name || 'P').slice(0, 2).toUpperCase() : '—'}
      </div>
    </div>
  </header>
}

function Sidebar({ activeNav, onNavigate, me, apiState, collapsed, onToggleCollapse, mobileOpen, onCloseMobile, onSwitchToGuest, onLogout }) {
  return <>
    <aside className={`sidebar ${collapsed ? 'sidebar-collapsed' : ''} ${mobileOpen ? 'sidebar-open' : ''}`} aria-label="侧边栏">
      <div className="sidebar-head">
        <div className="brand-lockup"><div className="brand-mark"><Pulse size={21} weight="bold" /></div><div className="brand-text"><strong>ProbeWatch</strong><span>纯监控控制台</span></div></div>
        <button className="icon-button collapse-toggle" aria-label={collapsed ? '展开侧边栏' : '折叠侧边栏'} onClick={onToggleCollapse}>{collapsed ? <CaretLineRight size={16} /> : <CaretLineLeft size={16} />}</button>
      </div>
      <div className="workspace-switcher"><div className="workspace-avatar">P</div><div className="workspace-text"><span>工作区</span><strong>{me?.login || me?.name || 'ProbeWatch'}</strong></div></div>
      <nav className="side-nav" aria-label="主导航">
        <span className="nav-section-label">监控</span>
        {navItems.map(({ id, label, icon: Icon }) => <button key={id} type="button" title={label} className={`nav-item ${activeNav === id ? 'nav-item-active' : ''}`} onClick={() => onNavigate(id)}><Icon size={18} weight={activeNav === id ? 'fill' : 'regular'} /><span>{label}</span></button>)}
        <span className="nav-section-label nav-section-spaced">配置</span>
        <button type="button" title="检测目标" className={`nav-item ${activeNav === 'targets' ? 'nav-item-active' : ''}`} onClick={() => onNavigate('targets')}><SlidersHorizontal size={18} /><span>检测目标</span></button>
        <button type="button" title="系统设置" className={`nav-item ${activeNav === 'settings' ? 'nav-item-active' : ''}`} onClick={() => onNavigate('settings')}><Database size={18} /><span>系统设置</span></button>
        <span className="nav-section-label nav-section-spaced">模式切换</span>
        <button type="button" title="切换至访客只读大屏" className="nav-item nav-item-guest-switch" onClick={onSwitchToGuest}><Eye size={18} /><span>游客大屏</span></button>
      </nav>
      <div className="sidebar-footer">
        <div className="health-chip"><span className={`status-dot status-${apiState.kind === 'ok' ? 'online' : apiState.kind === 'loading' ? 'attention' : 'offline'}`} /><span>{apiState.kind === 'ok' ? 'API 已连接' : apiState.kind === 'auth' ? '需要登录' : apiState.kind === 'loading' ? '正在连接 API' : 'API 不可用'}</span><span className="mono health-version">v0.1.0</span></div>
        <div className="profile-row">
          <div className="profile-avatar">{me ? String(me.login || me.name || 'P').slice(0, 2).toUpperCase() : '—'}</div>
          <div className="profile-text">
            <strong>{me?.login || me?.name || '未登录'}</strong>
            <span>{me?.provider === 'local' ? '本地管理员' : me ? 'GitHub 会话' : '未认证'}</span>
          </div>
          <button
            type="button"
            className="icon-button btn-logout-sidebar text-rose"
            onClick={onLogout}
            title="退出登录"
            aria-label="退出登录"
          >
            <SignOut size={17} />
          </button>
        </div>
      </div>
    </aside>
    {mobileOpen && <div className="sidebar-backdrop" onClick={onCloseMobile} aria-hidden="true" />}
  </>
}

export function App() {
  const [activeNav, setActiveNav] = useState('overview')
  const [selectedNode, setSelectedNode] = useState(null)
  const [detailNode, setDetailNode] = useState(null)
  const [data, setData] = useState([])
  const [alerts, setAlerts] = useState([])
  const [ackingId, setAckingId] = useState(null)
  const [overview, setOverview] = useState(null)
  const [statHistory, setStatHistory] = useState([])
  const [lossRates, setLossRates] = useState({})
  const [rates, setRates] = useState({})
  const [history, setHistory] = useState([])
  const [historyLoading, setHistoryLoading] = useState(false)
  const historyAbortRef = useRef(null)
  const [checksSummary, setChecksSummary] = useState(null)
  const [checksLoading, setChecksLoading] = useState(false)
  const checksAbortRef = useRef(null)
  const [traffic, setTraffic] = useState(null)
  const [trafficLoading, setTrafficLoading] = useState(false)
  const [trafficPeriod, setTrafficPeriod] = useState('day')
  const trafficAbortRef = useRef(null)
  const [isRefreshing, setIsRefreshing] = useState(false)
  const [lastSync, setLastSync] = useState(null)
  const [apiState, setApiState] = useState({ kind: 'loading', message: '正在加载 API 数据…' })
  const [me, setMe] = useState(null)
  const [publicStatus, setPublicStatus] = useState(null)
  const [guestPreview, setGuestPreview] = useState(false)
  const [clock, setClock] = useState(() => new Date())
  const [autoRefresh, setAutoRefresh] = useState(true)
  const [refreshInterval, setRefreshInterval] = useState(30)
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false)
  const [mobileNavOpen, setMobileNavOpen] = useState(false)
  const coreAbortRef = useRef(null)
  const coreRequestRef = useRef(0)
  const overviewAbortRef = useRef(null)
  const counterRef = useRef(new Map())

  const markSync = () => setLastSync(Date.now())

  const computeRates = useCallback((nodes) => {
    const now = Date.now()
    const next = {}
    nodes.forEach((node) => {
      const key = node.uuid || node.id
      if (!key || node.rx === null || node.tx === null) return
      const prev = counterRef.current.get(key)
      if (prev && now > prev.t && node.rx >= prev.rx && node.tx >= prev.tx) {
        const seconds = (now - prev.t) / 1000
        if (seconds >= 1) next[key] = { down: (node.rx - prev.rx) / seconds, up: (node.tx - prev.tx) / seconds }
      }
      counterRef.current.set(key, { rx: node.rx, tx: node.tx, t: now })
    })
    return next
  }, [])

  const enterGuest = useCallback(async (controller, current) => {
    const guestStatus = await fetchGuestStatus(controller.signal).catch(() => null)
    if (current !== coreRequestRef.current) return
    setMe(null); setData([]); setOverview(null); setAlerts([]); setRates({}); setLossRates({})
    setPublicStatus(guestStatus)
    setApiState({ kind: 'guest', message: '' })
  }, [])

  // 实时数据轮询：节点 / 告警 / 会话，间隔由顶栏开关控制（默认 30s）。
  const loadCore = useCallback(async (manual = false) => {
    const current = ++coreRequestRef.current
    coreAbortRef.current?.abort()
    const controller = new AbortController()
    coreAbortRef.current = controller
    if (manual) setIsRefreshing(true)
    try {
      const meResponse = await fetch('/api/me', { credentials: 'same-origin', signal: controller.signal })
      if (meResponse.status === 401) { await enterGuest(controller, current); return }
      if (!meResponse.ok) throw new Error(`me:${meResponse.status}`)
      const meJson = await meResponse.json()
      const [nodesResponse, alertsResponse] = await Promise.all([
        fetch('/api/nodes', { credentials: 'same-origin', signal: controller.signal }),
        fetch('/api/alerts?status=open,acked', { credentials: 'same-origin', signal: controller.signal }),
      ])
      if (nodesResponse.status === 401 || alertsResponse.status === 401) { await enterGuest(controller, current); return }
      if (!nodesResponse.ok) throw new Error(`nodes:${nodesResponse.status}`)
      if (!alertsResponse.ok) throw new Error(`alerts:${alertsResponse.status}`)
      const nodesJson = await nodesResponse.json()
      const alertsJson = await alertsResponse.json()
      if (!Array.isArray(nodesJson) || !Array.isArray(alertsJson)) throw new Error('nodes:invalid-json')
      if (current !== coreRequestRef.current) return
      const normalized = nodesJson.map(normalizeNode)
      setMe(meJson)
      setData(normalized)
      setAlerts(alertsJson.map(normalizeAlert))
      setRates(computeRates(normalized))
      markSync()
      setApiState(normalized.length ? { kind: 'ok', message: '' } : { kind: 'empty', message: 'API 返回空节点数组，暂无节点数据。' })
    } catch (error) {
      if (error?.name === 'AbortError') return
      if (current === coreRequestRef.current) { setMe(null); setData([]); setApiState({ kind: 'error', message: '无法加载节点数据，请检查网络或稍后重试。' }) }
    } finally {
      if (current === coreRequestRef.current) setIsRefreshing(false)
    }
  }, [computeRates, enterGuest])

  // 历史统计轮询：overview 聚合，独立 5 分钟间隔。
  const loadOverview = useCallback(async () => {
    overviewAbortRef.current?.abort()
    const controller = new AbortController()
    overviewAbortRef.current = controller
    try {
      const response = await fetch('/api/overview', { credentials: 'same-origin', signal: controller.signal })
      if (response.status === 401) return
      if (!response.ok) throw new Error(`overview:${response.status}`)
      const json = await response.json()
      if (!json || typeof json !== 'object' || Array.isArray(json)) throw new Error('overview:invalid-json')
      if (controller.signal.aborted) return
      setOverview(json)
      markSync()
      const nodes = json.nodes || {}
      const checks = json.checks || {}
      const resources = json.resources || {}
      const memUsed = numeric(resources.memory_used_bytes)
      const memTotal = numeric(resources.memory_total_bytes)
      const point = {
        online: numeric(nodes.online),
        latency: numeric(checks.avg_latency_ms),
        success: numeric(checks.success_rate),
        traffic: (numeric(resources.network_rx_bytes_delta) || 0) + (numeric(resources.network_tx_bytes_delta) || 0),
        cpu: numeric(resources.cpu_percent),
        mem: memUsed !== null && memTotal !== null && memTotal > 0 ? Math.round((memUsed / memTotal) * 1000) / 10 : null,
      }
      setStatHistory((current) => [...current, point].slice(-40))
    } catch (error) {
      if (error?.name === 'AbortError') return
      // overview 失败保持既有卡片数据，静默等待下一轮。
    }
  }, [])

  useEffect(() => {
    loadCore(true)
    loadOverview()
    return () => { coreAbortRef.current?.abort(); overviewAbortRef.current?.abort(); historyAbortRef.current?.abort(); checksAbortRef.current?.abort(); trafficAbortRef.current?.abort() }
  }, [loadCore, loadOverview])

  useEffect(() => {
    if (!autoRefresh) return undefined
    const id = setInterval(() => { if (document.visibilityState === 'visible') loadCore(false) }, refreshInterval * 1000)
    return () => clearInterval(id)
  }, [autoRefresh, refreshInterval, loadCore])

  useEffect(() => {
    const id = setInterval(() => { if (document.visibilityState === 'visible') loadOverview() }, OVERVIEW_INTERVAL_MS)
    return () => clearInterval(id)
  }, [loadOverview])

  useEffect(() => {
    const onVisibility = () => { if (document.visibilityState === 'visible') { loadCore(false); loadOverview() } }
    document.addEventListener('visibilitychange', onVisibility)
    return () => document.removeEventListener('visibilitychange', onVisibility)
  }, [loadCore, loadOverview])

  useEffect(() => {
    const id = setInterval(() => setClock(new Date()), 1000)
    return () => clearInterval(id)
  }, [])

  const nodeKey = data.map((node) => node.uuid || node.id).join('|')
  useEffect(() => {
    const uuids = nodeKey ? nodeKey.split('|') : []
    if (!uuids.length) { setLossRates({}); return undefined }
    const controller = new AbortController()
    Promise.all(uuids.map((uuid) => fetch(`/api/nodes/${encodeURIComponent(uuid)}/checks/summary`, { credentials: 'same-origin', signal: controller.signal }).then(async (response) => {
      if (!response.ok) return [uuid, null]
      const rows = await response.json()
      let total = 0
      let failure = 0
      if (Array.isArray(rows)) rows.forEach((row) => { const rowTotal = numeric(row?.total); const rowFailure = numeric(row?.failure); if (rowTotal !== null && rowTotal > 0) { total += rowTotal; failure += rowFailure || 0 } })
      return [uuid, total > 0 ? failure / total : null]
    }).catch(() => [uuid, null]))).then((entries) => { if (!controller.signal.aborted) setLossRates(Object.fromEntries(entries)) })
    return () => controller.abort()
  }, [nodeKey])

  useEffect(() => {
    const uuid = selectedNode?.uuid || selectedNode?.id
    if (!uuid) { setHistory([]); return undefined }
    historyAbortRef.current?.abort()
    const controller = new AbortController()
    historyAbortRef.current = controller
    setHistoryLoading(true)
    fetch(`/api/nodes/${encodeURIComponent(uuid)}/resource/history`, { credentials: 'same-origin', signal: controller.signal }).then(async (response) => {
      if (!response.ok) throw new Error(`history:${response.status}`)
      const json = await response.json()
      if (!Array.isArray(json)) throw new Error('history:invalid-json')
      return json.map((item) => { const resource = item?.resource || {}; const memoryTotal = numeric(resource.memory_total_bytes); const memoryUsed = numeric(resource.memory_used_bytes); return { cpu: numeric(resource.cpu_percent), mem: memoryUsed !== null && memoryTotal !== null && memoryTotal > 0 ? Math.round((memoryUsed / memoryTotal) * 1000) / 10 : null } }).reverse()
    }).then((points) => { if (!controller.signal.aborted) setHistory(points) }).catch((error) => { if (error?.name !== 'AbortError' && !controller.signal.aborted) setHistory([]) }).finally(() => { if (!controller.signal.aborted) setHistoryLoading(false) })
    return () => controller.abort()
  }, [selectedNode])

  const analyticsNode = selectedNode || detailNode
  const analyticsUuid = analyticsNode ? (safeText(analyticsNode.uuid) || safeText(analyticsNode.id)) : ''
  useEffect(() => {
    if (!analyticsUuid) { setChecksSummary(null); setChecksLoading(false); return undefined }
    checksAbortRef.current?.abort()
    const controller = new AbortController()
    checksAbortRef.current = controller
    setChecksLoading(true)
    fetch(`/api/nodes/${encodeURIComponent(analyticsUuid)}/checks/summary`, { credentials: 'same-origin', signal: controller.signal }).then(async (response) => { if (!response.ok) throw new Error(`checks:${response.status}`); const json = await response.json(); if (!Array.isArray(json)) throw new Error('checks:invalid-json'); return json }).then((json) => { if (!controller.signal.aborted) setChecksSummary(json) }).catch((error) => { if (error?.name !== 'AbortError' && !controller.signal.aborted) setChecksSummary(null) }).finally(() => { if (!controller.signal.aborted) setChecksLoading(false) })
    return () => controller.abort()
  }, [analyticsUuid])

  useEffect(() => { setTrafficPeriod('day') }, [analyticsUuid])

  useEffect(() => {
    if (!analyticsUuid) { setTraffic(null); setTrafficLoading(false); return undefined }
    trafficAbortRef.current?.abort()
    const controller = new AbortController()
    trafficAbortRef.current = controller
    setTrafficLoading(true)
    fetch(`/api/nodes/${encodeURIComponent(analyticsUuid)}/traffic?period=${encodeURIComponent(trafficPeriod)}`, { credentials: 'same-origin', signal: controller.signal }).then(async (response) => { if (!response.ok) throw new Error(`traffic:${response.status}`); const json = await response.json(); if (!json || typeof json !== 'object' || Array.isArray(json)) throw new Error('traffic:invalid-json'); return json }).then((json) => { if (!controller.signal.aborted) setTraffic(json) }).catch((error) => { if (error?.name !== 'AbortError' && !controller.signal.aborted) setTraffic(null) }).finally(() => { if (!controller.signal.aborted) setTrafficLoading(false) })
    return () => controller.abort()
  }, [analyticsUuid, trafficPeriod])

  const ackAlert = async (id) => {
    setAckingId(id)
    try {
      const csrfToken = await fetchCsrfToken()
      const response = await fetch(`/api/alerts/${encodeURIComponent(id)}/ack`, { method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken }, body: '{}' })
      if (response.status === 401) throw new Error('auth')
      if (response.status === 403) throw new Error('forbidden')
      if (response.status === 409) throw new Error('conflict')
      if (!response.ok) throw new Error('error')
      const updated = normalizeAlert(await response.json())
      setAlerts((current) => current.map((alert) => alert.id === id ? updated : alert))
      setApiState({ kind: 'ok', message: '告警已确认。' })
    } catch (error) {
      const messages = { auth: '需要登录才能确认告警。', forbidden: '告警确认被拒绝，请刷新后重试。', conflict: '告警已解决，无法重复确认。', csrf: '无法获取安全令牌，请刷新后重试。', error: '确认告警失败，请稍后重试。' }
      setApiState({ kind: error?.message === 'auth' ? 'auth' : 'error', message: messages[error?.message] || messages.error })
    } finally { setAckingId(null) }
  }

  const navigate = (page) => { setActiveNav(page); setMobileNavOpen(false) }
  const refreshAll = () => { loadCore(true); loadOverview() }
  const lastSyncText = lastSync ? formatTimeOfDay(lastSync) : '—'
  const clockText = formatTimeOfDay(clock)

  if (window.location.pathname === '/login/2fa') return <TOTPVerifyPage />
  if (guestPreview || apiState.kind === 'guest') {
    return (
      <GuestView
        status={publicStatus}
        isRefreshing={isRefreshing}
        onRefresh={refreshAll}
        onLoginSuccess={() => { setGuestPreview(false); refreshAll() }}
        isPreview={guestPreview}
        onExitPreview={() => setGuestPreview(false)}
        onLogout={performLogout}
      />
    )
  }
  return <div className="app-shell">
    <Sidebar
      activeNav={activeNav}
      onNavigate={navigate}
      me={me}
      apiState={apiState}
      collapsed={sidebarCollapsed}
      onToggleCollapse={() => setSidebarCollapsed((value) => !value)}
      mobileOpen={mobileNavOpen}
      onCloseMobile={() => setMobileNavOpen(false)}
      onSwitchToGuest={() => setGuestPreview(true)}
      onLogout={performLogout}
    />
    <main className="main-content">
      <Topbar
        activeNav={activeNav}
        clockText={clockText}
        autoRefresh={autoRefresh}
        refreshInterval={refreshInterval}
        onToggleRefresh={() => setAutoRefresh((value) => !value)}
        onIntervalChange={setRefreshInterval}
        lastSyncText={lastSyncText}
        apiState={apiState}
        me={me}
        onNavigate={navigate}
        onOpenMobileNav={() => setMobileNavOpen(true)}
        onSwitchToGuest={() => setGuestPreview(true)}
        onLogout={performLogout}
      />
      <div className="content-wrap">
        {activeNav === 'overview' ? <OverviewPage data={data} overview={overview} alerts={alerts} lossRates={lossRates} rates={rates} statHistory={statHistory} onAck={ackAlert} ackingId={ackingId} selectedNode={selectedNode} onSelectNode={setSelectedNode} onNavigate={navigate} isRefreshing={isRefreshing} onRefresh={refreshAll} lastSyncText={lastSyncText} apiState={apiState} />
          : activeNav === 'node-detail' && detailNode ? <NodeDetailPage node={detailNode} history={history} historyLoading={historyLoading} checksSummary={checksSummary} checksLoading={checksLoading} traffic={traffic} trafficLoading={trafficLoading} trafficPeriod={trafficPeriod} onTrafficPeriodChange={setTrafficPeriod} onBack={() => navigate('nodes')} rates={rates} />
            : activeNav === 'billing' ? <BillingCenter nodes={data} />
              : <SubPage page={activeNav} data={data} alerts={alerts} onAck={ackAlert} ackingId={ackingId} onBack={() => navigate('overview')} onSelectNode={setSelectedNode} rates={rates} lossRates={lossRates} />}
        <footer className="content-footer"><span><span className={`status-dot status-${apiState.kind === 'ok' ? 'online' : 'attention'}`} />{apiState.kind === 'ok' ? '数据来自实时 API · 资源与历史统计独立刷新' : apiState.message}</span><span className="footer-divider" /><span>资源字段缺失时显示 —</span></footer>
      </div>
    </main>
    {selectedNode && <NodeDrawer node={selectedNode} rates={rates} onClose={() => setSelectedNode(null)} onOpenDetails={(node) => { setSelectedNode(null); setDetailNode(node); navigate('node-detail') }} />}
  </div>
}
