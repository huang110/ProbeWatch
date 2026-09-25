import { useCallback, useEffect, useRef, useState } from 'react'
import { ArrowLeft, Bell, CaretDown, CaretLineLeft, CaretLineRight, Clock, DotsThree, Eye, List, Pulse, SignOut } from '@phosphor-icons/react'
import { normalizeAlert, normalizeNode, numeric, safeText, formatTimeOfDay, safeArray, detectRegionAndFlag } from './lib/format.js'
import { fetchCsrfToken, fetchGuestStatus, performLogout } from './lib/api.js'
import { getAllNodeCustomMeta } from './lib/billing.js'
import { NodeDrawer } from './components/NodeDrawer.jsx'
import { NodeDetailPage } from './components/NodeDetailPage.jsx'
import { OverviewPage } from './components/OverviewPage.jsx'
import { SubPage } from './components/SubPage.jsx'
import { GuestView } from './components/GuestView.jsx'
import { TOTPVerifyPage } from './components/TOTPVerifyPage.jsx'
import { BillingCenter } from './components/BillingCenter.jsx'
import { DashboardView } from './components/DashboardView.jsx'
import { ServerManageView } from './components/ServerManageView.jsx'
import { MonitoringView } from './components/MonitoringView.jsx'
import { TrafficReportView } from './components/TrafficReportView.jsx'
import { AlertCenterView } from './components/AlertCenterView.jsx'
import { LogsView } from './components/LogsView.jsx'
import { ThemeToggle } from './components/ThemeToggle.jsx'
import {
  GlobeHemisphereWest,
  WifiHigh,
  Broadcast,
  CloudArrowDown,
  SquaresFour,
  SlidersHorizontal,
  Database,
  Coins,
  ChartBar,
  Scroll,
} from '@phosphor-icons/react'

const navItems = [
  { id: 'overview', label: '仪表盘', icon: SquaresFour },
  { id: 'servers', label: '服务器管理', icon: GlobeHemisphereWest },
  { id: 'billing', label: '成本中心', icon: Coins },
  {
    id: 'monitoring',
    label: '监测',
    icon: Broadcast,
    children: [
      { id: 'latency', label: '延迟监测' },
      { id: 'route', label: '回程线路监测' },
    ],
  },
  { id: 'traffic', label: '流量与报告', icon: ChartBar },
  {
    id: 'notifications',
    label: '通知与告警',
    icon: Bell,
    children: [
      { id: 'notify-channel', label: '通知渠道' },
      { id: 'notify-offline', label: '离线通知' },
      { id: 'notify-load', label: '负载通知' },
      { id: 'notify-traffic', label: '流量定时报告' },
      { id: 'notify-latency', label: '延迟监测告警' },
      { id: 'notify-general', label: '通用' },
    ],
  },
  { id: 'logs', label: '系统日志', icon: Scroll },
]
const REFRESH_OPTIONS = [10, 30, 60]
const OVERVIEW_INTERVAL_MS = 300000
const pageTitleFor = (page) =>
  page === 'node-detail'
    ? '节点详情'
    : ({
        overview: '仪表盘',
        dashboard: '仪表盘',
        servers: '服务器管理',
        nodes: '服务器管理',
        billing: '成本中心',
        monitoring: '延迟监测',
        latency: '延迟监测',
        route: '回程线路监测',
        network: '延迟监测',
        mtr: '回程线路监测',
        traffic: '流量与报告',
        notifications: '通知与告警',
        alerts: '通知与告警',
        'notify-channel': '通知渠道',
        'notify-offline': '离线通知设置',
        'notify-load': '负载通知',
        'notify-traffic': '流量定时报告',
        'notify-latency': '延迟监测告警',
        'notify-general': '通用设置',
        logs: '系统日志',
        media: '流媒体',
        targets: '检测目标',
        settings: '系统设置',
      }[page] || '仪表盘')

// 基于 document.cookie 保持纯状态流通与主题偏好
function getSavedTheme() {
  try {
    const match = document.cookie.match(/(?:^|; )pb_theme=([^;]*)/)
    if (match) {
      const val = decodeURIComponent(match[1])
      if (val === 'light' || val === 'dark' || val === 'system') return val
    }
  } catch {}
  return 'system'
}

function saveTheme(val) {
  try {
    document.cookie = `pb_theme=${encodeURIComponent(val)}; path=/; max-age=31536000; SameSite=Lax`
  } catch {}
}

const VALID_NAV_PAGES = [
  'overview',
  'dashboard',
  'servers',
  'nodes',
  'billing',
  'monitoring',
  'latency',
  'route',
  'network',
  'mtr',
  'traffic',
  'notifications',
  'alerts',
  'notify-channel',
  'notify-offline',
  'notify-load',
  'notify-traffic',
  'notify-latency',
  'notify-general',
  'logs',
  'media',
  'targets',
  'settings',
  'node-detail',
]

function parseRouteFromHash() {
  try {
    const raw = (window.location.hash || '').replace(/^#\/?/, '').trim()
    if (!raw) return null
    const [pathPart, queryPart] = raw.split('?')
    const cleanPath = (pathPart || '').replace(/\/+$/, '').trim()
    if (!cleanPath || cleanPath === 'overview') {
      return { page: 'overview', nodeUuid: null }
    }
    if (VALID_NAV_PAGES.includes(cleanPath)) {
      const params = new URLSearchParams(queryPart || '')
      return {
        page: cleanPath,
        nodeUuid: params.get('uuid') || params.get('id') || null,
      }
    }
  } catch {}
  return null
}

function getCookieNav() {
  try {
    const match = document.cookie.match(/(?:^|; )pb_nav=([^;]*)/)
    if (match) {
      const val = decodeURIComponent(match[1])
      if (VALID_NAV_PAGES.includes(val)) {
        return { page: val, nodeUuid: null }
      }
    }
  } catch {}
  return null
}

function getInitialNav() {
  const fromHash = parseRouteFromHash()
  if (fromHash) return fromHash
  const fromCookie = getCookieNav()
  if (fromCookie) return fromCookie
  return { page: 'overview', nodeUuid: null }
}

function Topbar({ activeNav, clockText, autoRefresh, refreshInterval, onToggleRefresh, onIntervalChange, lastSyncText, apiState, me, onNavigate, onOpenMobileNav, onSwitchToGuest, onLogout, theme, onThemeChange }) {
  return <header className="topbar">
    <div className="topbar-left">
      <button className="icon-button mobile-menu" aria-label="打开导航菜单" onClick={onOpenMobileNav}><List size={19} /></button>
      <span className="topbar-clock" title="当前时间"><Clock size={14} /><b className="mono">{clockText}</b></span>
      <div className="breadcrumb"><span>监控</span><span className="breadcrumb-slash">/</span><strong>{pageTitleFor(activeNav)}</strong></div>
    </div>
    <div className="top-actions">
      <ThemeToggle theme={theme} onThemeChange={onThemeChange} compact={true} />
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
  const [openMenus, setOpenMenus] = useState(() => {
    const initial = { monitoring: false, notifications: false }
    if (activeNav === 'monitoring' || activeNav === 'latency' || activeNav === 'route' || activeNav === 'network' || activeNav === 'mtr') {
      initial.monitoring = true
    }
    if (activeNav === 'notifications' || activeNav === 'alerts' || (typeof activeNav === 'string' && activeNav.startsWith('notify-'))) {
      initial.notifications = true
    }
    return initial
  })

  useEffect(() => {
    if (activeNav === 'monitoring' || activeNav === 'latency' || activeNav === 'route' || activeNav === 'network' || activeNav === 'mtr') {
      setOpenMenus((prev) => ({ ...prev, monitoring: true }))
    } else if (activeNav === 'notifications' || activeNav === 'alerts' || (typeof activeNav === 'string' && activeNav.startsWith('notify-'))) {
      setOpenMenus((prev) => ({ ...prev, notifications: true }))
    }
  }, [activeNav])

  const handleToggleMenu = (itemId, firstChildId, isChildActive) => {
    setOpenMenus((prev) => {
      const willOpen = !prev[itemId]
      if (willOpen && !isChildActive) {
        onNavigate(firstChildId)
      }
      return { ...prev, [itemId]: willOpen }
    })
  }

  return <>
    <aside className={`sidebar ${collapsed ? 'sidebar-collapsed' : ''} ${mobileOpen ? 'sidebar-open' : ''}`} aria-label="侧边栏">
      <div className="sidebar-head">
        <div className="brand-lockup"><div className="brand-mark"><Pulse size={21} weight="bold" /></div><div className="brand-text"><strong>ProbeWatch</strong><span>纯监控控制台</span></div></div>
        <button className="icon-button collapse-toggle" aria-label={collapsed ? '展开侧边栏' : '折叠侧边栏'} onClick={onToggleCollapse}>{collapsed ? <CaretLineRight size={16} /> : <CaretLineLeft size={16} />}</button>
      </div>
      <div className="workspace-switcher"><div className="workspace-avatar">P</div><div className="workspace-text"><span>工作区</span><strong>{me?.login || me?.name || 'ProbeWatch'}</strong></div></div>
      <nav className="side-nav" aria-label="主导航">
        <span className="nav-section-label">Lite 核心管理</span>
        {navItems.map((item) => {
          const Icon = item.icon
          if (item.children) {
            const isChildActive =
              item.children.some((c) => activeNav === c.id) ||
              activeNav === item.id ||
              (item.id === 'monitoring' && (activeNav === 'network' || activeNav === 'mtr')) ||
              (item.id === 'notifications' && activeNav === 'alerts')
            const isOpen = !!openMenus[item.id]
            return (
              <div key={item.id} className="nav-expandable-wrap">
                <button
                  type="button"
                  title={item.label}
                  className={`nav-item ${isChildActive ? 'nav-item-active' : ''}`}
                  onClick={() => handleToggleMenu(item.id, item.children[0].id, isChildActive)}
                >
                  <Icon size={18} weight={isChildActive ? 'fill' : 'regular'} />
                  <span>{item.label}</span>
                  <span
                    className={`nav-chevron-icon ${isOpen ? 'nav-chevron-open' : ''}`}
                    onClick={(e) => {
                      e.stopPropagation()
                      setOpenMenus((prev) => ({ ...prev, [item.id]: !prev[item.id] }))
                    }}
                    title={isOpen ? '收起子菜单' : '展开子菜单'}
                  >
                    <CaretDown size={13} weight="bold" />
                  </span>
                </button>
                {isOpen && (
                  <div className="nav-sub-list">
                    {item.children.map((child) => (
                      <button
                        key={child.id}
                        type="button"
                        className={`nav-sub-item ${
                          activeNav === child.id ||
                          (child.id === 'latency' && (activeNav === 'monitoring' || activeNav === 'network')) ||
                          (child.id === 'route' && activeNav === 'mtr') ||
                          (child.id === 'notify-channel' && (activeNav === 'notifications' || activeNav === 'alerts'))
                            ? 'nav-sub-item-active'
                            : ''
                        }`}
                        onClick={() => onNavigate(child.id)}
                      >
                        <span>{child.label}</span>
                      </button>
                    ))}
                  </div>
                )}
              </div>
            )
          }
          return (
            <button
              key={item.id}
              type="button"
              title={item.label}
              className={`nav-item ${activeNav === item.id ? 'nav-item-active' : ''}`}
              onClick={() => onNavigate(item.id)}
            >
              <Icon size={item.id === activeNav ? 18 : 18} weight={activeNav === item.id ? 'fill' : 'regular'} />
              <span>{item.label}</span>
            </button>
          )
        })}
        <span className="nav-section-label nav-section-spaced">拓展与配置</span>
        <button type="button" title="流媒体矩阵" className={`nav-item ${activeNav === 'media' ? 'nav-item-active' : ''}`} onClick={() => onNavigate('media')}><CloudArrowDown size={18} /><span>流媒体</span></button>
        <button type="button" title="检测目标" className={`nav-item ${activeNav === 'targets' ? 'nav-item-active' : ''}`} onClick={() => onNavigate('targets')}><SlidersHorizontal size={18} /><span>检测目标</span></button>
        <button type="button" title="系统设置" className={`nav-item ${activeNav === 'settings' ? 'nav-item-active' : ''}`} onClick={() => onNavigate('settings')}><Database size={18} /><span>系统设置</span></button>
        <span className="nav-section-label nav-section-spaced">模式切换</span>
        <button type="button" title="切换至访客只读大屏" className="nav-item nav-item-guest-switch" onClick={onSwitchToGuest}><Eye size={18} /><span>游客大屏</span></button>
      </nav>
      <div className="sidebar-footer">
        <div className="health-chip"><span className={`status-dot status-${apiState.kind === 'ok' ? 'online' : apiState.kind === 'loading' ? 'attention' : 'offline'}`} /><span>{apiState.kind === 'ok' ? 'API 已连接' : apiState.kind === 'auth' ? '需要登录' : apiState.kind === 'loading' ? '正在连接 API' : 'API 不可用'}</span><span className="mono health-version">v0.3.0</span></div>
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
  const initialRouteRef = useRef(getInitialNav())
  const [activeNav, setActiveNav] = useState(() => initialRouteRef.current.page)
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
  const [historyTimeRange, setHistoryTimeRange] = useState('实时')
  const [pingTimeRange, setPingTimeRange] = useState('1小时')
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
  const [theme, setTheme] = useState(getSavedTheme)
  const coreAbortRef = useRef(null)
  const coreRequestRef = useRef(0)
  const overviewAbortRef = useRef(null)
  const counterRef = useRef(new Map())

  // 同步主题至 documentElement 属性与移动端 meta 状态栏
  useEffect(() => {
    saveTheme(theme)
    const applyTheme = () => {
      let resolved = theme
      if (theme === 'system') {
        const prefersDark = window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches
        resolved = prefersDark ? 'dark' : 'light'
      }
      document.documentElement.setAttribute('data-theme', resolved)
      document.documentElement.setAttribute('data-theme-setting', theme)
      const metaTheme = document.querySelector('meta[name="theme-color"]')
      if (metaTheme) {
        metaTheme.setAttribute('content', resolved === 'light' ? '#f6f7f9' : '#08090a')
      }
    }

    applyTheme()

    if (theme === 'system' && window.matchMedia) {
      const mql = window.matchMedia('(prefers-color-scheme: dark)')
      const handler = () => applyTheme()
      mql.addEventListener('change', handler)
      return () => mql.removeEventListener('change', handler)
    }
  }, [theme])

  // 同步 URL hash 与浏览器前进/后退
  useEffect(() => {
    try {
      const route = parseRouteFromHash()
      if (!route) {
        const targetHash = activeNav === 'overview' ? '#/' : `#/${activeNav}`
        window.history.replaceState(null, '', targetHash)
      }
    } catch {}

    const handleHashChange = () => {
      const route = parseRouteFromHash()
      if (route) {
        setActiveNav(route.page)
        document.cookie = `pb_nav=${encodeURIComponent(route.page)}; path=/; max-age=2592000; SameSite=Lax`
        if (route.page === 'node-detail' && route.nodeUuid && data.length > 0) {
          const found = data.find((n) => (n.uuid || n.id) === route.nodeUuid)
          if (found) setDetailNode(found)
        }
      } else {
        setActiveNav('overview')
        document.cookie = `pb_nav=overview; path=/; max-age=2592000; SameSite=Lax`
      }
    }

    window.addEventListener('hashchange', handleHashChange)
    return () => window.removeEventListener('hashchange', handleHashChange)
  }, [data, activeNav])

  // 页面刷新、链接进入或数据轮询更新时，保证 detailNode 始终与 data 中最新上报保持实时同步
  useEffect(() => {
    if (activeNav === 'node-detail' && data.length > 0) {
      const route = parseRouteFromHash()
      const targetUuid = detailNode?.uuid || detailNode?.id || route?.nodeUuid || initialRouteRef.current?.nodeUuid
      if (targetUuid) {
        const latest = data.find((n) => (n.uuid || n.id) === targetUuid)
        if (latest) {
          setDetailNode((prev) => {
            if (!prev) return latest
            if (
              prev.cpu !== latest.cpu ||
              prev.rx !== latest.rx ||
              prev.tx !== latest.tx ||
              prev.lastReportedAt !== latest.lastReportedAt ||
              prev.startedAt !== latest.startedAt ||
              prev.status !== latest.status
            ) {
              return latest
            }
            return prev
          })
        }
      }
    }
  }, [activeNav, data, detailNode?.uuid, detailNode?.id])

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

  const refreshGuest = useCallback(async () => {
    setIsRefreshing(true)
    try {
      const guestStatus = await fetchGuestStatus()
      if (guestStatus) {
        setPublicStatus(guestStatus)
        markSync()
      }
    } finally {
      setIsRefreshing(false)
    }
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

  const routeInfo = parseRouteFromHash()
  const targetDetailUuid = activeNav === 'node-detail' ? (routeInfo?.nodeUuid || initialRouteRef.current?.nodeUuid) : null

  useEffect(() => {
    const uuid = selectedNode?.uuid || selectedNode?.id || (activeNav === 'node-detail' ? (detailNode?.uuid || detailNode?.id || targetDetailUuid) : null)
    if (!uuid) { setHistory([]); return undefined }
    historyAbortRef.current?.abort()
    const controller = new AbortController()
    historyAbortRef.current = controller
    setHistoryLoading(true)

    const rangeParam = historyTimeRange === '实时' ? '15m'
      : historyTimeRange === '4小时' ? '4h'
      : historyTimeRange === '1天' ? '1d'
      : historyTimeRange === '7天' ? '7d'
      : historyTimeRange === '30天' ? '30d'
      : '15m'

    const fetchHistoryFromApi = async () => {
      let response
      try {
        response = await fetch(`/api/nodes/${encodeURIComponent(uuid)}/resource/history?range=${rangeParam}`, { credentials: 'same-origin', signal: controller.signal })
      } catch (e) {
        if (e?.name === 'AbortError') throw e
      }
      if (!response || response.status === 401 || response.status === 404) {
        response = await fetch(`/api/public/nodes/${encodeURIComponent(uuid)}/resource/history?range=${rangeParam}`, { credentials: 'same-origin', signal: controller.signal })
      }
      if (!response.ok) throw new Error(`history:${response.status}`)
      return response.json()
    }

    fetchHistoryFromApi().then((json) => {
      if (!Array.isArray(json)) throw new Error('history:invalid-json')
      const asc = [...json].sort((a, b) => {
        const ta = new Date(a?.reported_at || a?.recorded_at || a?.time || 0).getTime()
        const tb = new Date(b?.reported_at || b?.recorded_at || b?.time || 0).getTime()
        return ta - tb
      })
      return asc.map((item, index, arr) => {
        const resource = item?.resource || {}
        const memoryTotal = numeric(resource.memory_total_bytes)
        const memoryUsed = numeric(resource.memory_used_bytes)
        const diskTotal = numeric(resource.filesystem_total_bytes)
        const diskUsed = numeric(resource.filesystem_used_bytes)
        const swapTotal = numeric(resource.swap_total_bytes)
        const swapUsed = numeric(resource.swap_used_bytes)
        const rx = numeric(resource.network_rx_bytes) || 0
        const tx = numeric(resource.network_tx_bytes) || 0
        const tcp = numeric(resource.tcp_conn_count) || 0
        const udp = numeric(resource.udp_conn_count) || 0
        const proc = numeric(resource.process_count) || 0
        const rawTime = item?.reported_at || item?.recorded_at || item?.time
        let timeIso = null
        if (typeof rawTime === 'string') {
          const d = new Date(rawTime)
          if (!Number.isNaN(d.getTime())) timeIso = d.toISOString()
        } else if (typeof rawTime === 'number' && Number.isFinite(rawTime)) {
          const ms = rawTime > 1e14 ? Math.round(rawTime / 1e6) : rawTime > 1e11 ? rawTime : Math.round(rawTime * 1000)
          timeIso = new Date(ms).toISOString()
        }

        let downRate = 0
        let upRate = 0
        if (index > 0) {
          const prev = arr[index - 1]
          const prevRes = prev?.resource || {}
          const prevRx = numeric(prevRes.network_rx_bytes) || 0
          const prevTx = numeric(prevRes.network_tx_bytes) || 0
          const prevRawTime = prev?.reported_at || prev?.recorded_at || prev?.time
          const curMs = timeIso ? new Date(timeIso).getTime() : 0
          const prevMs = typeof prevRawTime === 'string' ? new Date(prevRawTime).getTime() : (typeof prevRawTime === 'number' ? (prevRawTime > 1e14 ? prevRawTime / 1e6 : prevRawTime * 1000) : 0)
          const dt = (curMs - prevMs) / 1000
          if (dt >= 1 && dt <= 7200) {
            if (rx >= prevRx) downRate = Math.round((rx - prevRx) / dt)
            if (tx >= prevTx) upRate = Math.round((tx - prevTx) / dt)
          }
        }

        return {
          time: timeIso,
          cpu: numeric(resource.cpu_percent) ?? 0,
          mem: memoryUsed !== null && memoryTotal !== null && memoryTotal > 0 ? Math.round((memoryUsed / memoryTotal) * 1000) / 10 : null,
          swap: swapUsed !== null && swapTotal !== null && swapTotal > 0 ? Math.round((swapUsed / swapTotal) * 1000) / 10 : 0,
          disk: diskUsed !== null && diskTotal !== null && diskTotal > 0 ? Math.round((diskUsed / diskTotal) * 1000) / 10 : null,
          rx,
          tx,
          tcp,
          udp,
          conn: tcp + udp,
          proc,
          downRate,
          upRate,
        }
      })
    }).then((points) => { if (!controller.signal.aborted) setHistory(points) }).catch((error) => { if (error?.name !== 'AbortError' && !controller.signal.aborted) setHistory([]) }).finally(() => { if (!controller.signal.aborted) setHistoryLoading(false) })
    return () => controller.abort()
  }, [selectedNode, detailNode, activeNav, targetDetailUuid, historyTimeRange, lastSync])

  const analyticsNode = selectedNode || detailNode
  const analyticsUuid = analyticsNode ? (safeText(analyticsNode.uuid) || safeText(analyticsNode.id)) : (targetDetailUuid || '')
  useEffect(() => {
    if (!analyticsUuid) { setChecksSummary(null); setChecksLoading(false); return undefined }
    checksAbortRef.current?.abort()
    const controller = new AbortController()
    checksAbortRef.current = controller
    setChecksLoading(true)

    const pingParam = pingTimeRange === '1小时' ? '1h'
      : pingTimeRange === '6小时' ? '6h'
      : pingTimeRange === '12小时' ? '12h'
      : pingTimeRange === '1天' ? '1d'
      : '1h'

    const fetchChecksFromApi = async () => {
      let response
      try {
        response = await fetch(`/api/nodes/${encodeURIComponent(analyticsUuid)}/checks/summary?range=${pingParam}`, { credentials: 'same-origin', signal: controller.signal })
      } catch (e) {
        if (e?.name === 'AbortError') throw e
      }
      if (!response || response.status === 401 || response.status === 404) {
        response = await fetch(`/api/public/nodes/${encodeURIComponent(analyticsUuid)}/checks/summary?range=${pingParam}`, { credentials: 'same-origin', signal: controller.signal })
      }
      if (!response.ok) throw new Error(`checks:${response.status}`)
      return response.json()
    }

    fetchChecksFromApi().then((json) => {
      if (!Array.isArray(json)) throw new Error('checks:invalid-json')
      if (!controller.signal.aborted) setChecksSummary(json)
    }).catch((error) => {
      if (error?.name !== 'AbortError' && !controller.signal.aborted) setChecksSummary(null)
    }).finally(() => {
      if (!controller.signal.aborted) setChecksLoading(false)
    })
    return () => controller.abort()
  }, [analyticsUuid, pingTimeRange, lastSync])

  useEffect(() => { setTrafficPeriod('day') }, [analyticsUuid])

  useEffect(() => {
    if (!analyticsUuid) { setTraffic(null); setTrafficLoading(false); return undefined }
    trafficAbortRef.current?.abort()
    const controller = new AbortController()
    trafficAbortRef.current = controller
    setTrafficLoading(true)

    const fetchTrafficFromApi = async () => {
      let response
      try {
        response = await fetch(`/api/nodes/${encodeURIComponent(analyticsUuid)}/traffic?period=${encodeURIComponent(trafficPeriod)}`, { credentials: 'same-origin', signal: controller.signal })
      } catch (e) {
        if (e?.name === 'AbortError') throw e
      }
      if (!response || response.status === 401 || response.status === 404) {
        response = await fetch(`/api/public/nodes/${encodeURIComponent(analyticsUuid)}/traffic?period=${encodeURIComponent(trafficPeriod)}`, { credentials: 'same-origin', signal: controller.signal })
      }
      if (!response.ok) throw new Error(`traffic:${response.status}`)
      return response.json()
    }

    fetchTrafficFromApi().then((json) => {
      if (!json || typeof json !== 'object' || Array.isArray(json)) throw new Error('traffic:invalid-json')
      if (!controller.signal.aborted) setTraffic(json)
    }).catch((error) => {
      if (error?.name !== 'AbortError' && !controller.signal.aborted) setTraffic(null)
    }).finally(() => {
      if (!controller.signal.aborted) setTrafficLoading(false)
    })
    return () => controller.abort()
  }, [analyticsUuid, trafficPeriod, lastSync])

  // 当处于 node-detail 页面时，按 10s 周期自驱刷新节点实时状态
  useEffect(() => {
    if (activeNav !== 'node-detail') return undefined
    const interval = setInterval(() => {
      if (document.visibilityState === 'visible') {
        loadCore(false)
        if (guestPreview || apiState.kind === 'guest') refreshGuest()
      }
    }, 10000)
    return () => clearInterval(interval)
  }, [activeNav, loadCore, refreshGuest, guestPreview, apiState.kind])

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

  const navigate = useCallback((page, extra = {}) => {
    setActiveNav(page)
    setMobileNavOpen(false)
    try {
      let targetHash = `#/${page}`
      const uuid = extra?.uuid || (page === 'node-detail' ? (detailNode?.uuid || detailNode?.id) : null)
      if (page === 'node-detail' && uuid) {
        targetHash = `#/node-detail?uuid=${encodeURIComponent(uuid)}`
      } else if (page === 'overview') {
        targetHash = '#/'
      }
      if (window.location.hash !== targetHash) {
        window.history.pushState(null, '', targetHash)
      }
      document.cookie = `pb_nav=${encodeURIComponent(page)}; path=/; max-age=2592000; SameSite=Lax`
    } catch {}
  }, [detailNode])

  const handleSwitchToGuest = useCallback(() => {
    setGuestPreview(true)
    refreshGuest()
  }, [refreshGuest])

  useEffect(() => {
    if (guestPreview || apiState.kind === 'guest') {
      refreshGuest()
      const interval = activeNav === 'node-detail' ? 3000 : 10000
      const timer = setInterval(() => {
        if (document.visibilityState === 'visible') {
          refreshGuest()
        }
      }, interval)
      return () => clearInterval(timer)
    }
  }, [guestPreview, apiState.kind, refreshGuest, activeNav])

  // Fast 3-second real-time polling when viewing node-detail in admin mode
  useEffect(() => {
    if (activeNav === 'node-detail' && !(guestPreview || apiState.kind === 'guest')) {
      const timer = setInterval(() => {
        if (document.visibilityState === 'visible') {
          loadCore(false)
        }
      }, 3000)
      return () => clearInterval(timer)
    }
  }, [activeNav, guestPreview, apiState.kind, loadCore])

  const refreshAll = () => { loadCore(true); loadOverview() }
  const lastSyncText = lastSync ? formatTimeOfDay(lastSync) : '—'
  const clockText = formatTimeOfDay(clock)

  if (window.location.pathname === '/login/2fa') return <TOTPVerifyPage />
  if (guestPreview || apiState.kind === 'guest') {
    const previewFallback = (guestPreview && data.length > 0) ? {
      nodes: {
        online: data.filter((n) => n.status === 'online').length,
        total: data.length,
        names: data.map((n) => safeText(n.name)).filter(Boolean),
      },
      checks: {
        success_rate: overview?.checks?.success_rate ?? 100,
        avg_latency_ms: overview?.checks?.avg_latency_ms ?? null,
      },
      last_updated_at: new Date().toISOString(),
    } : null

    const effectiveGuestStatus = publicStatus || previewFallback
    const allCustomMeta = getAllNodeCustomMeta()
    const guestNames = safeArray(effectiveGuestStatus?.nodes?.names).map((n) => safeText(n)).filter(Boolean)
    const visibleGuestNames = guestNames.filter((name) => {
      if (guestPreview) return true
      const meta = allCustomMeta[name] || Object.values(allCustomMeta).find((m) => m.customName === name)
      return !meta?.hidden
    })

    const guestNodesList = visibleGuestNames.map((name) => {
      if (data.length > 0) {
        const found = data.find((n) => n.name === name || n.customName === name)
        if (found) return found
      }
      const customKey = allCustomMeta[name] ? name : (Object.keys(allCustomMeta).find((k) => allCustomMeta[k]?.customName === name) || name)
      const custom = allCustomMeta[name] || Object.values(allCustomMeta).find((m) => m.customName === name) || {}
      const meta = detectRegionAndFlag(name, '')
      const displayFlag = custom.customFlag && custom.customFlag !== '自动识别' ? custom.customFlag : (meta.flag || '🌐')
      const displayName = custom.customName || name
      const os = custom.os || 'Ubuntu 24.04 LTS'
      const cpuPercent = custom.cpu !== undefined ? Number(custom.cpu) : 0.1
      const uptimeText = custom.uptime || '24 天'

      return {
        uuid: custom.uuid || customKey || `guest-${encodeURIComponent(name)}`,
        id: custom.uuid || customKey || `guest-${encodeURIComponent(name)}`,
        name: name,
        status: 'online',
        customName: displayName,
        flag: displayFlag,
        os: os,
        arch: custom.arch || 'kvm (x86_64)',
        kernel: custom.kernel || '6.8.0-31-generic',
        uptime: uptimeText.includes('天') ? uptimeText : `${uptimeText} 天`,
        cpu: cpuPercent,
        memUsed: 143339520,
        memTotal: 463994880,
        diskUsed: 1395864371,
        diskTotal: 21045339750,
        swapUsed: 0,
        swapTotal: 2147483648,
        rx: 12133285888,
        tx: 13207024435,
        resource: {
          cpu_name: custom.cpuModel || 'Intel(R) Xeon(R) CPU E5-2680 v3 @ 2.50GHz (1 vCPU)',
          cpu_cores: 1,
          ip: custom.ip || '103.159.207.11',
          process_count: 106,
          tcp_conn_count: 67,
          udp_conn_count: 5,
        },
      }
    })

    const targetUuid = parseRouteFromHash()?.nodeUuid
    const currentDetailNode = detailNode || (targetUuid ? guestNodesList.find((n) => (n.uuid || n.id) === targetUuid) : null) || (activeNav === 'node-detail' && guestNodesList.length > 0 ? guestNodesList[0] : null)

    if (activeNav === 'node-detail' && currentDetailNode) {
      return (
        <main className="guest-shell guest-mjj-shell">
          {guestPreview && (
            <div className="guest-preview-banner">
              <div className="preview-banner-left">
                <Eye size={17} weight="bold" className="text-mint" />
                <span>您当前处于<strong>「游客大屏模式」</strong>（访客将直接看到此只读页面）</span>
              </div>
              <div className="preview-banner-right">
                <button
                  type="button"
                  className="button button-primary btn-sm"
                  onClick={() => {
                    setGuestPreview(false)
                    setActiveNav('node-detail')
                  }}
                  title="返回管理后台"
                >
                  <SquaresFour size={15} weight="bold" />
                  <span>返回管理后台</span>
                </button>
                <button
                  type="button"
                  className="button button-quiet btn-sm text-rose"
                  onClick={performLogout}
                  title="退出当前登录"
                >
                  <SignOut size={15} />
                  <span>退出登录</span>
                </button>
              </div>
            </div>
          )}

          {/* 顶部导航 */}
          <header className="guest-header">
            <div className="brand-lockup">
              <div className="brand-mark">
                <Pulse size={22} weight="bold" />
              </div>
              <div className="brand-text">
                <strong>ProbeWatch</strong>
                <span>全球基础设施服务状态监控大屏</span>
              </div>
            </div>

            <div className="heading-actions">
              <ThemeToggle theme={theme} onThemeChange={setTheme} compact={true} />
              <button
                type="button"
                className="button button-quiet"
                onClick={() => {
                  setSelectedNode(null)
                  setDetailNode(null)
                  navigate('overview')
                }}
                title="返回服务大屏"
              >
                <ArrowLeft size={16} />
                <span>返回大屏</span>
              </button>
            </div>
          </header>

          <div style={{ maxWidth: '1440px', margin: '0 auto', padding: '16px 20px 48px' }}>
            <NodeDetailPage
              node={currentDetailNode}
              nodes={guestNodesList}
              onSelectNode={(node) => {
                setSelectedNode(node)
                setDetailNode(node)
                navigate('node-detail', { uuid: node.uuid || node.id })
              }}
              loading={false}
              history={history}
              historyLoading={historyLoading}
              historyTimeRange={historyTimeRange}
              onHistoryTimeRangeChange={setHistoryTimeRange}
              checksSummary={checksSummary}
              checksLoading={checksLoading}
              pingTimeRange={pingTimeRange}
              onPingTimeRangeChange={setPingTimeRange}
              traffic={traffic}
              trafficLoading={trafficLoading}
              trafficPeriod={trafficPeriod}
              onTrafficPeriodChange={setTrafficPeriod}
              onBack={() => {
                setSelectedNode(null)
                setDetailNode(null)
                navigate('overview')
              }}
              rates={rates}
            />
          </div>
        </main>
      )
    }

    return (
      <GuestView
        status={publicStatus || previewFallback}
        isRefreshing={isRefreshing}
        onRefresh={refreshGuest}
        onLoginSuccess={() => { setGuestPreview(false); refreshAll() }}
        isPreview={guestPreview}
        onExitPreview={() => setGuestPreview(false)}
        onLogout={performLogout}
        theme={theme}
        onThemeChange={setTheme}
        onSelectNode={(node) => {
          const matched = (data.length > 0 && data.find((n) => n.name === node.name || n.customName === node.name || (n.uuid && n.uuid === node.uuid))) || node
          setSelectedNode(matched)
          setDetailNode(matched)
          navigate('node-detail', { uuid: matched.uuid || matched.id })
        }}
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
      onSwitchToGuest={handleSwitchToGuest}
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
        onSwitchToGuest={handleSwitchToGuest}
        onLogout={performLogout}
        theme={theme}
        onThemeChange={setTheme}
      />
      <div className="content-wrap">
        {activeNav === 'overview' || activeNav === 'dashboard' ? (
          <DashboardView
            nodes={data}
            overview={overview}
            alerts={alerts}
            lossRates={lossRates}
            rates={rates}
            onNavigate={navigate}
            onSelectNode={setSelectedNode}
            refreshInterval={refreshInterval}
            onRefresh={refreshAll}
          />
        ) : activeNav === 'classic-overview' ? (
          <OverviewPage
            data={data}
            overview={overview}
            alerts={alerts}
            lossRates={lossRates}
            rates={rates}
            statHistory={statHistory}
            onAck={ackAlert}
            ackingId={ackingId}
            selectedNode={selectedNode}
            onSelectNode={setSelectedNode}
            onNavigate={navigate}
            isRefreshing={isRefreshing}
            onRefresh={refreshAll}
            lastSyncText={lastSyncText}
            apiState={apiState}
          />
        ) : activeNav === 'node-detail' ? (
          <NodeDetailPage
            node={detailNode}
            nodes={data}
            onSelectNode={setSelectedNode}
            loading={!detailNode && (apiState.kind === 'loading' || data.length === 0)}
            history={history}
            historyLoading={historyLoading}
            historyTimeRange={historyTimeRange}
            onHistoryTimeRangeChange={setHistoryTimeRange}
            checksSummary={checksSummary}
            checksLoading={checksLoading}
            pingTimeRange={pingTimeRange}
            onPingTimeRangeChange={setPingTimeRange}
            traffic={traffic}
            trafficLoading={trafficLoading}
            trafficPeriod={trafficPeriod}
            onTrafficPeriodChange={setTrafficPeriod}
            onBack={() => navigate('servers')}
            rates={rates}
          />
        ) : activeNav === 'servers' || activeNav === 'nodes' ? (
          <ServerManageView
            nodes={data}
            rates={rates}
            lossRates={lossRates}
            onSelectNode={setSelectedNode}
          />
        ) : activeNav === 'billing' ? (
          <BillingCenter nodes={data} />
        ) : activeNav === 'monitoring' || activeNav === 'latency' || activeNav === 'route' || activeNav === 'network' || activeNav === 'mtr' ? (
          <MonitoringView
            nodes={data}
            readOnly={true}
            initialTab={activeNav === 'route' || activeNav === 'mtr' ? 'route' : 'latency'}
            onNavigate={navigate}
          />
        ) : activeNav === 'traffic' ? (
          <TrafficReportView nodes={data} onSelectNode={setSelectedNode} />
        ) : activeNav === 'notifications' ||
            activeNav === 'alerts' ||
            activeNav === 'notify-channel' ||
            activeNav === 'notify-offline' ||
            activeNav === 'notify-load' ||
            activeNav === 'notify-traffic' ||
            activeNav === 'notify-latency' ||
            activeNav === 'notify-general' ? (
          <AlertCenterView
            alerts={alerts}
            nodes={data}
            onAck={ackAlert}
            ackingId={ackingId}
            activeSubView={
              activeNav === 'notify-offline'
                ? 'offline'
                : activeNav === 'notify-load'
                ? 'load'
                : activeNav === 'notify-traffic'
                ? 'traffic_report'
                : activeNav === 'notify-latency'
                ? 'latency_alert'
                : activeNav === 'notify-general'
                ? 'general'
                : 'channel'
            }
            onNavigate={navigate}
          />
        ) : activeNav === 'logs' ? (
          <LogsView />
        ) : (
          <SubPage
            page={activeNav}
            data={data}
            alerts={alerts}
            onAck={ackAlert}
            ackingId={ackingId}
            onBack={() => navigate('overview')}
            onSelectNode={setSelectedNode}
            onNavigate={navigate}
            rates={rates}
            lossRates={lossRates}
            refreshInterval={refreshInterval}
            onIntervalChange={setRefreshInterval}
            theme={theme}
            onThemeChange={setTheme}
            overview={overview}
            onRefresh={refreshAll}
          />
        )}
        <footer className="content-footer"><span><span className={`status-dot status-${apiState.kind === 'ok' ? 'online' : 'attention'}`} />{apiState.kind === 'ok' ? '数据来自实时 API · 资源与历史统计独立刷新' : apiState.message}</span><span className="footer-divider" /><span>资源字段缺失时显示 —</span></footer>
      </div>
    </main>
    {selectedNode && <NodeDrawer node={selectedNode} rates={rates} onClose={() => setSelectedNode(null)} onOpenDetails={(node) => { setSelectedNode(null); setDetailNode(node); navigate('node-detail', { uuid: node.uuid || node.id }) }} onNavigate={navigate} />}
  </div>
}
