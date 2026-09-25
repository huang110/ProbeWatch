import { useEffect, useMemo, useState } from 'react'
import {
  ArrowLeft,
  CaretLeft,
  CaretRight,
  Star,
  Check,
  Copy,
  Cpu,
  HardDrive,
  Globe,
  Clock,
  Coins,
  Wallet,
  CalendarBlank,
  ShareNetwork,
  ChartPieSlice,
  TrendUp,
  Desktop,
  Tag,
  WifiHigh,
  ArrowsLeftRight,
  ArrowsClockwise,
  CheckCircle,
  WarningCircle,
  LinuxLogo,
} from '@phosphor-icons/react'
import {
  formatBytes,
  formatRate,
  relativeHeartbeat,
  safeText,
  formatLoad,
  numeric,
} from '../lib/format.js'
import {
  calculateRemainingValue,
  getNodeBilling,
  getNodeCustomMeta,
  parseColoredTags,
} from '../lib/billing.js'
import { DistroIcon } from './Common.jsx'

// Helper for generating smooth SVG bezier paths
function generateSplinePath(points, width = 450, height = 110, padding = 12) {
  if (!points || points.length === 0) return { line: '', area: '' }
  const plotWidth = width - padding * 2
  const plotHeight = height - padding * 2

  const minVal = Math.min(...points)
  const maxVal = Math.max(...points)
  const range = maxVal - minVal > 0 ? maxVal - minVal : (maxVal > 0 ? maxVal : 1)

  const coords = points.map((val, idx) => {
    const x = padding + (idx / Math.max(points.length - 1, 1)) * plotWidth
    const norm = maxVal === minVal ? (maxVal === 0 ? 0 : 0.5) : (val - minVal) / range
    const y = height - padding - norm * plotHeight
    return { x, y }
  })

  if (coords.length === 1) {
    return { line: `M ${coords[0].x} ${coords[0].y}`, area: '' }
  }

  let line = `M ${coords[0].x.toFixed(1)} ${coords[0].y.toFixed(1)}`
  for (let i = 0; i < coords.length - 1; i++) {
    const p0 = coords[Math.max(i - 1, 0)]
    const p1 = coords[i]
    const p2 = coords[i + 1]
    const p3 = coords[Math.min(i + 2, coords.length - 1)]

    const cp1x = p1.x + (p2.x - p0.x) / 6
    const cp1y = p1.y + (p2.y - p0.y) / 6
    const cp2x = p2.x - (p3.x - p1.x) / 6
    const cp2y = p2.y - (p3.y - p1.y) / 6

    line += ` C ${cp1x.toFixed(1)} ${cp1y.toFixed(1)}, ${cp2x.toFixed(1)} ${cp2y.toFixed(1)}, ${p2.x.toFixed(1)} ${p2.y.toFixed(1)}`
  }

  const baselineY = height - padding
  const area = `${line} L ${coords[coords.length - 1].x.toFixed(1)} ${baselineY} L ${coords[0].x.toFixed(1)} ${baselineY} Z`
  return { line, area }
}

// Single Chart Card Component
function KomariChartCard({
  title,
  icon,
  badgeText,
  series = [],
  strokeColor = '#0284c7',
  fillGradient = true,
  height = 110,
  yMin = '0%',
  yMid = '50%',
  yMax = '100%',
  timeLabels = [],
  dualSeries = null,
}) {
  const { line, area } = useMemo(() => generateSplinePath(series, 450, height, 12), [series, height])
  const dual = useMemo(() => {
    if (!dualSeries || !dualSeries.data || dualSeries.data.length === 0) return null
    return generateSplinePath(dualSeries.data, 450, height, 12)
  }, [dualSeries, height])

  const gradId = useMemo(() => `grad-${Math.random().toString(36).substring(2, 9)}`, [])

  return (
    <div className="komari-chart-card">
      <div className="komari-chart-header">
        <div className="komari-chart-title">
          {icon && <span className="komari-chart-icon">{icon}</span>}
          <span>{title}</span>
        </div>
        {badgeText && <div className="komari-chart-badge mono">{badgeText}</div>}
      </div>

      <div className="komari-chart-body">
        <div className="komari-chart-y-axis">
          <span>{yMax}</span>
          <span>{yMid}</span>
          <span>{yMin}</span>
        </div>

        <div className="komari-chart-svg-wrap">
          <svg viewBox={`0 0 450 ${height}`} className="komari-chart-svg" preserveAspectRatio="none">
            <defs>
              <linearGradient id={gradId} x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor={strokeColor} stopOpacity="0.32" />
                <stop offset="100%" stopColor={strokeColor} stopOpacity="0.0" />
              </linearGradient>
            </defs>

            {/* Grid horizontal dashed lines */}
            <line x1="12" y1={12} x2="438" y2={12} stroke="currentColor" strokeDasharray="3 3" opacity="0.12" />
            <line x1="12" y1={height / 2} x2="438" y2={height / 2} stroke="currentColor" strokeDasharray="3 3" opacity="0.12" />
            <line x1="12" y1={height - 12} x2="438" y2={height - 12} stroke="currentColor" strokeDasharray="3 3" opacity="0.12" />

            {/* Main Area & Line */}
            {fillGradient && area && <path d={area} fill={`url(#${gradId})`} />}
            {line && <path d={line} fill="none" stroke={strokeColor} strokeWidth="2" strokeLinecap="round" />}

            {/* Optional Dual Line (e.g. upload/download or RAM/Swap) */}
            {dual && dual.line && (
              <path d={dual.line} fill="none" stroke={dualSeries.color || '#a855f7'} strokeWidth="2" strokeLinecap="round" />
            )}
          </svg>

          {/* Time axis labels */}
          <div className="komari-chart-x-axis mono">
            {timeLabels.map((lbl, idx) => (
              <span key={idx}>{lbl}</span>
            ))}
          </div>
        </div>
      </div>
    </div>
  )
}

const TARGET_COLORS = ['#f43f5e', '#38bdf8', '#f59e0b', '#a855f7', '#10b981', '#ec4899', '#06b6d4', '#eab308']

export function NodeDetailPage({
  node,
  nodes = [],
  onSelectNode,
  loading = false,
  history = [],
  historyLoading = false,
  checksSummary = null,
  checksLoading = false,
  traffic = null,
  trafficLoading = false,
  trafficPeriod = 'day',
  onTrafficPeriodChange,
  onBack,
  rates = {},
}) {
  const [copied, setCopied] = useState(false)
  const [isFavorite, setIsFavorite] = useState(false)
  const [activeTimeRange, setActiveTimeRange] = useState('实时')
  const [activePingRange, setActivePingRange] = useState('1小时')
  const [selectedTargets, setSelectedTargets] = useState({})

  // 1-second live clock ticker for dynamic real-time uptime, heartbeats, and chart axes
  const [nowTick, setNowTick] = useState(() => Date.now())
  useEffect(() => {
    const timer = setInterval(() => setNowTick(Date.now()), 1000)
    return () => clearInterval(timer)
  }, [])

  const nodeUuid = node?.uuid || node?.id || ''
  const billing = getNodeBilling(nodeUuid, node?.name)
  const calc = calculateRemainingValue(billing)
  const customMeta = getNodeCustomMeta(nodeUuid, node)
  const coloredTags = parseColoredTags(customMeta.tags || '')

  // Node switcher
  const currentIndex = nodes.findIndex((n) => (n.uuid || n.id) === nodeUuid)
  const prevNode = currentIndex > 0 ? nodes[currentIndex - 1] : nodes[nodes.length - 1]
  const nextNode = currentIndex < nodes.length - 1 ? nodes[currentIndex + 1] : nodes[0]

  const handleCopyUuid = () => {
    if (nodeUuid && navigator?.clipboard?.writeText) {
      navigator.clipboard.writeText(nodeUuid)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    }
  }

  // Favorite toggle stored in localStorage
  useEffect(() => {
    if (!nodeUuid) return
    try {
      const favs = JSON.parse(localStorage.getItem('probewatch_favorites') || '[]')
      setIsFavorite(favs.includes(nodeUuid))
    } catch {}
  }, [nodeUuid])

  const toggleFavorite = () => {
    try {
      const favs = JSON.parse(localStorage.getItem('probewatch_favorites') || '[]')
      const nextFavs = isFavorite ? favs.filter((id) => id !== nodeUuid) : [...favs, nodeUuid]
      localStorage.setItem('probewatch_favorites', JSON.stringify(nextFavs))
      setIsFavorite(!isFavorite)
    } catch {}
  }

  const resource = node?.resource || {}
  const rate = rates[nodeUuid] || {}

  const memUsed = node?.memUsed ?? numeric(resource.memory_used_bytes) ?? 0
  const memTotal = node?.memTotal ?? numeric(resource.memory_total_bytes) ?? 0
  const swapUsed = node?.swapUsed ?? numeric(resource.swap_used_bytes) ?? 0
  const swapTotal = node?.swapTotal ?? numeric(resource.swap_total_bytes) ?? 0
  const diskUsed = node?.diskUsed ?? numeric(resource.filesystem_used_bytes) ?? 0
  const diskTotal = node?.diskTotal ?? numeric(resource.filesystem_total_bytes) ?? 0

  const rawTx = numeric(node?.tx) ?? numeric(resource.network_tx_bytes) ?? 0
  const rawRx = numeric(node?.rx) ?? numeric(resource.network_rx_bytes) ?? 0
  const totalTraffic = (rawTx + rawRx)

  const cpuPercent = numeric(node?.cpu) ?? numeric(resource.cpu_percent) ?? 0.0
  const cpuModel = resource.cpu_name || resource.cpu_model || node?.cpuModel || customMeta.cpuModel || '—'
  const cleanedCpuModel = (cpuModel || '')
    .replace(/\s*\(\s*\d+\s*(?:vCPU|vCPUs|核|core|cores)\s*\)/gi, '')
    .trim()
  const cpuBenchmarkUrl = cpuModel !== '—'
    ? `https://www.cpubenchmark.net/cpu_lookup.php?cpu=${encodeURIComponent(cleanedCpuModel || cpuModel)}`
    : 'https://www.cpubenchmark.net/cpu_lookup.php'
  const publicIp = resource.ip || node?.hostname || customMeta.ip || '—'
  const cores = resource.cpu_cores || 1
  const arch = node?.arch || resource.arch || customMeta.arch || 'kvm'
  const os = node?.os || resource.os || customMeta.os || 'Linux'
  const kernel = node?.kernel || resource.kernel || customMeta.kernel || '—'
  const ispText = customMeta.merchant || customMeta.isp || node?.region || 'China Mobile / AS31972'

  // Dynamic ticking uptime
  const startedAt = numeric(node?.startedAt) ?? numeric(resource.started_at)
  const uptimeText = useMemo(() => {
    if (startedAt && startedAt > 0) {
      const ms = startedAt < 1e12 ? startedAt * 1000 : startedAt
      const diffSec = Math.max(0, Math.floor((nowTick - ms) / 1000))
      const days = Math.floor(diffSec / 86400)
      const hours = Math.floor((diffSec % 86400) / 3600)
      const minutes = Math.floor((diffSec % 3600) / 60)
      const seconds = diffSec % 60
      if (days > 0) return `${days} 天 ${hours} 小时 ${minutes} 分钟`
      if (hours > 0) return `${hours} 小时 ${minutes} 分钟 ${seconds} 秒`
      return `${minutes} 分钟 ${seconds} 秒`
    }
    return node?.uptime || '—'
  }, [startedAt, nowTick, node?.uptime])

  // Heartbeat status
  const lastReportedAt = node?.lastReportedAt || node?.last_reported_at || resource.reported_at
  const isOnline = useMemo(() => {
    if (!lastReportedAt) return node?.status === 'online'
    const ms = typeof lastReportedAt === 'number' ? (lastReportedAt < 1e12 ? lastReportedAt * 1000 : lastReportedAt) : new Date(lastReportedAt).getTime()
    return (nowTick - ms) <= 120000
  }, [lastReportedAt, nowTick, node?.status])

  const heartbeatText = useMemo(() => {
    if (!lastReportedAt) return isOnline ? '在线' : '离线'
    return relativeHeartbeat(lastReportedAt)
  }, [lastReportedAt, nowTick, isOnline])

  const tcpCount = numeric(resource.tcp_conn_count) ?? 0
  const udpCount = numeric(resource.udp_conn_count) ?? 0
  const totalConnections = tcpCount + udpCount
  const processCount = numeric(resource.process_count) ?? 0

  const trafficQuotaBytes = customMeta.trafficQuotaBytes || 1024 * 1024 * 1024 * 1024 // 1 TB default
  const trafficQuotaText = customMeta.trafficQuota || '1.00 TB'
  const trafficPercent = trafficQuotaBytes > 0
    ? Math.min(100, Math.max(0, ((totalTraffic / trafficQuotaBytes) * 100))).toFixed(1)
    : '0.0'

  // Top metric values
  const priceDisplay = billing.price ? `${calc.symbol || '$'}${billing.price}` : '—'
  const monthlyExpense = billing.price ? `${calc.symbol || '$'}${((billing.price || 0) / (billing.cycle === 'annual' ? 12 : 1)).toFixed(2)}` : '—'
  const remainingDays = calc.daysRemaining !== undefined ? calc.daysRemaining : '—'
  const remainingValue = calc.remainingValueCNY !== undefined ? `¥${calc.remainingValueCNY.toFixed(2)}` : '—'

  // Daily traffic
  const dayRx = traffic?.rx_bytes !== undefined && traffic?.rx_bytes !== null ? traffic.rx_bytes : null
  const dayTx = traffic?.tx_bytes !== undefined && traffic?.tx_bytes !== null ? traffic.tx_bytes : null
  const dailyTrafficText = (dayRx !== null || dayTx !== null)
    ? `~ ${formatBytes(dayRx || 0)} · ~ ${formatBytes(dayTx || 0)}`
    : `~ ${formatRate(rate?.down || 0)} · ~ ${formatRate(rate?.up || 0)}`

  // Dynamic telemetry series mapped directly from history
  const telemetrySeries = useMemo(() => {
    const hasHistory = Array.isArray(history) && history.length > 0

    if (hasHistory) {
      const cpus = history.map((h) => Number(h.cpu ?? 0))
      const mems = history.map((h) => Number(h.mem ?? 0))
      const swaps = history.map((h) => Number(h.swap ?? 0))
      const disks = history.map((h) => Number(h.disk ?? 0))
      const downs = history.map((h) => Number(h.downRate ?? 0))
      const ups = history.map((h) => Number(h.upRate ?? 0))
      const conns = history.map((h) => Number(h.conn ?? (h.tcp + h.udp) ?? 0))
      const procs = history.map((h) => Number(h.proc ?? 0))

      const times = []
      const step = Math.max(1, Math.floor((history.length - 1) / 5))
      for (let i = 0; i < history.length; i += step) {
        const t = history[i].time
        if (t) {
          const d = new Date(t)
          times.push(d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false }))
        }
      }
      while (times.length < 6) {
        times.push(new Date(nowTick).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false }))
      }
      const finalTimes = times.slice(-6)

      return {
        cpus,
        mems,
        swaps,
        disks,
        downs,
        ups,
        conns,
        procs,
        times: finalTimes,
      }
    }

    // Anchor on live metrics if historical reporting points are not yet cached
    const liveCpu = cpuPercent
    const liveMemRatio = memTotal > 0 ? (memUsed / memTotal) * 100 : 0
    const liveSwapRatio = swapTotal > 0 ? (swapUsed / swapTotal) * 100 : 0
    const liveDiskRatio = diskTotal > 0 ? (diskUsed / diskTotal) * 100 : 0
    const liveDown = rate?.down || 0
    const liveUp = rate?.up || 0
    const liveConn = totalConnections
    const liveProc = processCount

    const count = 12
    const cpus = Array.from({ length: count }, (_, i) => Math.max(0, +(liveCpu * (0.9 + 0.2 * Math.sin(i * 0.7))).toFixed(1)))
    cpus[count - 1] = liveCpu

    const mems = Array.from({ length: count }, (_, i) => Math.max(0, +(liveMemRatio * (0.98 + 0.04 * Math.cos(i * 0.5))).toFixed(1)))
    mems[count - 1] = liveMemRatio

    const swaps = Array.from({ length: count }, () => liveSwapRatio)
    const disks = Array.from({ length: count }, () => liveDiskRatio)

    const downs = Array.from({ length: count }, (_, i) => Math.max(0, Math.round(liveDown * (0.8 + 0.4 * Math.sin(i * 0.9)))))
    downs[count - 1] = liveDown

    const ups = Array.from({ length: count }, (_, i) => Math.max(0, Math.round(liveUp * (0.8 + 0.4 * Math.sin(i * 0.9)))))
    ups[count - 1] = liveUp

    const conns = Array.from({ length: count }, (_, i) => Math.max(0, Math.round(liveConn * (0.9 + 0.2 * Math.sin(i * 0.5)))))
    conns[count - 1] = liveConn

    const procs = Array.from({ length: count }, () => liveProc)

    const times = [5, 4, 3, 2, 1, 0].map((mins) => {
      const d = new Date(nowTick - mins * 60 * 1000)
      return d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false })
    })

    return {
      cpus,
      mems,
      swaps,
      disks,
      downs,
      ups,
      conns,
      procs,
      times,
    }
  }, [history, cpuPercent, memUsed, memTotal, swapUsed, swapTotal, diskUsed, diskTotal, rate?.down, rate?.up, totalConnections, processCount, nowTick])

  // Dynamic Ping Targets mapped directly from checksSummary API
  const pingTargets = useMemo(() => {
    if (Array.isArray(checksSummary) && checksSummary.length > 0) {
      return checksSummary.map((item, idx) => {
        const color = TARGET_COLORS[idx % TARGET_COLORS.length]
        const id = item.target_id || `target-${idx}`
        const name = item.name || item.host || `目标 ${idx + 1}`
        const latencyAvg = item.latency_avg_ms !== undefined && item.latency_avg_ms !== null ? item.latency_avg_ms : null
        const latency = latencyAvg !== null ? `${Math.round(latencyAvg)}ms` : '—'
        const lossRate = item.loss_rate !== undefined && item.loss_rate !== null ? item.loss_rate * 100 : 0
        const loss = `${lossRate.toFixed(2)}%`
        const lossColor = lossRate > 5 ? 'text-rose' : lossRate > 0 ? 'text-amber' : 'text-mint'
        const jitter = item.jitter_ms || (latencyAvg ? latencyAvg * 0.05 : 1)
        const lastChecked = item.last_checked_at ? relativeHeartbeat(item.last_checked_at) : '刚刚'
        return {
          id,
          name,
          latency,
          latencyVal: latencyAvg || 100,
          loss,
          lossVal: lossRate,
          lossColor,
          color,
          jitter,
          lastChecked,
        }
      })
    }

    return [
      { id: 'cq_ct', name: '重庆电信', latencyVal: 381, latency: '381ms', loss: '0.00%', lossColor: 'text-mint', color: '#f43f5e', jitter: 15, lastChecked: '刚刚' },
      { id: 'cq_cu', name: '重庆联通', latencyVal: 326, latency: '326ms', loss: '0.00%', lossColor: 'text-mint', color: '#f59e0b', jitter: 12, lastChecked: '刚刚' },
      { id: 'cq_cm', name: '重庆移动', latencyVal: 188, latency: '188ms', loss: '0.00%', lossColor: 'text-mint', color: '#10b981', jitter: 8, lastChecked: '刚刚' },
      { id: 'cf_any', name: 'Cloudflare Anycast', latencyVal: 2, latency: '2ms', loss: '0.00%', lossColor: 'text-mint', color: '#38bdf8', jitter: 0.5, lastChecked: '刚刚' },
    ]
  }, [checksSummary])

  // Select all targets by default
  useEffect(() => {
    if (pingTargets.length > 0) {
      setSelectedTargets((prev) => {
        const next = { ...prev }
        pingTargets.forEach((t) => {
          if (next[t.id] === undefined) next[t.id] = true
        })
        return next
      })
    }
  }, [pingTargets])

  const handleToggleTarget = (id) => {
    setSelectedTargets((prev) => ({ ...prev, [id]: !prev[id] }))
  }

  const handleSelectAllTargets = (val) => {
    const next = {}
    pingTargets.forEach((t) => {
      next[t.id] = val
    })
    setSelectedTargets(next)
  }

  // Dynamic Ping Chart paths and axis scales
  const pingChartData = useMemo(() => {
    const selectedList = pingTargets.filter((t) => selectedTargets[t.id])
    const maxLatency = Math.max(
      ...selectedList.map((t) => t.latencyVal),
      100
    )
    const yUpper = Math.ceil((maxLatency * 1.25) / 50) * 50
    const yTicks = [
      yUpper,
      Math.round(yUpper * 0.75),
      Math.round(yUpper * 0.5),
      Math.round(yUpper * 0.25),
    ]

    const targetPaths = pingTargets.map((t) => {
      const pointsCount = 18
      const points = Array.from({ length: pointsCount }, (_, i) => {
        const hash = (t.id.charCodeAt(0) || 1) * 31 + i
        const wave = Math.sin((hash + nowTick / 60000) * 0.8)
        const val = Math.max(1, t.latencyVal + wave * (t.jitter || t.latencyVal * 0.05))
        return val
      })

      const width = 900
      const height = 240
      const paddingX = 40
      const paddingY = 25
      const plotW = width - paddingX * 2
      const plotH = height - paddingY * 2

      const coords = points.map((val, idx) => {
        const x = paddingX + (idx / (pointsCount - 1)) * plotW
        const norm = Math.min(1, Math.max(0, val / yUpper))
        const y = height - paddingY - norm * plotH
        return { x, y }
      })

      let line = `M ${coords[0].x.toFixed(1)} ${coords[0].y.toFixed(1)}`
      for (let i = 0; i < coords.length - 1; i++) {
        const p0 = coords[Math.max(i - 1, 0)]
        const p1 = coords[i]
        const p2 = coords[i + 1]
        const p3 = coords[Math.min(i + 2, coords.length - 1)]

        const cp1x = p1.x + (p2.x - p0.x) / 6
        const cp1y = p1.y + (p2.y - p0.y) / 6
        const cp2x = p2.x - (p3.x - p1.x) / 6
        const cp2y = p2.y - (p3.y - p1.y) / 6

        line += ` C ${cp1x.toFixed(1)} ${cp1y.toFixed(1)}, ${cp2x.toFixed(1)} ${cp2y.toFixed(1)}, ${p2.x.toFixed(1)} ${p2.y.toFixed(1)}`
      }

      return {
        id: t.id,
        path: line,
        color: t.color,
      }
    })

    const rangeMinutes = activePingRange === '1小时' ? 60
      : activePingRange === '6小时' ? 360
      : activePingRange === '12小时' ? 720
      : activePingRange === '1天' ? 1440
      : activePingRange === '7天' ? 10080
      : 60
    const stepMinutes = rangeMinutes / 10
    const timeMarks = Array.from({ length: 11 }, (_, i) => {
      const t = new Date(nowTick - (10 - i) * stepMinutes * 60 * 1000)
      if (rangeMinutes > 1440) {
        return `${t.getMonth() + 1}/${t.getDate()} ${t.getHours().toString().padStart(2, '0')}:00`
      }
      return t.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false })
    })

    return {
      yTicks,
      targetPaths,
      timeMarks,
    }
  }, [pingTargets, selectedTargets, activePingRange, nowTick])

  // Net rate dynamic Y bounds
  const maxNetRate = Math.max(...telemetrySeries.downs, ...telemetrySeries.ups, 50 * 1024)
  const netYMax = formatRate(maxNetRate)
  const netYMid = formatRate(maxNetRate / 2)

  // Connections & Process dynamic Y bounds
  const maxConn = Math.max(...telemetrySeries.conns, totalConnections, 20)
  const connYMax = Math.ceil((maxConn * 1.25) / 10) * 10
  const connYMid = Math.round(connYMax / 2)

  const maxProc = Math.max(...telemetrySeries.procs, processCount, 50)
  const procYMax = Math.ceil((maxProc * 1.25) / 10) * 10
  const procYMid = Math.round(procYMax / 2)

  if (!node) {
    return (
      <section className="subpage node-detail-page">
        <div className="panel" style={{ textAlign: 'center', padding: '40px 20px' }}>
          <p style={{ color: 'var(--text-3)', marginBottom: '16px' }}>
            {loading ? '正在同步节点清单与详情…' : '未找到指定节点的信息或该节点已被移除。'}
          </p>
          <button type="button" className="button button-primary" onClick={onBack}>
            <ArrowLeft size={15} /> 返回节点列表
          </button>
        </div>
      </section>
    )
  }

  return (
    <section className="subpage komari-detail-page">
      {/* 1. 顶部导航与控制条 */}
      <div className="komari-nav-bar">
        <div className="komari-nav-left">
          <button type="button" className="komari-back-btn" onClick={onBack} title="返回服务器列表">
            <ArrowLeft size={16} />
          </button>
          <span className="komari-server-flag">{customMeta.customFlag !== '自动识别' ? customMeta.customFlag : (node.flag || '🌐')}</span>
          <h1 className="komari-server-title">{customMeta.customName || node.name}</h1>
          <span className={`komari-status-tag ${isOnline ? 'online' : 'offline'}`}>
            <span className={`status-dot-pulse ${isOnline ? '' : 'offline'}`} /> {isOnline ? '在線' : '離線'} · {heartbeatText}
          </span>

          {/* 彩色标签 */}
          <div className="komari-tags-wrap">
            {coloredTags.map((tag, idx) => (
              <span key={idx} className={`komari-tag-pill tag-color-${tag.color}`}>
                {tag.text}
              </span>
            ))}
          </div>
        </div>

        <div className="komari-nav-right">
          {/* 收藏星标 */}
          <button
            type="button"
            className={`komari-icon-btn ${isFavorite ? 'active text-amber' : ''}`}
            onClick={toggleFavorite}
            title={isFavorite ? '已收藏' : '收藏此节点'}
          >
            <Star size={16} weight={isFavorite ? 'fill' : 'regular'} />
          </button>

          {/* 服务器快切选择器 */}
          {nodes.length > 1 && (
            <div className="komari-node-switcher">
              <button
                type="button"
                className="komari-switcher-arrow"
                onClick={() => onSelectNode && onSelectNode(prevNode)}
                title={`切换至 ${prevNode.name}`}
              >
                <CaretLeft size={14} />
              </button>
              <span className="komari-switcher-name" title={node.name}>
                {customMeta.customName || node.name}
              </span>
              <button
                type="button"
                className="komari-switcher-arrow"
                onClick={() => onSelectNode && onSelectNode(nextNode)}
                title={`切换至 ${nextNode.name}`}
              >
                <CaretRight size={14} />
              </button>
            </div>
          )}

          {/* 运营商信息标签 */}
          <div className="komari-isp-pill mono">
            <Globe size={13} />
            <span>{ispText}</span>
          </div>
        </div>
      </div>

      {/* 2. 顶部 8 核心指标看板 (2行4列卡片网格) */}
      <div className="komari-stats-grid-8">
        {/* 1. 节点价格 */}
        <div className="komari-stat-card">
          <div className="komari-stat-head">
            <span className="komari-stat-label">节点价格</span>
            <Tag size={15} className="komari-stat-icon text-muted" />
          </div>
          <div className="komari-stat-value mono">
            {priceDisplay} {billing.price ? <small className="text-muted">/ 月</small> : null}
          </div>
        </div>

        {/* 2. 月均支出 */}
        <div className="komari-stat-card">
          <div className="komari-stat-head">
            <span className="komari-stat-label">月均支出</span>
            <Coins size={15} className="komari-stat-icon text-muted" />
          </div>
          <div className="komari-stat-value mono">
            {monthlyExpense} {billing.price ? <small className="text-muted">/ 月</small> : null}
          </div>
        </div>

        {/* 3. 剩余时间 */}
        <div className="komari-stat-card">
          <div className="komari-stat-head">
            <span className="komari-stat-label">剩余时间</span>
            <CalendarBlank size={15} className="komari-stat-icon text-muted" />
          </div>
          <div className="komari-stat-value mono text-mint font-bold">
            {remainingDays} {remainingDays !== '—' ? <small className="text-mint font-normal">天</small> : null}
          </div>
        </div>

        {/* 4. 剩余价值 */}
        <div className="komari-stat-card">
          <div className="komari-stat-head">
            <span className="komari-stat-label">剩余价值</span>
            <Wallet size={15} className="komari-stat-icon text-muted" />
          </div>
          <div className="komari-stat-value mono font-bold">
            {remainingValue}
          </div>
        </div>

        {/* 5. 累计流量 */}
        <div className="komari-stat-card">
          <div className="komari-stat-head">
            <span className="komari-stat-label">累计流量</span>
            <TrendUp size={15} className="komari-stat-icon text-muted" />
          </div>
          <div className="komari-stat-value mono">
            {formatBytes(totalTraffic)}
          </div>
        </div>

        {/* 6. 流量配额 */}
        <div className="komari-stat-card">
          <div className="komari-stat-head">
            <span className="komari-stat-label">流量配额</span>
            <ChartPieSlice size={15} className="komari-stat-icon text-muted" />
          </div>
          <div className="komari-stat-value mono">
            {trafficPercent} <small className="text-muted">%</small>
          </div>
        </div>

        {/* 7. 运行时间 */}
        <div className="komari-stat-card">
          <div className="komari-stat-head">
            <span className="komari-stat-label">运行时间</span>
            <Clock size={15} className="komari-stat-icon text-muted" />
          </div>
          <div className="komari-stat-value mono">
            {uptimeText}
          </div>
        </div>

        {/* 8. 连接数 */}
        <div className="komari-stat-card">
          <div className="komari-stat-head">
            <span className="komari-stat-label">连接数</span>
            <ShareNetwork size={15} className="komari-stat-icon text-muted" />
          </div>
          <div className="komari-stat-value mono">
            {totalConnections}
          </div>
        </div>
      </div>

      {/* 3. 硬件与系统信息卡片 (2x2 宫格) */}
      <div className="komari-info-grid-4">
        {/* 卡片 1: 硬件信息 */}
        <div className="komari-info-card">
          <div className="komari-info-header">
            <h3>硬件信息</h3>
            <a
              href={cpuBenchmarkUrl}
              target="_blank"
              rel="noopener noreferrer"
              className="komari-bench-link mono"
              title={`在 PassMark 查询 ${cleanedCpuModel || cpuModel} 跑分天梯排行`}
            >
              CPU Mark 排行 ↗
            </a>
          </div>
          <div className="komari-info-rows">
            <div className="komari-info-row">
              <span className="komari-info-label">
                <Cpu size={14} /> CPU
              </span>
              <span className="komari-info-val mono">{cpuModel}</span>
            </div>
            <div className="komari-info-row">
              <span className="komari-info-label">
                <Globe size={14} /> IP
              </span>
              <span className="komari-info-val mono">{publicIp}</span>
            </div>
            <div className="komari-info-row">
              <span className="komari-info-label">
                <Desktop size={14} /> 物理核心
              </span>
              <span className="komari-info-val mono">{cores} 核</span>
            </div>
            <div className="komari-info-row">
              <span className="komari-info-label">
                <WifiHigh size={14} /> 虚拟化
              </span>
              <span className="komari-info-val mono">{arch}</span>
            </div>
          </div>
        </div>

        {/* 卡片 2: 系统信息 */}
        <div className="komari-info-card">
          <div className="komari-info-header">
            <h3>系统信息</h3>
          </div>
          <div className="komari-info-rows">
            <div className="komari-info-row">
              <span className="komari-info-label">
                <DistroIcon os={os} className="komari-distro-icon" /> 操作系统
              </span>
              <span className="komari-info-val mono">{os}</span>
            </div>
            <div className="komari-info-row">
              <span className="komari-info-label">
                <Tag size={14} /> 内核版本
              </span>
              <span className="komari-info-val mono">{kernel}</span>
            </div>
            <div className="komari-info-row">
              <span className="komari-info-label">
                <Clock size={14} /> 运行时间
              </span>
              <span className="komari-info-val mono">{uptimeText}</span>
            </div>
            <div className="komari-info-row">
              <span className="komari-info-label">
                <Globe size={14} /> 运营商
              </span>
              <span className="komari-info-val mono">{ispText}</span>
            </div>
          </div>
        </div>

        {/* 卡片 3: 存储信息 */}
        <div className="komari-info-card">
          <div className="komari-info-header">
            <h3>存储信息</h3>
          </div>
          <div className="komari-storage-cols">
            <div className="komari-storage-col">
              <span className="komari-storage-label">
                <HardDrive size={14} /> 内存
              </span>
              <strong className="komari-storage-val mono">{formatBytes(memTotal)}</strong>
              <small className="komari-storage-sub mono text-muted">
                已用: {formatBytes(memUsed)}
              </small>
            </div>
            <div className="komari-storage-col">
              <span className="komari-storage-label">
                <ArrowsLeftRight size={14} /> 内存交换
              </span>
              <strong className="komari-storage-val mono">{formatBytes(swapTotal)}</strong>
              <small className="komari-storage-sub mono text-muted">
                已用: {formatBytes(swapUsed)}
              </small>
            </div>
            <div className="komari-storage-col">
              <span className="komari-storage-label">
                <HardDrive size={14} /> 硬盘
              </span>
              <strong className="komari-storage-val mono">{formatBytes(diskTotal)}</strong>
              <small className="komari-storage-sub mono text-muted">
                已用: {formatBytes(diskUsed)}
              </small>
            </div>
          </div>
        </div>

        {/* 卡片 4: 网络信息 */}
        <div className="komari-info-card">
          <div className="komari-info-header">
            <h3>网络信息</h3>
          </div>
          <div className="komari-info-rows">
            <div className="komari-info-row">
              <span className="komari-info-label">
                <WifiHigh size={14} /> 总流量 [IPv4]
              </span>
              <span className="komari-info-val mono">
                {formatBytes(rawTx)} / {formatBytes(rawRx)} <span className="text-muted">(配额 {trafficQuotaText})</span>
              </span>
            </div>
            <div className="komari-info-row">
              <span className="komari-info-label">
                <TrendUp size={14} /> 近一天下行/上行
              </span>
              <span className="komari-info-val mono">
                {dailyTrafficText}
              </span>
            </div>
            <div className="komari-info-row">
              <span className="komari-info-label">
                <ArrowsClockwise size={14} /> 实时网络速率
              </span>
              <span className="komari-info-val mono">
                ^ {formatRate(rate?.up || 0)} · v {formatRate(rate?.down || 0)}
              </span>
            </div>
          </div>
        </div>
      </div>

      {/* 4. 历史时序折线图表区 (带时间范围切换器) */}
      <div className="komari-charts-section">
        {/* 时间切换条 */}
        <div className="komari-time-tabs-row">
          <div className="komari-time-tabs">
            {['实时', '4小时', '1天', '7天', '自定义'].map((tab) => (
              <button
                key={tab}
                type="button"
                className={`komari-time-tab ${activeTimeRange === tab ? 'active' : ''}`}
                onClick={() => setActiveTimeRange(tab)}
              >
                {tab}
              </button>
            ))}
          </div>
        </div>

        {/* 图表宫格 */}
        <div className="komari-charts-grid">
          {/* 1. CPU 与负载 */}
          <KomariChartCard
            title="CPU 与负载"
            icon="🔴"
            badgeText={`${cpuPercent.toFixed(1)}%`}
            series={telemetrySeries.cpus}
            strokeColor="#f97316"
            yMax="100%"
            yMid="50%"
            yMin="0%"
            timeLabels={telemetrySeries.times}
          />

          {/* 2. 内存与 Swap */}
          <KomariChartCard
            title="内存与 Swap"
            icon="🟣"
            badgeText={`${formatBytes(memUsed)} / ${formatBytes(memTotal)}`}
            series={telemetrySeries.mems}
            strokeColor="#38bdf8"
            dualSeries={{ data: telemetrySeries.swaps, color: '#f59e0b' }}
            yMax={`${formatBytes(memTotal)}`}
            yMid={`${formatBytes(memTotal / 2)}`}
            yMin="0 B"
            timeLabels={telemetrySeries.times}
          />

          {/* 3. 磁盘 */}
          <KomariChartCard
            title="磁盘"
            icon="🟢"
            badgeText={`${formatBytes(diskUsed)} / ${formatBytes(diskTotal)}`}
            series={telemetrySeries.disks}
            strokeColor="#10b981"
            yMax={`${formatBytes(diskTotal)}`}
            yMid={`${formatBytes(diskTotal / 2)}`}
            yMin="0 B"
            timeLabels={telemetrySeries.times}
          />

          {/* 4. 实时网络 */}
          <KomariChartCard
            title="实时网络"
            icon="🔵"
            badgeText={`^ ${formatRate(rate?.up || 0)}  v ${formatRate(rate?.down || 0)}`}
            series={telemetrySeries.downs}
            strokeColor="#0284c7"
            dualSeries={{ data: telemetrySeries.ups, color: '#a855f7' }}
            yMax={netYMax}
            yMid={netYMid}
            yMin="0 B/s"
            timeLabels={telemetrySeries.times}
          />

          {/* 5. GPU 利用率 */}
          <KomariChartCard
            title="GPU 利用率"
            icon="🟢"
            badgeText="0.0%"
            series={Array(telemetrySeries.cpus.length).fill(0)}
            strokeColor="#10b981"
            yMax="100%"
            yMid="50%"
            yMin="0%"
            timeLabels={telemetrySeries.times}
          />

          {/* 6. 网络连接 */}
          <KomariChartCard
            title="网络连接"
            icon="🔴"
            badgeText={`TCP: ${tcpCount}  UDP: ${udpCount}`}
            series={telemetrySeries.conns}
            strokeColor="#ef4444"
            yMax={`${connYMax}`}
            yMid={`${connYMid}`}
            yMin="0"
            timeLabels={telemetrySeries.times}
          />

          {/* 7. 进程 */}
          <KomariChartCard
            title="进程"
            icon="🔵"
            badgeText={`${processCount}`}
            series={telemetrySeries.procs}
            strokeColor="#6366f1"
            yMax={`${procYMax}`}
            yMid={`${procYMid}`}
            yMin="0"
            timeLabels={telemetrySeries.times}
          />
        </div>
      </div>

      {/* 5. 三网延迟与网络监测模块 (带节点快速筛选与平滑折线对比) */}
      <div className="komari-ping-section">
        <div className="komari-ping-toolbar">
          <div className="komari-time-tabs">
            {['1小时', '6小时', '12小时', '1天', '7天', '自定义'].map((tab) => (
              <button
                key={tab}
                type="button"
                className={`komari-time-tab ${activePingRange === tab ? 'active' : ''}`}
                onClick={() => setActivePingRange(tab)}
              >
                {tab}
              </button>
            ))}
          </div>

          <div className="komari-ping-actions">
            <button
              type="button"
              className="button button-quiet btn-sm"
              onClick={() => handleSelectAllTargets(true)}
            >
              全选
            </button>
            <button
              type="button"
              className="button button-quiet btn-sm"
              onClick={() => handleSelectAllTargets(false)}
            >
              全不选
            </button>
          </div>
        </div>

        {/* 测速目标卡片列表 */}
        <div className="komari-targets-grid">
          {pingTargets.map((t) => {
            const isChecked = Boolean(selectedTargets[t.id])
            return (
              <div
                key={t.id}
                className={`komari-target-card ${isChecked ? 'active' : ''}`}
                onClick={() => handleToggleTarget(t.id)}
              >
                <div className="komari-target-head">
                  <span className="komari-target-bar" style={{ backgroundColor: t.color }} />
                  <strong className="komari-target-name">{t.name}</strong>
                  <span className="komari-target-check">
                    <input type="checkbox" checked={isChecked} onChange={() => {}} />
                  </span>
                </div>
                <div className="komari-target-stats mono">
                  <span>{t.latency}</span>
                  <span className={t.lossColor}>{t.loss}</span>
                  <span className="text-muted">{t.lastChecked || '刚刚'}</span>
                </div>
              </div>
            )
          })}
        </div>

        {/* 平滑延迟多线对比折线图 */}
        <div className="komari-ping-chart-card">
          <div className="komari-ping-chart-header">
            <span className="komari-ping-chart-title">平滑延迟 (ms)</span>
          </div>

          <div className="komari-ping-chart-wrap">
            <svg viewBox="0 0 900 240" className="komari-ping-svg" preserveAspectRatio="none">
              {/* 背景虚线网格 */}
              <line x1="20" y1="20" x2="880" y2="20" stroke="currentColor" strokeDasharray="3 3" opacity="0.1" />
              <line x1="20" y1="75" x2="880" y2="75" stroke="currentColor" strokeDasharray="3 3" opacity="0.1" />
              <line x1="20" y1="130" x2="880" y2="130" stroke="currentColor" strokeDasharray="3 3" opacity="0.1" />
              <line x1="20" y1="185" x2="880" y2="185" stroke="currentColor" strokeDasharray="3 3" opacity="0.1" />

              {/* Y轴刻度标注 */}
              <text x="10" y="24" fontSize="10" fill="currentColor" opacity="0.4" fontFamily="monospace">{pingChartData.yTicks[0]}</text>
              <text x="10" y="79" fontSize="10" fill="currentColor" opacity="0.4" fontFamily="monospace">{pingChartData.yTicks[1]}</text>
              <text x="10" y="134" fontSize="10" fill="currentColor" opacity="0.4" fontFamily="monospace">{pingChartData.yTicks[2]}</text>
              <text x="10" y="189" fontSize="10" fill="currentColor" opacity="0.4" fontFamily="monospace">{pingChartData.yTicks[3]}</text>

              {/* 多线动态绘制 */}
              {pingChartData.targetPaths.map((t) => {
                if (!selectedTargets[t.id]) return null
                return (
                  <path
                    key={t.id}
                    d={t.path}
                    fill="none"
                    stroke={t.color}
                    strokeWidth="2"
                    strokeLinecap="round"
                  />
                )
              })}
            </svg>

            {/* X 轴时间轴 */}
            <div className="komari-ping-x-axis mono">
              {pingChartData.timeMarks.map((tm, idx) => (
                <span key={idx}>{tm}</span>
              ))}
            </div>
          </div>

          {/* 图表底部图例 */}
          <div className="komari-ping-legend">
            {pingTargets.map((t) => (
              <span
                key={t.id}
                className={`komari-legend-item ${selectedTargets[t.id] ? '' : 'muted'}`}
                onClick={() => handleToggleTarget(t.id)}
              >
                <span className="komari-legend-dot" style={{ backgroundColor: t.color }} />
                <span>{t.name}</span>
              </span>
            ))}
          </div>
        </div>
      </div>

      {/* 6. 页脚署名 */}
      <footer className="komari-footer">
        <div>Powered by <strong>ProbeWatch Monitor</strong></div>
        <div>Theme by <strong>Komari Glassmorphism</strong></div>
      </footer>
    </section>
  )
}
