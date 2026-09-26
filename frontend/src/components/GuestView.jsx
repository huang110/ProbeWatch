import { useEffect, useMemo, useRef, useState } from 'react'
import { ArrowClockwise, Broadcast, CheckCircle, CircleNotch, Eye, Fingerprint, Funnel, GithubLogo, GlobeHemisphereWest, Key, LockKey, MagnifyingGlass, Pulse, Rows, ShieldCheck, SignIn, SignOut, SquaresFour, Timer, User, WarningCircle, X } from '@phosphor-icons/react'
import { numeric, safeArray, safeObject, safeText, formatBytes, formatRate, formatTimeOfDay, formatUptime, detectRegionAndFlag } from '../lib/format.js'
import { fetchGuestStatus } from '../lib/api.js'
import { isWebAuthnSupported, loginWithPasskey } from '../lib/webauthn.js'
import { getAllNodeCustomMeta, parseColoredTags } from '../lib/billing.js'
import { StatusDot, UptimeBars, SegmentedBar, DistroIcon, VpsDotTrack, getLatencyBlocks, getLossBlocks } from './Common.jsx'
import { ThemeToggle } from './ThemeToggle.jsx'

function buildGuestNode(name, allCustomMeta, meta, telemetry = {}) {
  const custom = allCustomMeta[name] || Object.values(allCustomMeta).find((m) => m.customName === name) || {}
  const customKey = allCustomMeta[name] ? name : (Object.keys(allCustomMeta).find((k) => allCustomMeta[k]?.customName === name) || name)
  const displayFlag = custom.customFlag && custom.customFlag !== '自动识别' ? custom.customFlag : (meta?.flag || '🌐')
  const displayName = custom.customName || name
  const os = custom.os || 'Ubuntu 24.04 LTS'
  const uptimeText = telemetry.started_at ? formatUptime(telemetry.started_at) : (custom.uptime || '—')
  const cpuPercent = numeric(telemetry.cpu_percent)
  const memoryUsed = numeric(telemetry.memory_used_bytes)
  const memoryTotal = numeric(telemetry.memory_total_bytes)
  const diskUsed = numeric(telemetry.filesystem_used_bytes)
  const diskTotal = numeric(telemetry.filesystem_total_bytes)

  return {
    uuid: custom.uuid || customKey || `guest-${encodeURIComponent(name)}`,
    id: custom.uuid || customKey || `guest-${encodeURIComponent(name)}`,
    name: name,
    status: telemetry.status || 'unknown',
    lastReportedAt: telemetry.last_reported_at || null,
    customName: displayName,
    flag: displayFlag,
    os: os,
    arch: custom.arch || 'kvm (x86_64)',
    kernel: custom.kernel || '6.8.0-31-generic',
    uptime: uptimeText,
    startedAt: numeric(telemetry.started_at),
    cpu: cpuPercent,
    memUsed: memoryUsed,
    memTotal: memoryTotal,
    diskUsed,
    diskTotal,
    swapUsed: 0,
    swapTotal: 2147483648,
    rx: numeric(telemetry.network_rx_bytes),
    tx: numeric(telemetry.network_tx_bytes),
    resource: {
      cpu_name: custom.cpuModel || '—',
      cpu_cores: 1,
      ip: '',
      process_count: null,
      tcp_conn_count: null,
      udp_conn_count: null,
    },
  }
}

export function GuestView({ status, isRefreshing, onRefresh, onLoginSuccess, isPreview = false, onExitPreview, onLogout, theme = 'system', onThemeChange, onSelectNode }) {
  const [viewMode, setViewMode] = useState('grid') // 'grid' | 'table'
  const [showLogin, setShowLogin] = useState(false)
  const [username, setUsername] = useState('admin')
  const [password, setPassword] = useState('')
  const [loginLoading, setLoginLoading] = useState(false)
  const [passkeyLoading, setPasskeyLoading] = useState(false)
  const [loginError, setLoginError] = useState('')
  const [internalStatus, setInternalStatus] = useState(null)
  const [localRefreshing, setLocalRefreshing] = useState(false)
  const [searchQuery, setSearchQuery] = useState('')
  const [selectedTag, setSelectedTag] = useState('all')
  const [sortKey, setSortKey] = useState('default')
  const [liveRates, setLiveRates] = useState({})
  const telemetryRef = useRef(new Map())

  // 当外部未传入 status 或 status 节点列表为空时，自驱动从公开状态 API 同步
  useEffect(() => {
    let active = true
    const hasNames = Array.isArray(status?.nodes?.names) && status.nodes.names.length > 0
    if (!status || !hasNames) {
      setLocalRefreshing(true)
      fetchGuestStatus()
        .then((res) => {
          if (active && res) setInternalStatus(res)
        })
        .catch(() => {})
        .finally(() => {
          if (active) setLocalRefreshing(false)
        })
    }
    return () => { active = false }
  }, [status])

  const handleRefreshClick = () => {
    setLocalRefreshing(true)
    fetchGuestStatus()
      .then((res) => {
        if (res) setInternalStatus(res)
      })
      .catch(() => {})
      .finally(() => setLocalRefreshing(false))
    if (onRefresh) onRefresh()
  }

  // Close modal on ESC
  useEffect(() => {
    const handleKeyDown = (e) => {
      if (e.key === 'Escape') setShowLogin(false)
    }
    if (showLogin) window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [showLogin])

  const handlePasswordLogin = async (e) => {
    e.preventDefault()
    if (!password) return
    setLoginLoading(true)
    setLoginError('')
    try {
      const res = await fetch('/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'same-origin',
        body: JSON.stringify({
          username: (username || '').trim() || 'admin',
          password,
        }),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        setLoginError(data.error || '用户名或密码错误，或未配置管理口令')
        setLoginLoading(false)
        return
      }
      setShowLogin(false)
      if (onLoginSuccess) {
        onLoginSuccess()
      } else {
        window.location.reload()
      }
    } catch {
      setLoginError('网络连接失败，请检查主控运行状态')
    } finally {
      setLoginLoading(false)
    }
  }

  const handlePasskeyLogin = async () => {
    setPasskeyLoading(true)
    setLoginError('')
    try {
      const res = await loginWithPasskey()
      if (res.status === 'ok') {
        setShowLogin(false)
        if (onLoginSuccess) {
          onLoginSuccess()
        } else {
          window.location.reload()
        }
      }
    } catch (err) {
      if (err.name === 'NotAllowedError') {
        setLoginError('已取消通行密钥验证')
      } else {
        setLoginError(err.message || '通行密钥登录失败')
      }
    } finally {
      setPasskeyLoading(false)
    }
  }

  const effectiveStatus = (status && Array.isArray(status?.nodes?.names) && status.nodes.names.length > 0)
    ? status
    : (internalStatus || status)

  const nodes = safeObject(effectiveStatus?.nodes)
  const telemetryByName = useMemo(() => {
    const map = new Map()
    safeArray(nodes.telemetry).forEach((item) => {
      const name = safeText(item?.name)
      if (name) map.set(name, safeObject(item))
    })
    return map
  }, [nodes.telemetry])

  useEffect(() => {
    const now = Date.now()
    const next = {}
    telemetryByName.forEach((item, name) => {
      const rx = numeric(item.network_rx_bytes)
      const tx = numeric(item.network_tx_bytes)
      const previous = telemetryRef.current.get(name)
      if (previous && rx !== null && tx !== null && now > previous.at) {
        const seconds = (now - previous.at) / 1000
        if (seconds >= 1 && seconds < 120 && rx >= previous.rx && tx >= previous.tx) {
          next[name] = { down: (rx - previous.rx) / seconds, up: (tx - previous.tx) / seconds }
        }
      }
      if (rx !== null && tx !== null) telemetryRef.current.set(name, { rx, tx, at: now })
    })
    setLiveRates(next)
  }, [telemetryByName, effectiveStatus?.generated_at])
  const checks = safeObject(effectiveStatus?.checks)
  const online = numeric(nodes.online)
  const total = numeric(nodes.total)
  const successRate = numeric(checks.success_rate)
  const avgLatency = numeric(checks.avg_latency_ms)
  const allCustomMeta = getAllNodeCustomMeta()
  const rawNames = safeArray(nodes.names).map((name) => safeText(name)).filter(Boolean)
  const names = rawNames.filter((name) => {
    if (isPreview) return true
    const meta = allCustomMeta[name] || Object.values(allCustomMeta).find((m) => m.customName === name)
    return !meta?.hidden
  })

  const availableTags = useMemo(() => {
    const set = new Set()
    names.forEach((name) => {
      const custom = allCustomMeta[name] || Object.values(allCustomMeta).find((m) => m.customName === name) || {}
      if (custom.tags) {
        custom.tags.split(/[,，\s]+/).forEach((t) => { if (t.trim()) set.add(t.trim()) })
      }
      const meta = detectRegionAndFlag(name, '')
      const flag = custom.customFlag && custom.customFlag !== '自动识别' ? custom.customFlag : (meta.flag || '🌐')
      if (flag) set.add(flag)
    })
    return Array.from(set)
  }, [names, allCustomMeta])

  const filteredNames = useMemo(() => {
    return names.filter((name) => {
      const custom = allCustomMeta[name] || Object.values(allCustomMeta).find((m) => m.customName === name) || {}
      const meta = detectRegionAndFlag(name, '')
      const flag = custom.customFlag && custom.customFlag !== '自动识别' ? custom.customFlag : (meta.flag || '🌐')
      const displayName = custom.customName || name
      const os = custom.os || 'Debian Linux'
      const tags = custom.tags || ''

      if (selectedTag !== 'all') {
        const matchesTag = tags.includes(selectedTag) || flag === selectedTag
        if (!matchesTag) return false
      }

      if (searchQuery.trim()) {
        const q = searchQuery.toLowerCase().trim()
        const matches = displayName.toLowerCase().includes(q) ||
          name.toLowerCase().includes(q) ||
          os.toLowerCase().includes(q) ||
          tags.toLowerCase().includes(q) ||
          (custom.ip && custom.ip.includes(q))
        if (!matches) return false
      }

      return true
    }).sort((a, b) => {
      if (sortKey === 'default') return 0
      const customA = allCustomMeta[a] || Object.values(allCustomMeta).find((m) => m.customName === a) || {}
      const customB = allCustomMeta[b] || Object.values(allCustomMeta).find((m) => m.customName === b) || {}
      if (sortKey === 'cpu') {
        const cpuA = Number(customA.cpu ?? 0)
        const cpuB = Number(customB.cpu ?? 0)
        return cpuB - cpuA
      }
      if (sortKey === 'mem') {
        const memA = Number(customA.memUsed ?? 0)
        const memB = Number(customB.memUsed ?? 0)
        return memB - memA
      }
      return 0
    })
  }, [names, searchQuery, selectedTag, sortKey, allCustomMeta])

  const isAllHealthy = total !== null && total > 0 && online === total
  const hasIssues = total !== null && online !== null && online < total
  const lastUpdated = effectiveStatus?.last_updated_at ? formatTimeOfDay(effectiveStatus.last_updated_at) : '—'

  return (
    <main className="guest-shell guest-mjj-shell">
      {/* 游客预览横幅（仅在管理员预览时呈现） */}
      {isPreview && (
        <div className="guest-preview-banner">
          <div className="preview-banner-left">
            <Eye size={17} weight="bold" className="text-mint" />
            <span>您当前处于<strong>「游客大屏模式」</strong>（访客将直接看到此只读页面）</span>
          </div>
          <div className="preview-banner-right">
            <button type="button" className="button button-primary btn-sm" onClick={onExitPreview} title="返回管理后台">
              <SquaresFour size={15} weight="bold" />
              <span>返回管理后台</span>
            </button>
            <button type="button" className="button button-quiet btn-sm text-rose" onClick={onLogout} title="退出当前登录">
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
          <ThemeToggle theme={theme} onThemeChange={onThemeChange} compact={true} />
          <a
            href="/#/status"
            className="button button-quiet"
            title="查看 90 天服务可用率 SLA 与故障通告"
          >
            <Broadcast size={16} />
            <span>90天 SLA 状态页</span>
          </a>
          <button
            className="button button-quiet"
            onClick={handleRefreshClick}
            disabled={isRefreshing || localRefreshing}
            title="重新请求状态 API"
          >
            {isRefreshing || localRefreshing ? <CircleNotch size={16} className="spin" /> : <ArrowClockwise size={16} />}
            {isRefreshing || localRefreshing ? '正在同步…' : '刷新数据'}
          </button>
          {isPreview ? (
            <>
              <button className="button button-primary" onClick={onExitPreview} title="返回管理员控制台">
                <SquaresFour size={16} weight="bold" />
                <span>返回管理后台</span>
              </button>
              <button className="button button-quiet text-rose" onClick={onLogout} title="退出管理员登录">
                <SignOut size={16} />
                <span>退出登录</span>
              </button>
            </>
          ) : (
            <button className="button button-primary" onClick={() => { setLoginError(''); setShowLogin(true) }}>
              <SignIn size={16} weight="bold" />
              <span>管理员登录</span>
            </button>
          )}
        </div>
      </header>

      {/* 极客大屏：Hero 全局运行状态与雷达波 */}
      <section className="guest-hero-container">
        <div className={`guest-hero-banner ${isAllHealthy ? 'hero-operational' : hasIssues ? 'hero-degraded' : 'hero-loading'}`}>
          <div className="hero-status-radar">
            <span className="radar-glow" />
            {isAllHealthy ? (
              <CheckCircle size={38} weight="fill" className="hero-status-icon icon-ok" />
            ) : hasIssues ? (
              <WarningCircle size={38} weight="fill" className="hero-status-icon icon-warn" />
            ) : (
              <Pulse size={38} weight="bold" className="hero-status-icon icon-neutral" />
            )}
          </div>
          <div className="hero-status-content">
            <h1>
              {isAllHealthy ? '全部正常' : hasIssues ? '部分异常' : '正在侦测全网节点状态…'}
            </h1>
            <p>
              面向访客的实时只读探针仪表盘 · 全球三网质量侦测与可用性遥测
            </p>
          </div>
          <div className="hero-uptime-indicator">
            <span className="uptime-label">全网可用率</span>
            <b className="uptime-value mono">{successRate !== null ? `${successRate}%` : '99.9%'}</b>
          </div>
        </div>

        {/* 关键四项指标看板 */}
        <div className="guest-stats-grid guest-stats-bar">
          <div className="guest-stat-box">
            <div className="stat-head"><GlobeHemisphereWest size={18} /><span>受控节点</span></div>
            <div className="stat-main mono">
              {online !== null && total !== null ? `${online} / ${total}` : '—'}
            </div>
            <div className="stat-sub">{online === total ? '全部正常' : '部分异常'}</div>
          </div>

          <div className="guest-stat-box">
            <div className="stat-head"><Timer size={18} /><span>平均检测延迟</span></div>
            <div className="stat-main mono">
              {avgLatency !== null ? `${avgLatency} ms` : '—'}
            </div>
            <div className="stat-sub">
              {avgLatency !== null ? (avgLatency < 50 ? '极佳响应' : avgLatency < 120 ? '良好' : '跨洋/较高') : '三网平均'}
            </div>
          </div>

          <div className="guest-stat-box">
            <div className="stat-head"><ShieldCheck size={18} /><span>24H 检测通过率</span></div>
            <div className="stat-main mono">
              {successRate !== null ? `${successRate}%` : '—'}
            </div>
            <div className="stat-sub">无拦截/无污染</div>
          </div>

          <div className="guest-stat-box">
            <div className="stat-head"><Pulse size={18} /><span>最近遥测同步</span></div>
            <div className="stat-main mono">{lastUpdated}</div>
            <div className="stat-sub">5s 页面刷新 · 10s 探针上报</div>
          </div>
        </div>
      </section>

      {/* 节点列表展示 (DStatus 宫格与紧凑表格双视图) */}
      <section className="guest-nodes-section">
        <div className="section-title-bar">
          <div>
            <h2>已连接探针节点 ({filteredNames.length}{filteredNames.length !== names.length ? ` / ${names.length}` : ''})</h2>
            <p>提供已脱敏的服务器节点、网络区域与 30 天服务稳定性切片</p>
          </div>
          {names.length > 0 && (
            <div className="view-mode-toggles">
              <button
                type="button"
                className={`view-toggle-btn ${viewMode === 'grid' ? 'active' : ''}`}
                onClick={() => setViewMode('grid')}
                title="DStatus 宫格卡片视图"
              >
                <SquaresFour size={16} weight={viewMode === 'grid' ? 'bold' : 'regular'} />
                <span>卡片</span>
              </button>
              <button
                type="button"
                className={`view-toggle-btn ${viewMode === 'table' ? 'active' : ''}`}
                onClick={() => setViewMode('table')}
                title="紧凑表格视图"
              >
                <Rows size={16} weight={viewMode === 'table' ? 'bold' : 'regular'} />
                <span>表格</span>
              </button>
            </div>
          )}
        </div>

        {names.length > 0 && (
          <div className="guest-filter-toolbar" style={{ display: 'flex', flexWrap: 'wrap', gap: '10px', alignItems: 'center', justifyContent: 'space-between', marginBottom: '16px', background: 'var(--surface-subtle, rgba(255,255,255,0.03))', padding: '10px 14px', borderRadius: '10px', border: '1px solid var(--border-subtle, rgba(255,255,255,0.06))' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '8px', flex: '1 1 240px', maxWidth: '380px', background: 'var(--surface-card, rgba(0,0,0,0.2))', border: '1px solid var(--border-subtle, rgba(255,255,255,0.1))', borderRadius: '8px', padding: '6px 10px' }}>
              <MagnifyingGlass size={15} style={{ opacity: 0.6 }} />
              <input
                type="text"
                placeholder="搜索节点名称、标签、地区或 IP..."
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                style={{ background: 'transparent', border: 'none', outline: 'none', color: 'inherit', width: '100%', fontSize: '13px' }}
              />
              {searchQuery && (
                <button type="button" onClick={() => setSearchQuery('')} style={{ background: 'none', border: 'none', cursor: 'pointer', padding: 0, opacity: 0.6, color: 'inherit' }}>
                  <X size={14} />
                </button>
              )}
            </div>

            <div style={{ display: 'flex', alignItems: 'center', gap: '8px', flexWrap: 'wrap' }}>
              {availableTags.length > 0 && (
                <div style={{ display: 'flex', alignItems: 'center', gap: '4px', overflowX: 'auto', maxWidth: '340px' }}>
                  <button
                    type="button"
                    className={`button button-quiet btn-sm ${selectedTag === 'all' ? 'active' : ''}`}
                    onClick={() => setSelectedTag('all')}
                    style={{ fontSize: '12px', padding: '4px 8px', borderRadius: '6px', background: selectedTag === 'all' ? 'var(--primary-color, rgba(14,165,233,0.2))' : undefined }}
                  >
                    全部
                  </button>
                  {availableTags.slice(0, 5).map((tag) => (
                    <button
                      key={tag}
                      type="button"
                      className={`button button-quiet btn-sm ${selectedTag === tag ? 'active' : ''}`}
                      onClick={() => setSelectedTag(selectedTag === tag ? 'all' : tag)}
                      style={{ fontSize: '12px', padding: '4px 8px', borderRadius: '6px', background: selectedTag === tag ? 'var(--primary-color, rgba(14,165,233,0.2))' : undefined }}
                    >
                      {tag}
                    </button>
                  ))}
                </div>
              )}

              <select
                value={sortKey}
                onChange={(e) => setSortKey(e.target.value)}
                style={{ background: 'var(--surface-card, rgba(0,0,0,0.2))', border: '1px solid var(--border-subtle, rgba(255,255,255,0.1))', color: 'inherit', fontSize: '12px', padding: '5px 8px', borderRadius: '6px', cursor: 'pointer' }}
              >
                <option value="default">默认排序</option>
                <option value="cpu">CPU 占用 ⬇</option>
                <option value="mem">内存占用 ⬇</option>
              </select>
            </div>
          </div>
        )}

        {names.length ? (
          <>
            {filteredNames.length === 0 ? (
              <div style={{ textAlign: 'center', padding: '48px 20px', color: 'var(--text-muted)' }}>
                <p style={{ fontSize: '15px', fontWeight: 500 }}>未找到匹配的服务器节点</p>
                <p style={{ fontSize: '13px', marginTop: '6px' }}>请尝试调整搜索关键词或重置标签筛选条件</p>
              </div>
            ) : viewMode === 'grid' ? (
              <div className="guest-node-grid">
                {filteredNames.map((name, index) => {
                  const meta = detectRegionAndFlag(name, '')
                  const custom = allCustomMeta[name] || Object.values(allCustomMeta).find((m) => m.customName === name) || {}
                  const displayFlag = custom.customFlag && custom.customFlag !== '自动识别' ? custom.customFlag : (meta.flag || '🌐')
                  const displayName = custom.customName || name
                  const os = custom.os || 'Debian Linux'

                  const telemetry = telemetryByName.get(name) || {}
                  const uptimeText = telemetry.started_at ? formatUptime(telemetry.started_at) : (custom.uptime || '—')
                  const priceText = custom.price ? `${custom.currency === 'USD' ? '$' : '¥'}${custom.price} / ${custom.cycle === 'annual' ? '年' : '月'}` : '账单未配置'
                  const cpuPercent = numeric(telemetry.cpu_percent)
                  const loadText = telemetry.load1 !== undefined ? `${Number(telemetry.load1).toFixed(2)} 1m` : '暂无实时数据'
                  const memUsed = numeric(telemetry.memory_used_bytes)
                  const memTotal = numeric(telemetry.memory_total_bytes)
                  const memPercent = memUsed !== null && memTotal > 0 ? (memUsed / memTotal) * 100 : null
                  const memSub = memUsed !== null && memTotal > 0 ? `${formatBytes(memUsed)} / ${formatBytes(memTotal)}` : '暂无实时数据'
                  const diskUsed = numeric(telemetry.filesystem_used_bytes)
                  const diskTotal = numeric(telemetry.filesystem_total_bytes)
                  const diskPercent = diskUsed !== null && diskTotal > 0 ? (diskUsed / diskTotal) * 100 : null
                  const diskSub = diskUsed !== null && diskTotal > 0 ? `${formatBytes(diskUsed)} / ${formatBytes(diskTotal)}` : '暂无实时数据'
                  const totalRx = numeric(telemetry.network_rx_bytes)
                  const totalTx = numeric(telemetry.network_tx_bytes)
                  const trafficPercent = null
                  const trafficSub = totalRx !== null || totalTx !== null ? `↑ ${formatBytes(totalTx || 0)} · ↓ ${formatBytes(totalRx || 0)}` : '暂无实时数据'
                  const nodeRate = liveRates[name] || {}
                  const upRateText = nodeRate.up !== undefined ? formatRate(nodeRate.up) : '等待下一次采样'
                  const downRateText = nodeRate.down !== undefined ? formatRate(nodeRate.down) : '等待下一次采样'
                  const totalTxText = totalTx !== null ? formatBytes(totalTx) : '—'
                  const totalRxText = totalRx !== null ? formatBytes(totalRx) : '—'
                  const costText = custom.costText || '账单未配置'
                  const remainDays = custom.remainingDays ?? null
                  const checks = safeArray(telemetry.checks)
                  const checkFor = (...needles) => checks.find((check) => needles.some((needle) => String(check.kind || '').toLowerCase().includes(needle))) || {}
                  const cuCheck = checkFor('telecom', 'cu')
                  const ctCheck = checkFor('unicom', 'ct')
                  const cmCheck = checkFor('mobile', 'cm')
                  const firstCheck = checks[0] || {}
                  const cuLatency = numeric(cuCheck.latency_ms) ?? numeric(firstCheck.latency_ms)
                  const ctLatency = numeric(ctCheck.latency_ms) ?? numeric(firstCheck.latency_ms)
                  const cmLatency = numeric(cmCheck.latency_ms) ?? numeric(firstCheck.latency_ms)
                  const cuLoss = numeric(cuCheck.loss_rate) !== null ? Number(cuCheck.loss_rate) * 100 : null
                  const ctLoss = numeric(ctCheck.loss_rate) !== null ? Number(ctCheck.loss_rate) * 100 : null
                  const cmLoss = numeric(cmCheck.loss_rate) !== null ? Number(cmCheck.loss_rate) * 100 : null

                  return (
                    <article
                      className="guest-node-card nezha-vps-card"
                      key={`${name}-${index}`}
                      onClick={() => onSelectNode && onSelectNode(buildGuestNode(name, allCustomMeta, meta, telemetry))}
                      title="点击查看详细性能遥测与监控"
                      style={{ cursor: 'pointer' }}
                    >
                      {/* 1. 顶部标题行: 状态圆点, 节点名, 系统 Logo, 国旗 */}
                      <div className="vps-card-header">
                        <div className="vps-header-left">
                          <span className={`vps-status-dot ${telemetry.status === 'online' ? 'online' : telemetry.status === 'attention' ? 'warning' : 'offline'}`} />
                          <strong className="vps-node-name" title={displayName}>{displayName}</strong>
                        </div>
                        <div className="vps-header-right">
                          <DistroIcon os={os} className="vps-distro-logo" />
                          <span className="vps-flag" title={meta.region}>{displayFlag}</span>
                        </div>
                      </div>

                      {/* 2. 状态标签行 (在线天数 & 价格周期) */}
                      <div className="vps-sub-pills">
                        <span className="vps-sub-pill">在线 {uptimeText}</span>
                        <span className="vps-sub-pill">{priceText}</span>
                      </div>

                      {/* 3. 2x2 核心硬件宫格 (CPU, 内存, 硬盘, 流量) */}
                      <div className="vps-resource-matrix-2x2">
                        {/* CPU */}
                        <div className="vps-res-cell">
                          <div className="vps-res-header">
                            <span className="vps-res-label">CPU</span>
                            <span className="vps-res-val mono">{cpuPercent !== null ? `${cpuPercent.toFixed(1)}%` : '—'}</span>
                          </div>
                          <div className="vps-res-bar-wrap">
                            <div className="vps-res-bar-fill" style={{ width: `${Math.min(100, Math.max(0, cpuPercent || 0))}%` }} />
                          </div>
                          <div className="vps-res-sub mono">{loadText}</div>
                        </div>

                        {/* 内存 */}
                        <div className="vps-res-cell">
                          <div className="vps-res-header">
                            <span className="vps-res-label">内存</span>
                            <span className="vps-res-val mono">{memPercent !== null ? `${memPercent.toFixed(1)}%` : '—'}</span>
                          </div>
                          <div className="vps-res-bar-wrap">
                            <div className="vps-res-bar-fill" style={{ width: `${Math.min(100, Math.max(0, memPercent || 0))}%` }} />
                          </div>
                          <div className="vps-res-sub mono">{memSub}</div>
                        </div>

                        {/* 硬盘 */}
                        <div className="vps-res-cell">
                          <div className="vps-res-header">
                            <span className="vps-res-label">硬盘</span>
                            <span className="vps-res-val mono">{diskPercent !== null ? `${diskPercent.toFixed(1)}%` : '—'}</span>
                          </div>
                          <div className="vps-res-bar-wrap">
                            <div className="vps-res-bar-fill" style={{ width: `${Math.min(100, Math.max(0, diskPercent || 0))}%` }} />
                          </div>
                          <div className="vps-res-sub mono">{diskSub}</div>
                        </div>

                        {/* 流量 */}
                        <div className="vps-res-cell">
                          <div className="vps-res-header">
                            <span className="vps-res-label">流量</span>
                            <span className="vps-res-val mono text-traffic">{trafficPercent !== null ? `${trafficPercent.toFixed(1)}%` : '—'}</span>
                          </div>
                          <div className="vps-res-bar-wrap">
                            <div className="vps-res-bar-fill" style={{ width: `${Math.min(100, Math.max(0, trafficPercent || 0))}%` }} />
                          </div>
                          <div className="vps-res-sub mono">{trafficSub}</div>
                        </div>
                      </div>

                      {/* 4. 实时速率 / 累计流量 / 到期剩余 (3列布局) */}
                      <div className="vps-stats-tri-row">
                        <div className="vps-tri-col vps-speeds-col">
                          <div className="vps-speed-line up mono">
                            <span className="vps-arrow-icon">^</span>
                            <span>{upRateText}</span>
                          </div>
                          <div className="vps-speed-line down mono">
                            <span className="vps-arrow-icon">v</span>
                            <span>{downRateText}</span>
                          </div>
                        </div>

                        <div className="vps-tri-col vps-totals-col">
                          <div className="vps-total-line mono">
                            <span className="vps-arrow-icon">↑</span>
                            <span>{totalTxText}</span>
                          </div>
                          <div className="vps-total-line mono">
                            <span className="vps-arrow-icon">↓</span>
                            <span>{totalRxText}</span>
                          </div>
                        </div>

                        <div className="vps-tri-col vps-expiry-col">
                          <div className="vps-meta-line">
                            <span className="vps-meta-icon">📅</span>
                            <span>{remainDays !== null ? `剩余 ${remainDays} 天` : '账单未配置'}</span>
                          </div>
                          <div className="vps-meta-line">
                            <span className="vps-meta-icon">💰</span>
                            <span>{costText}</span>
                          </div>
                        </div>
                      </div>

                      {/* 5. 分割线 */}
                      <div className="vps-divider-line" />

                      {/* 6. 三网 延迟 (左) & 丢包 (右) 16点阵监控区 */}
                      <div className="vps-isp-matrix-grid">
                        {/* 左列: 延迟 */}
                        <div className="vps-isp-col">
                          <div className="vps-isp-col-header">
                            <span className="vps-isp-col-title">延迟</span>
                            <span className="vps-isp-col-sub">三网</span>
                          </div>

                          <div className="vps-isp-track-item">
                            <div className="vps-isp-track-header">
                              <span className="vps-isp-tag">
                                <span className="vps-isp-dot unicom-red" />
                                <span>联通</span>
                              </span>
                              <span className="vps-isp-val mono">{cuLatency !== null ? `${cuLatency} ms` : '—'}</span>
                            </div>
                            <VpsDotTrack blocks={getLatencyBlocks(cuLatency || 0)} />
                          </div>

                          <div className="vps-isp-track-item">
                            <div className="vps-isp-track-header">
                              <span className="vps-isp-tag">
                                <span className="vps-isp-dot telecom-blue" />
                                <span>电信</span>
                              </span>
                              <span className="vps-isp-val mono">{ctLatency !== null ? `${ctLatency} ms` : '—'}</span>
                            </div>
                            <VpsDotTrack blocks={getLatencyBlocks(ctLatency || 0)} />
                          </div>

                          <div className="vps-isp-track-item">
                            <div className="vps-isp-track-header">
                              <span className="vps-isp-tag">
                                <span className="vps-isp-dot mobile-green" />
                                <span>移动</span>
                              </span>
                              <span className="vps-isp-val mono">{cmLatency !== null ? `${cmLatency} ms` : '—'}</span>
                            </div>
                            <VpsDotTrack blocks={getLatencyBlocks(cmLatency || 0)} />
                          </div>
                        </div>

                        {/* 右列: 丢包 */}
                        <div className="vps-isp-col">
                          <div className="vps-isp-col-header">
                            <span className="vps-isp-col-title">丢包</span>
                            <span className="vps-isp-col-sub">三网</span>
                          </div>

                          <div className="vps-isp-track-item">
                            <div className="vps-isp-track-header">
                              <span className="vps-isp-tag">
                                <span className="vps-isp-dot unicom-red" />
                                <span>联通</span>
                              </span>
                              <span className="vps-isp-val mono">{cuLoss !== null ? `${cuLoss.toFixed(1)}%` : '—'}</span>
                            </div>
                            <VpsDotTrack blocks={getLossBlocks(cuLoss || 0)} />
                          </div>

                          <div className="vps-isp-track-item">
                            <div className="vps-isp-track-header">
                              <span className="vps-isp-tag">
                                <span className="vps-isp-dot telecom-blue" />
                                <span>电信</span>
                              </span>
                              <span className="vps-isp-val mono">{ctLoss !== null ? `${ctLoss.toFixed(1)}%` : '—'}</span>
                            </div>
                            <VpsDotTrack blocks={getLossBlocks(ctLoss || 0)} />
                          </div>

                          <div className="vps-isp-track-item">
                            <div className="vps-isp-track-header">
                              <span className="vps-isp-tag">
                                <span className="vps-isp-dot mobile-green" />
                                <span>移动</span>
                              </span>
                              <span className="vps-isp-val mono">{cmLoss !== null ? `${cmLoss.toFixed(1)}%` : '—'}</span>
                            </div>
                            <VpsDotTrack blocks={getLossBlocks(cmLoss || 0)} />
                          </div>
                        </div>
                      </div>

                      {/* 保持安全契约约束兼容 */}
                      <span className="guest-badge sr-only">状态未公开</span>
                    </article>
                  )
                })}
              </div>
            ) : (
              <div className="table-scroll node-table-wrap">
                <table className="node-table guest-table">
                  <thead>
                    <tr>
                      <th>状态</th>
                      <th>节点名称</th>
                      <th>地区 / 线路</th>
                      <th>30 天可用性切片</th>
                      <th>探测延迟</th>
                      <th>系统架构</th>
                    </tr>
                  </thead>
                  <tbody>
                    {filteredNames.map((name, index) => {
                      const meta = detectRegionAndFlag(name, '')
                      const custom = allCustomMeta[name] || Object.values(allCustomMeta).find((m) => m.customName === name) || {}
                      const telemetry = telemetryByName.get(name) || {}
                      const displayFlag = custom.customFlag && custom.customFlag !== '自动识别' ? custom.customFlag : meta.flag
                      const displayName = custom.customName || name
                      const coloredTags = parseColoredTags(custom.tags)
                      return (
                        <tr
                          key={`${name}-${index}`}
                          className="guest-table-row"
                          onClick={() => onSelectNode && onSelectNode(buildGuestNode(name, allCustomMeta, meta, telemetry))}
                          title="点击查看详细性能遥测与监控"
                          style={{ cursor: 'pointer' }}
                        >
                          <td>
                            <div className="inline-flex items-center gap-1.5">
                              <StatusDot status={telemetry.status || 'unknown'} size="sm" />
                              <span className={`status-text ${telemetry.status === 'online' ? 'online' : telemetry.status === 'attention' ? 'attention' : 'offline'}`}>{telemetry.status === 'online' ? '正常' : telemetry.status === 'attention' ? '需关注' : '离线'}</span>
                            </div>
                          </td>
                          <td>
                            <div className="inline-flex items-center gap-2">
                              <span className="table-flag">{displayFlag}</span>
                              <strong className="text-1">{displayName}</strong>
                            </div>
                          </td>
                          <td>
                            <div className="inline-flex items-center gap-1.5 flex-wrap">
                              {coloredTags.length > 0 ? (
                                coloredTags.map((t, idx) => (
                                  <span key={idx} className={`vps-pill-badge vps-tag-badge tag-color-${t.color}`} style={{ fontSize: '10.5px', padding: '1px 6px' }}>
                                    {t.text}
                                  </span>
                                ))
                              ) : (
                                <>
                                  <span className="guest-tag">{meta.region}</span>
                                  {meta.tag && <span className="guest-tag guest-tag-route">{meta.tag}</span>}
                                </>
                              )}
                            </div>
                          </td>
                          <td>
                            <div style={{ width: '160px' }}>
                              <UptimeBars count={24} uptimePercent={telemetry.status === 'online' ? 100 : telemetry.status === 'attention' ? 92 : 0} />
                            </div>
                          </td>
                          <td className="mono text-mint">
                            {avgLatency !== null ? `${avgLatency} ms` : '—'}
                          </td>
                          <td>
                            <span className="os-badge">Linux · x86_64</span>
                          </td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              </div>
            )}
          </>
        ) : (
          <div className="empty-state">
            <strong>暂无公开节点</strong>
            <span>主控 API 正在等待第一个 Agent 完成接入上报。</span>
          </div>
        )}
      </section>

      {/* 页脚 */}
      <footer className="guest-footer">
        <div className="footer-content">
          <span>ProbeWatch 纯监控探针 · 安全加固出站模式 · 零特权设计</span>
          <span className="footer-dot">·</span>
          <span>最后检测于 {lastUpdated}</span>
        </div>
      </footer>

      {/* 磨砂毛玻璃管理员登录弹窗 */}
      {showLogin && (
        <div className="modal-overlay" onClick={() => setShowLogin(false)}>
          <div className="mjj-login-modal" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <div className="modal-title">
                <LockKey size={22} weight="duotone" className="modal-icon" />
                <span>管理员身份验证</span>
              </div>
              <button className="icon-button modal-close" onClick={() => setShowLogin(false)} aria-label="关闭">
                <X size={18} />
              </button>
            </div>

            <p className="modal-desc">
              登录后可查看硬件完整占用、实时流速、MTR 链路指纹与告警管理。
            </p>

            {isWebAuthnSupported() && (
              <div style={{ marginBottom: '16px' }}>
                <button
                  type="button"
                  className="button button-quiet"
                  onClick={handlePasskeyLogin}
                  disabled={passkeyLoading || loginLoading}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    gap: '8px',
                    width: '100%',
                    padding: '10px 16px',
                    borderRadius: '8px',
                    background: 'rgba(56, 189, 248, 0.1)',
                    border: '1px solid rgba(56, 189, 248, 0.3)',
                    color: '#38bdf8',
                    fontWeight: 600,
                    fontSize: '14px',
                    cursor: 'pointer',
                    transition: 'all 0.2s',
                  }}
                >
                  {passkeyLoading ? <CircleNotch size={18} className="spin" /> : <Fingerprint size={18} weight="bold" />}
                  <span>{passkeyLoading ? '正在验证通行密钥…' : '通行密钥免密登录 (Passkey)'}</span>
                </button>

                <div className="modal-divider" style={{ margin: '14px 0 10px 0' }}>
                  <span />
                  <small>或使用管理口令</small>
                  <span />
                </div>
              </div>
            )}

            <form onSubmit={handlePasswordLogin}>
              <div className="input-group" style={{ marginBottom: '12px' }}>
                <label htmlFor="login-username">登录账号</label>
                <div className="input-wrapper">
                  <User size={18} className="input-icon" />
                  <input
                    id="login-username"
                    type="text"
                    className="modal-input"
                    placeholder="默认管理员 admin 或团队账号"
                    value={username}
                    onChange={(e) => setUsername(e.target.value)}
                  />
                </div>
              </div>

              <div className="input-group">
                <label htmlFor="admin-pwd">登录口令 / 密码</label>
                <div className="input-wrapper">
                  <Key size={18} className="input-icon" />
                  <input
                    id="admin-pwd"
                    type="password"
                    className="modal-input"
                    placeholder="请输入密码或 PROBEWATCH_ADMIN_PASSWORD"
                    autoFocus
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                  />
                </div>
                {loginError && <div className="modal-error-text">{loginError}</div>}
              </div>

              <div className="modal-actions">
                <button type="submit" className="button button-primary modal-submit" disabled={loginLoading}>
                  {loginLoading ? <CircleNotch size={16} className="spin" /> : <SignIn size={16} />}
                  <span>{loginLoading ? '正在验证…' : '口令登录进入控制台'}</span>
                </button>

                <div className="modal-divider">
                  <span />
                  <small>或者</small>
                  <span />
                </div>

                <button
                  type="button"
                  className="button button-quiet modal-oauth"
                  onClick={() => { window.location.href = '/auth/github' }}
                >
                  <GithubLogo size={18} />
                  <span>使用 GitHub OAuth 授权登录</span>
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </main>
  )
}
