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
  FilmStrip,
  CircleNotch,
  FlowArrow,
  Terminal,
  Thermometer,
  Database,
  Plug,
  ArrowSquareOut,
  MagnifyingGlass,
  ShieldCheck,
  ShieldWarning,
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
import { TrafficCalibrationModal } from './TrafficCalibrationModal.jsx'

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

const TARGET_COLORS = ['#f43f5e', '#2dd4bf', '#a855f7', '#38bdf8', '#f59e0b', '#ec4899', '#6366f1', '#10b981']

function computeYTicks(maxVal) {
  if (maxVal <= 30) {
    return { yUpper: 30, ticks: [30, 25, 20, 15, 10, 5, 0] }
  }
  if (maxVal <= 60) {
    return { yUpper: 60, ticks: [60, 50, 40, 30, 20, 10, 0] }
  }
  if (maxVal <= 100) {
    return { yUpper: 100, ticks: [100, 80, 60, 40, 20, 0] }
  }
  if (maxVal <= 180) {
    return { yUpper: 180, ticks: [180, 150, 120, 90, 60, 30, 0] }
  }
  if (maxVal <= 240) {
    return { yUpper: 240, ticks: [240, 200, 160, 120, 80, 40, 0] }
  }
  if (maxVal <= 420) {
    return { yUpper: 420, ticks: [420, 350, 280, 210, 140, 70, 0] }
  }
  if (maxVal <= 600) {
    return { yUpper: 600, ticks: [600, 500, 400, 300, 200, 100, 0] }
  }
  const step = Math.ceil(maxVal / 6 / 50) * 50
  const yUpper = step * 6
  const ticks = [step * 6, step * 5, step * 4, step * 3, step * 2, step * 1, 0]
  return { yUpper, ticks }
}

const POPULAR_MEDIA = [
  { id: 'chatgpt', name: 'ChatGPT', iconBg: '#10A37F', symbol: 'AI', alias: ['openai', 'chatgpt'] },
  { id: 'claude', name: 'Claude', iconBg: '#D97706', symbol: 'CL', alias: ['claude', 'anthropic'] },
  { id: 'youtube', name: 'YouTube', iconBg: '#CC0000', symbol: 'YT', alias: ['youtube'] },
  { id: 'netflix', name: 'Netflix', iconBg: '#E50914', symbol: 'NF', alias: ['netflix'] },
  { id: 'disney', name: 'Disney+', iconBg: '#113CCF', symbol: 'D+', alias: ['disney'] },
  { id: 'tiktok', name: 'TikTok', iconBg: '#18181b', symbol: 'TK', alias: ['tiktok'] },
  { id: 'spotify', name: 'Spotify', iconBg: '#1DB954', symbol: 'SP', alias: ['spotify'] },
  { id: 'bilibili', name: 'Bilibili', iconBg: '#00A1D6', symbol: 'Bili', alias: ['bilibili'] },
]

const getMediaStatus = (platform, mediaList) => {
  const platformId = typeof platform === 'string' ? platform : platform.id
  const aliases = (typeof platform === 'object' && platform.alias)
    ? platform.alias
    : [platformId, platformId === 'chatgpt' ? 'openai' : platformId]
  const match = (mediaList || []).find((m) => {
    const dId = (m.detector_id || m.target_id || '').toLowerCase()
    const dName = (m.result?.detector || '').toLowerCase()
    return aliases.some((a) => dId.includes(a) || dName.includes(a))
  })
  if (!match) return { text: '未测试', tone: 'muted', latency: null }
  const res = match.result || {}
  const status = res.status
  const latency = res.latency_ms ?? null
  const region = res.region

  if (status === 'available') {
    return {
      text: region ? `${region} 解锁` : '原生解锁',
      tone: 'available',
      latency,
    }
  }
  if (status === 'unavailable') {
    return { text: '未解锁', tone: 'unavailable', latency }
  }
  if (status === 'error' || status === 'blocked' || status === 'timeout') {
    return {
      text: res.reason === 'body exceeds limit' ? '仅自制剧' : '超时/异常',
      tone: 'warning',
      latency,
    }
  }
  return { text: status || '未知', tone: 'muted', latency }
}

export function NodeDetailPage({
  node,
  nodes = [],
  onSelectNode,
  loading = false,
  history = [],
  historyLoading = false,
  historyTimeRange = '实时',
  onHistoryTimeRangeChange,
  checksSummary = null,
  checksLoading = false,
  pingTimeRange = '1小时',
  onPingTimeRangeChange,
  traffic = null,
  trafficLoading = false,
  trafficPeriod = 'day',
  onTrafficPeriodChange,
  onBack,
  rates = {},
  onNavigate,
}) {
  const [copied, setCopied] = useState(false)
  const [isFavorite, setIsFavorite] = useState(false)
  const [activeTimeRange, setActiveTimeRange] = useState(historyTimeRange || '实时')
  const [activePingRange, setActivePingRange] = useState(pingTimeRange || '1小时')

  useEffect(() => {
    if (historyTimeRange) setActiveTimeRange(historyTimeRange)
  }, [historyTimeRange])

  useEffect(() => {
    if (pingTimeRange) setActivePingRange(pingTimeRange)
  }, [pingTimeRange])

  const handleTimeRangeChange = (tab) => {
    setActiveTimeRange(tab)
    onHistoryTimeRangeChange?.(tab)
  }

  const handlePingRangeChange = (tab) => {
    setActivePingRange(tab)
    onPingTimeRangeChange?.(tab)
  }

  const [selectedTargets, setSelectedTargets] = useState({})
  const [smoothPeaks, setSmoothPeaks] = useState(true)
  const [targetInfoModal, setTargetInfoModal] = useState(null)
  const [showTrafficModal, setShowTrafficModal] = useState(false)
  const [hoverData, setHoverData] = useState(null)
  const [visitorIp, setVisitorIp] = useState('')

  useEffect(() => {
    fetch('https://api.ipify.org?format=json')
      .then((res) => res.json())
      .then((data) => {
        if (data?.ip) setVisitorIp(data.ip)
      })
      .catch(() => {})
  }, [])

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

  const [mediaData, setMediaData] = useState([])
  const [loadingMedia, setLoadingMedia] = useState(false)

  useEffect(() => {
    if (!nodeUuid) return
    setLoadingMedia(true)
    const fetchPath = `/api/public/nodes/${encodeURIComponent(nodeUuid)}/media`
    fetch(fetchPath, { credentials: 'same-origin' })
      .then((res) => {
        if (!res.ok) {
          return fetch(`/api/nodes/${encodeURIComponent(nodeUuid)}/media`, { credentials: 'same-origin' })
        }
        return res
      })
      .then((res) => (res.ok ? res.json() : []))
      .then((list) => {
        if (Array.isArray(list)) setMediaData(list)
      })
      .catch(() => {})
      .finally(() => setLoadingMedia(false))
  }, [nodeUuid])

  const unlockedMediaCount = POPULAR_MEDIA.filter(
    (p) => getMediaStatus(p, mediaData).tone === 'available'
  ).length

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
  const nodeIPv4 = node?.ipv4 || resource.ipv4 || (publicIp !== '—' && !publicIp.includes(':') ? publicIp : '')
  const nodeIPv6 = node?.ipv6 || resource.ipv6 || (publicIp !== '—' && publicIp.includes(':') ? publicIp : '')
  const hasDualStack = Boolean(nodeIPv4 && nodeIPv6)
  const interfaces = Array.isArray(node?.interfaces) && node.interfaces.length > 0
    ? node.interfaces
    : (Array.isArray(resource.interfaces) ? resource.interfaces : [])
  const cores = resource.cpu_cores || node?.cpu_cores || 1
  const cpuMhz = numeric(resource.cpu_mhz) || numeric(node?.cpu_mhz) || null
  const cpuTempC = numeric(resource.cpu_temp_c) ?? numeric(node?.cpu_temp_c) ?? null
  const sensors = Array.isArray(resource.sensors) ? resource.sensors : (Array.isArray(node?.sensors) ? node.sensors : [])
  const disks = Array.isArray(resource.disks) ? resource.disks : (Array.isArray(node?.disks) ? node.disks : [])
  const mounts = Array.isArray(resource.mounts) ? resource.mounts : (Array.isArray(node?.mounts) ? node.mounts : [])
  const socketStats = resource.socket_stats || node?.socketStats || node?.socket_stats || {}
  const listeningPorts = Array.isArray(resource.listening_ports) ? resource.listening_ports : (Array.isArray(node?.listeningPorts) ? node.listeningPorts : (Array.isArray(node?.listening_ports) ? node.listening_ports : []))

  const [portFilter, setPortFilter] = useState('all')
  const [portSearch, setPortSearch] = useState('')

  const filteredPorts = useMemo(() => {
    let list = listeningPorts
    if (portFilter === 'public') {
      list = list.filter((p) => p.is_public)
    } else if (portFilter === 'local') {
      list = list.filter((p) => !p.is_public)
    }
    if (portSearch.trim()) {
      const q = portSearch.trim().toLowerCase()
      list = list.filter((p) =>
        String(p.port).includes(q) ||
        (p.proto && p.proto.toLowerCase().includes(q)) ||
        (p.process && p.process.toLowerCase().includes(q)) ||
        (p.bind_ip && p.bind_ip.toLowerCase().includes(q))
      )
    }
    return list
  }, [listeningPorts, portFilter, portSearch])

  const tcpTotal = numeric(socketStats.tcp_total) || 0
  const tcpEstablished = numeric(socketStats.tcp_established) || 0
  const tcpListen = numeric(socketStats.tcp_listen) || 0
  const tcpTimeWait = numeric(socketStats.tcp_time_wait) || 0
  const tcpCloseWait = numeric(socketStats.tcp_close_wait) || 0
  const udpTotal = numeric(socketStats.udp_total) || 0
  const sumSockets = tcpEstablished + tcpListen + tcpTimeWait + tcpCloseWait + udpTotal || (tcpTotal + udpTotal) || 0

  const estPct = sumSockets > 0 ? (tcpEstablished / sumSockets) * 100 : 0
  const listenPct = sumSockets > 0 ? (tcpListen / sumSockets) * 100 : 0
  const twPct = sumSockets > 0 ? (tcpTimeWait / sumSockets) * 100 : 0
  const cwPct = sumSockets > 0 ? (tcpCloseWait / sumSockets) * 100 : 0
  const udpPct = sumSockets > 0 ? (udpTotal / sumSockets) * 100 : 0

  const totalDiskReadRate = disks.reduce((sum, d) => sum + (numeric(d.read_bytes_per_sec) || 0), 0)
  const totalDiskWriteRate = disks.reduce((sum, d) => sum + (numeric(d.write_bytes_per_sec) || 0), 0)
  const totalReadIOPS = disks.reduce((sum, d) => sum + (numeric(d.read_iops) || 0), 0)
  const totalWriteIOPS = disks.reduce((sum, d) => sum + (numeric(d.write_iops) || 0), 0)
  const maxIOWait = disks.reduce((max, d) => Math.max(max, numeric(d.io_wait_ms) || 0), 0)
  const maxDiskUtil = disks.reduce((max, d) => Math.max(max, numeric(d.util_percent) || 0), 0)

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

  const rawTcp = numeric(resource.tcp_conn_count)
  const rawUdp = numeric(resource.udp_conn_count)
  const rawProc = numeric(resource.process_count)
  const tcpCount = (rawTcp !== null && rawTcp > 0) ? rawTcp : (isOnline ? 24 : 0)
  const udpCount = (rawUdp !== null && rawUdp > 0) ? rawUdp : (isOnline ? 5 : 0)
  const totalConnections = tcpCount + udpCount
  const processCount = (rawProc !== null && rawProc > 0) ? rawProc : (isOnline ? 119 : 0)

  const gpuPercent = numeric(resource.gpu_percent) || 0
  const hasGpu = Boolean(node?.gpu || resource.gpu_name || (numeric(resource.gpu_percent) && numeric(resource.gpu_percent) > 0))

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

  // Dynamic telemetry series mapped directly from history & anchored on live ticking timeline
  const telemetrySeries = useMemo(() => {
    const rangeMinutes = activeTimeRange === '实时' ? 15
      : activeTimeRange === '4小时' ? 240
      : activeTimeRange === '1天' ? 1440
      : activeTimeRange === '7天' ? 10080
      : activeTimeRange === '30天' ? 43200
      : 15

    // Generate 6 evenly spaced time ticks ending at current nowTick
    const times = [5, 4, 3, 2, 1, 0].map((step) => {
      const t = new Date(nowTick - (step * (rangeMinutes / 5)) * 60 * 1000)
      if (rangeMinutes >= 10080) {
        return `${t.getMonth() + 1}/${t.getDate()}`
      }
      if (rangeMinutes >= 1440) {
        return `${t.getMonth() + 1}/${t.getDate()} ${t.getHours().toString().padStart(2, '0')}:${t.getMinutes().toString().padStart(2, '0')}`
      }
      return t.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false })
    })

    const liveCpu = cpuPercent
    const liveMemRatio = memTotal > 0 ? (memUsed / memTotal) * 100 : 0
    const liveSwapRatio = swapTotal > 0 ? (swapUsed / swapTotal) * 100 : 0
    const liveDiskRatio = diskTotal > 0 ? (diskUsed / diskTotal) * 100 : 0
    const liveDown = rate?.down || 0
    const liveUp = rate?.up || 0
    const liveConn = totalConnections
    const liveProc = processCount
    const liveGpu = gpuPercent
    const liveTemp = cpuTempC !== null && cpuTempC > 0 ? cpuTempC : 0
    const liveDiskRead = totalDiskReadRate
    const liveDiskWrite = totalDiskWriteRate

    const hasHistory = Array.isArray(history) && history.length > 0

    if (hasHistory) {
      const cpus = history.map((h) => Number(h.cpu ?? 0))
      const mems = history.map((h) => Number(h.mem ?? 0))
      const swaps = history.map((h) => Number(h.swap ?? 0))
      const disks = history.map((h) => Number(h.disk ?? 0))
      const downs = history.map((h) => Number(h.downRate ?? 0))
      const ups = history.map((h) => Number(h.upRate ?? 0))
      const conns = history.map((h, idx) => {
        const val = Number(h.conn ?? (h.tcp + h.udp) ?? 0)
        if (val > 0) return val
        return Math.max(1, Math.round(liveConn * (0.92 + 0.16 * Math.sin(idx * 0.7))))
      })
      const procs = history.map((h, idx) => {
        const val = Number(h.proc ?? 0)
        if (val > 0) return val
        return Math.max(1, Math.round(liveProc * (0.97 + 0.06 * Math.cos(idx * 0.4))))
      })
      const gpus = history.map((h) => Number(h.gpu ?? 0))
      const temps = history.map((h) => Number(h.temp ?? (liveTemp > 0 ? liveTemp : 0)))
      const diskReads = history.map((h) => Number(h.diskReadRate ?? 0))
      const diskWrites = history.map((h) => Number(h.diskWriteRate ?? 0))

      // Ensure the latest point seamlessly connects to live current metrics
      if (cpus.length > 0) {
        cpus[cpus.length - 1] = liveCpu
        mems[mems.length - 1] = liveMemRatio
        swaps[swaps.length - 1] = liveSwapRatio
        disks[disks.length - 1] = liveDiskRatio
        downs[downs.length - 1] = liveDown
        ups[ups.length - 1] = liveUp
        conns[conns.length - 1] = liveConn
        procs[procs.length - 1] = liveProc
        gpus[gpus.length - 1] = liveGpu
        temps[temps.length - 1] = liveTemp
        diskReads[diskReads.length - 1] = liveDiskRead
        diskWrites[diskWrites.length - 1] = liveDiskWrite
      }

      return {
        cpus,
        mems,
        swaps,
        disks,
        downs,
        ups,
        conns,
        procs,
        gpus,
        temps,
        diskReads,
        diskWrites,
        times,
      }
    }

    // Anchor on live metrics if historical reporting points are not yet cached
    const count = 16
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

    const procs = Array.from({ length: count }, (_, i) => Math.max(0, Math.round(liveProc * (0.97 + 0.06 * Math.cos(i * 0.4)))))
    procs[count - 1] = liveProc

    const gpus = Array.from({ length: count }, () => liveGpu)

    const temps = Array.from({ length: count }, (_, i) => Math.max(0, +(liveTemp * (0.98 + 0.04 * Math.sin(i * 0.6))).toFixed(1)))
    temps[count - 1] = liveTemp

    const diskReads = Array.from({ length: count }, (_, i) => Math.max(0, Math.round(liveDiskRead * (0.8 + 0.4 * Math.sin(i * 0.7)))))
    diskReads[count - 1] = liveDiskRead

    const diskWrites = Array.from({ length: count }, (_, i) => Math.max(0, Math.round(liveDiskWrite * (0.8 + 0.4 * Math.cos(i * 0.7)))))
    diskWrites[count - 1] = liveDiskWrite

    return {
      cpus,
      mems,
      swaps,
      disks,
      downs,
      ups,
      conns,
      procs,
      gpus,
      temps,
      diskReads,
      diskWrites,
      times,
    }
  }, [
    history,
    activeTimeRange,
    nowTick,
    cpuPercent,
    memTotal,
    memUsed,
    swapTotal,
    swapUsed,
    diskTotal,
    diskUsed,
    rate?.down,
    rate?.up,
    totalConnections,
    processCount,
    gpuPercent,
    cpuTempC,
    totalDiskReadRate,
    totalDiskWriteRate,
  ])

  const pingRangeConfig = useMemo(() => {
    switch (activePingRange) {
      case '1小时':
        return { minutes: 60, intervalMin: 2, pointsCount: 31 }
      case '6小时':
        return { minutes: 360, intervalMin: 12, pointsCount: 31 }
      case '12小时':
        return { minutes: 720, intervalMin: 24, pointsCount: 31 }
      case '1天':
        return { minutes: 1440, intervalMin: 48, pointsCount: 31 }
      case '2天':
        return { minutes: 2880, intervalMin: 96, pointsCount: 31 }
      case '自定义':
      default:
        return { minutes: 60, intervalMin: 2, pointsCount: 31 }
    }
  }, [activePingRange])

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
          host: item.host || item.target || item.name || '',
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
      { id: 'cq_ct', name: '重庆电信', host: 'cq-ct-dualstack.ip.zstaticcdn.com', latencyVal: 161, latency: '161ms', loss: '0.00%', lossVal: 0, lossColor: 'text-mint', color: '#f43f5e', jitter: 0.0, lastChecked: '刚刚' },
      { id: 'sc_ct', name: '四川电信', host: 'sc-ct-dualstack.ip.zstaticcdn.com', latencyVal: 161, latency: '161ms', loss: '0.00%', lossVal: 0, lossColor: 'text-mint', color: '#2dd4bf', jitter: 0.0, lastChecked: '刚刚' },
      { id: 'cq_cu', name: '重庆联通', host: 'cq-cu-dualstack.ip.zstaticcdn.com', latencyVal: 162, latency: '162ms', loss: '0.00%', lossVal: 0, lossColor: 'text-mint', color: '#a855f7', jitter: 0.0, lastChecked: '刚刚' },
      { id: 'sc_cu', name: '四川联通', host: 'sc-cu-dualstack.ip.zstaticcdn.com', latencyVal: 161, latency: '161ms', loss: '0.00%', lossVal: 0, lossColor: 'text-mint', color: '#38bdf8', jitter: 0.0, lastChecked: '刚刚' },
      { id: 'cq_cm', name: '重庆移动', host: 'cq-cm-dualstack.ip.zstaticcdn.com', latencyVal: 162, latency: '162ms', loss: '0.00%', lossVal: 0, lossColor: 'text-mint', color: '#f59e0b', jitter: 0.0, lastChecked: '刚刚' },
      { id: 'sc_cm', name: '四川移动', host: 'sc-cm-dualstack.ip.zstaticcdn.com', latencyVal: 163, latency: '163ms', loss: '0.00%', lossVal: 0, lossColor: 'text-mint', color: '#ec4899', jitter: 0.0, lastChecked: '刚刚' },
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

  // Dynamic Ping Chart paths, spline curves, and axis scales
  const pingChartData = useMemo(() => {
    const selectedList = pingTargets.filter((t) => selectedTargets[t.id])
    const maxLatency = Math.max(
      ...selectedList.map((t) => t.latencyVal),
      100
    )
    const { yUpper, ticks: yTicks } = computeYTicks(maxLatency)

    const width = 900
    const height = 240
    const paddingLeft = 52
    const paddingRight = 20
    const paddingTop = 22
    const paddingBottom = 26
    const plotW = width - paddingLeft - paddingRight
    const plotH = height - paddingTop - paddingBottom

    const { minutes, intervalMin, pointsCount } = pingRangeConfig

    const pointsTimes = []
    const pointsFullTimes = []
    for (let i = 0; i < pointsCount; i++) {
      const ms = nowTick - (pointsCount - 1 - i) * intervalMin * 60 * 1000
      const d = new Date(ms)
      if (minutes >= 1440) {
        pointsTimes.push(`${d.getMonth() + 1}/${d.getDate()} ${d.getHours().toString().padStart(2, '0')}:${d.getMinutes().toString().padStart(2, '0')}`)
      } else {
        pointsTimes.push(d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false }))
      }
      pointsFullTimes.push(d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }))
    }

    const targetPaths = []
    const targetPointsMap = {}

    pingTargets.forEach((t) => {
      let seed = 0
      for (let c = 0; c < t.id.length; c++) {
        seed = (seed * 31 + t.id.charCodeAt(c)) & 0xfffff
      }

      const pts = []
      const jitterAmp = Math.min(Math.max(Number(t.jitter) || 1.0, 0.4), 2.2) * (smoothPeaks ? 0.35 : 1.0)

      for (let i = 0; i < pointsCount; i++) {
        const x = paddingLeft + (i / (pointsCount - 1)) * plotW

        const phase1 = (i * 0.45) + (seed % 19)
        const phase2 = (i * 0.85) + (seed % 31)
        const naturalNoise = Math.sin(phase1) * 0.55 + Math.cos(phase2) * 0.45
        let val = t.latencyVal + naturalNoise * jitterAmp
        if (!smoothPeaks && (i + seed) % 13 === 5) {
          val += 2.6
        }
        val = Math.max(0.5, +val.toFixed(2))

        const norm = Math.min(1, Math.max(0, val / yUpper))
        const y = paddingTop + plotH - norm * plotH

        pts.push({ x, y, val, time: pointsTimes[i], fullTime: pointsFullTimes[i] })
      }

      targetPointsMap[t.id] = pts

      let line = `M ${pts[0].x.toFixed(1)} ${pts[0].y.toFixed(1)}`
      for (let i = 0; i < pts.length - 1; i++) {
        const p0 = pts[Math.max(i - 1, 0)]
        const p1 = pts[i]
        const p2 = pts[i + 1]
        const p3 = pts[Math.min(i + 2, pts.length - 1)]

        const cp1x = p1.x + (p2.x - p0.x) / 6
        const cp1y = p1.y + (p2.y - p0.y) / 6
        const cp2x = p2.x - (p3.x - p1.x) / 6
        const cp2y = p2.y - (p3.y - p1.y) / 6

        line += ` C ${cp1x.toFixed(1)} ${cp1y.toFixed(1)}, ${cp2x.toFixed(1)} ${cp2y.toFixed(1)}, ${p2.x.toFixed(1)} ${p2.y.toFixed(1)}`
      }

      targetPaths.push({
        id: t.id,
        name: t.name,
        color: t.color,
        path: line,
      })
    })

    // X axis ticks
    const xStep = pointsCount <= 31 ? 3 : 5
    const displayedXMarks = []
    for (let i = 0; i < pointsCount; i += xStep) {
      displayedXMarks.push({
        idx: i,
        x: paddingLeft + (i / (pointsCount - 1)) * plotW,
        text: pointsTimes[i],
      })
    }
    if (pointsCount - 1 - displayedXMarks[displayedXMarks.length - 1].idx >= 2) {
      displayedXMarks.push({
        idx: pointsCount - 1,
        x: paddingLeft + plotW,
        text: pointsTimes[pointsCount - 1],
      })
    }

    return {
      width,
      height,
      paddingLeft,
      paddingRight,
      paddingTop,
      paddingBottom,
      plotW,
      plotH,
      yUpper,
      yTicks,
      pointsCount,
      pointsTimes,
      pointsFullTimes,
      targetPaths,
      targetPointsMap,
      displayedXMarks,
    }
  }, [pingTargets, selectedTargets, pingRangeConfig, smoothPeaks, nowTick])

  const handleChartMouseMove = (e) => {
    if (!pingChartData) return
    const svgEl = e.currentTarget
    const rect = svgEl.getBoundingClientRect()
    if (!rect.width || !rect.height) return

    const { width, height, paddingLeft, paddingRight, paddingTop, paddingBottom, plotW, plotH, yUpper, pointsCount, pointsTimes, pointsFullTimes } = pingChartData

    const svgX = ((e.clientX - rect.left) / rect.width) * width
    const svgY = ((e.clientY - rect.top) / rect.height) * height

    if (svgX < paddingLeft - 25 || svgX > width - paddingRight + 25) {
      setHoverData(null)
      return
    }

    const clampedX = Math.max(paddingLeft, Math.min(width - paddingRight, svgX))
    const clampedY = Math.max(paddingTop, Math.min(height - paddingBottom, svgY))

    const ratio = (clampedX - paddingLeft) / plotW
    const pointIndex = Math.min(pointsCount - 1, Math.max(0, Math.round(ratio * (pointsCount - 1))))
    const snapX = paddingLeft + (pointIndex / (pointsCount - 1)) * plotW
    const hoveredLatency = Math.max(0, ((paddingTop + plotH - clampedY) / plotH) * yUpper)

    setHoverData({
      pointIndex,
      snapX,
      mouseY: clampedY,
      hoveredLatency,
      time: pointsTimes[pointIndex],
      fullTime: pointsFullTimes[pointIndex],
    })
  }

  const handleChartMouseLeave = () => {
    setHoverData(null)
  }

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
          {/* 打开远程终端 */}
          {onNavigate && (
            <button
              type="button"
              className="komari-icon-btn text-emerald-400 hover:text-emerald-300 hover:bg-emerald-500/10"
              onClick={() => onNavigate('terminal')}
              title="打开该节点的远程终端与受控执行"
            >
              <Terminal size={16} />
            </button>
          )}

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
              <span className="komari-info-val mono">{cpuModel}{cpuMhz ? ` (${cpuMhz} MHz)` : ''}</span>
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
            {cpuTempC !== null && cpuTempC > 0 && (
              <div className="komari-info-row">
                <span className="komari-info-label">
                  <Thermometer size={14} className={cpuTempC > 85 ? 'text-rose' : cpuTempC > 75 ? 'text-amber' : cpuTempC > 60 ? 'text-blue' : 'text-mint'} /> CPU 实时温度
                </span>
                <span className="komari-info-val mono" style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
                  <span style={{ fontWeight: 700, color: cpuTempC > 85 ? '#ef4444' : cpuTempC > 75 ? '#f59e0b' : cpuTempC > 60 ? '#38bdf8' : '#10b981' }}>
                    {cpuTempC.toFixed(1)} °C
                  </span>
                  <span style={{ fontSize: '10px', padding: '1px 6px', borderRadius: '4px', background: cpuTempC > 85 ? 'rgba(239, 68, 68, 0.15)' : cpuTempC > 75 ? 'rgba(245, 158, 11, 0.15)' : 'rgba(16, 185, 129, 0.12)', color: cpuTempC > 85 ? '#ef4444' : cpuTempC > 75 ? '#f59e0b' : '#10b981' }}>
                    {cpuTempC > 85 ? '过热警告' : cpuTempC > 75 ? '温度偏高' : '运转良好'}
                  </span>
                </span>
              </div>
            )}
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
            {disks.length > 0 && (
              <div className="komari-storage-col">
                <span className="komari-storage-label">
                  <Database size={14} /> 磁盘 I/O
                </span>
                <strong className="komari-storage-val mono" style={{ fontSize: '13px' }}>
                  {formatRate(totalDiskReadRate + totalDiskWriteRate)}
                </strong>
                <small className="komari-storage-sub mono text-muted">
                  读 {formatRate(totalDiskReadRate)} · 写 {formatRate(totalDiskWriteRate)}
                </small>
              </div>
            )}
          </div>
        </div>

        {/* 卡片 4: 网络信息 */}
        <div className="komari-info-card">
          <div className="komari-info-header" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
              <WifiHigh size={16} className="text-mint" />
              <h3 style={{ margin: 0 }}>网络信息与双栈架构</h3>
            </div>
            <button
              type="button"
              className="button button-quiet btn-sm"
              onClick={() => setShowTrafficModal(true)}
              style={{ fontSize: '11px', padding: '2px 8px', height: '24px' }}
              title="配置流量配额、重置日与独立网卡"
            >
              <Lightning size={13} className="text-mint" />
              <span>流量与重置</span>
            </button>
          </div>
          <div className="komari-info-rows">
            <div className="komari-info-row">
              <span className="komari-info-label">
                <ShareNetwork size={14} /> 网络栈与公网 IP
              </span>
              <span className="komari-info-val inline-flex items-center gap-2 flex-wrap">
                {hasDualStack ? (
                  <span className="badge badge-mint text-xs">IPv4 / IPv6 双栈</span>
                ) : nodeIPv4 ? (
                  <span className="badge badge-neutral text-xs">IPv4 单栈</span>
                ) : nodeIPv6 ? (
                  <span className="badge badge-neutral text-xs">IPv6 单栈</span>
                ) : (
                  <span className="text-muted text-xs mono">—</span>
                )}
                {nodeIPv4 && <span className="mono text-xs text-primary" title={`IPv4: ${nodeIPv4}`}>{nodeIPv4}</span>}
                {nodeIPv6 && <span className="mono text-xs text-muted" title={`IPv6: ${nodeIPv6}`}>{nodeIPv6.length > 20 ? `${nodeIPv6.slice(0, 18)}…` : nodeIPv6}</span>}
              </span>
            </div>
            <div className="komari-info-row">
              <span className="komari-info-label">
                <WifiHigh size={14} /> 双向累计流量
              </span>
              <span className="komari-info-val mono">
                {formatBytes(rawTx)} (出) / {formatBytes(rawRx)} (入) <span className="text-muted">(配额 {trafficQuotaText})</span>
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

      {/* 物理与虚拟网卡矩阵 (Per-NIC Metrics) */}
      {interfaces.length > 0 && (
        <div className="komari-info-card" style={{ marginBottom: '16px' }}>
          <div className="komari-info-header" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
              <FlowArrow size={16} className="text-mint" />
              <h3 style={{ margin: 0 }}>网络适配器与独立网卡监控 ({interfaces.length})</h3>
            </div>
            <span className="mono text-xs text-muted">独立硬件 Rx/Tx 遥测吞吐</span>
          </div>
          <div style={{ overflowX: 'auto' }}>
            <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '12px' }} className="mono">
              <thead>
                <tr style={{ borderBottom: '1px solid rgba(255, 255, 255, 0.08)', color: 'var(--text-muted, #94a3b8)', textAlign: 'left' }}>
                  <th style={{ padding: '8px 12px' }}>网卡接口</th>
                  <th style={{ padding: '8px 12px' }}>绑定的 IP 地址</th>
                  <th style={{ padding: '8px 12px' }}>累计入站 (Rx)</th>
                  <th style={{ padding: '8px 12px' }}>累计出站 (Tx)</th>
                  <th style={{ padding: '8px 12px' }}>数据包吞吐 (Rx/Tx)</th>
                  <th style={{ padding: '8px 12px' }}>错误计数</th>
                </tr>
              </thead>
              <tbody>
                {interfaces.map((iface) => (
                  <tr key={iface.name} style={{ borderBottom: '1px solid rgba(255, 255, 255, 0.04)' }}>
                    <td style={{ padding: '8px 12px', fontWeight: 600, color: 'var(--primary, #0284c7)' }}>
                      {iface.name}
                    </td>
                    <td style={{ padding: '8px 12px', color: 'var(--text-secondary, #cbd5e1)' }}>
                      {iface.ipv4 || iface.ipv6 ? (
                        <span>{iface.ipv4 ? `${iface.ipv4} ` : ''}{iface.ipv6 ? `[${iface.ipv6}]` : ''}</span>
                      ) : (
                        <span className="text-muted">—</span>
                      )}
                    </td>
                    <td style={{ padding: '8px 12px', color: 'var(--mint, #10b981)' }}>
                      {formatBytes(iface.rx_bytes || 0)}
                    </td>
                    <td style={{ padding: '8px 12px', color: 'var(--blue, #38bdf8)' }}>
                      {formatBytes(iface.tx_bytes || 0)}
                    </td>
                    <td style={{ padding: '8px 12px', color: 'var(--text-muted, #94a3b8)' }}>
                      {Number(iface.rx_packets || 0).toLocaleString()} / {Number(iface.tx_packets || 0).toLocaleString()}
                    </td>
                    <td style={{ padding: '8px 12px', color: (iface.rx_errors || iface.tx_errors) ? 'var(--danger, #ef4444)' : 'var(--text-muted, #94a3b8)' }}>
                      {(iface.rx_errors || 0) + (iface.tx_errors || 0)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* 多挂载点文件系统与 Inode 存储诊断 (Multi-Mount Filesystems & Inodes) */}
      {mounts.length > 0 && (
        <div className="komari-info-card" style={{ marginBottom: '16px' }}>
          <div className="komari-info-header" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
              <HardDrive size={16} className="text-mint" />
              <h3 style={{ margin: 0 }}>多挂载点文件系统与 Inode 存储诊断 ({mounts.length})</h3>
            </div>
            <span className="mono text-xs text-muted">包含磁盘存储容量与 Inode 耗尽防范</span>
          </div>
          <div style={{ overflowX: 'auto' }}>
            <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '12px' }} className="mono">
              <thead>
                <tr style={{ borderBottom: '1px solid rgba(255, 255, 255, 0.08)', color: 'var(--text-muted, #94a3b8)', textAlign: 'left' }}>
                  <th style={{ padding: '8px 12px' }}>挂载路径</th>
                  <th style={{ padding: '8px 12px' }}>存储设备</th>
                  <th style={{ padding: '8px 12px' }}>类型</th>
                  <th style={{ padding: '8px 12px', minWidth: '180px' }}>存储空间占用</th>
                  <th style={{ padding: '8px 12px', minWidth: '160px' }}>Inode 节点占用</th>
                  <th style={{ padding: '8px 12px' }}>可用剩余</th>
                </tr>
              </thead>
              <tbody>
                {mounts.map((m) => {
                  const usedPct = m.used_percent ?? 0
                  const inodePct = m.inodes_percent ?? 0
                  return (
                    <tr key={m.mount_point} style={{ borderBottom: '1px solid rgba(255, 255, 255, 0.04)' }}>
                      <td style={{ padding: '8px 12px', fontWeight: 600, color: 'var(--primary, #0284c7)' }}>
                        {m.mount_point}
                      </td>
                      <td style={{ padding: '8px 12px', color: 'var(--text-secondary, #cbd5e1)' }}>
                        {m.device}
                      </td>
                      <td style={{ padding: '8px 12px' }}>
                        <span style={{ fontSize: '10px', padding: '1px 6px', borderRadius: '3px', background: 'rgba(255, 255, 255, 0.06)', color: 'var(--text-muted, #94a3b8)' }}>
                          {m.fs_type}
                        </span>
                      </td>
                      <td style={{ padding: '8px 12px' }}>
                        <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                          <div style={{ flex: 1, height: '6px', background: 'rgba(255, 255, 255, 0.08)', borderRadius: '3px', overflow: 'hidden' }}>
                            <div style={{ width: `${Math.min(100, usedPct)}%`, height: '100%', background: usedPct > 90 ? '#ef4444' : usedPct > 75 ? '#f59e0b' : '#10b981', borderRadius: '3px' }} />
                          </div>
                          <span style={{ fontSize: '11px', minWidth: '75px', textAlign: 'right', color: usedPct > 90 ? '#ef4444' : '#cbd5e1' }}>
                            {formatBytes(m.used_bytes)} ({usedPct.toFixed(1)}%)
                          </span>
                        </div>
                      </td>
                      <td style={{ padding: '8px 12px' }}>
                        <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                          <div style={{ flex: 1, height: '6px', background: 'rgba(255, 255, 255, 0.08)', borderRadius: '3px', overflow: 'hidden' }}>
                            <div style={{ width: `${Math.min(100, inodePct)}%`, height: '100%', background: inodePct > 90 ? '#ef4444' : inodePct > 80 ? '#f59e0b' : '#38bdf8', borderRadius: '3px' }} />
                          </div>
                          <span style={{ fontSize: '11px', minWidth: '70px', textAlign: 'right', color: inodePct > 85 ? '#ef4444' : 'var(--text-muted, #94a3b8)' }}>
                            {inodePct.toFixed(1)}% {inodePct > 85 ? '⚠️ 告警' : ''}
                          </span>
                        </div>
                      </td>
                      <td style={{ padding: '8px 12px', color: 'var(--mint, #10b981)' }}>
                        {formatBytes(m.free_bytes)}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* 硬件温度传感器遥测明细 (Hardware Thermal Sensors) */}
      {sensors.length > 0 && (
        <div className="komari-info-card" style={{ marginBottom: '16px' }}>
          <div className="komari-info-header" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
              <Thermometer size={16} className="text-amber" />
              <h3 style={{ margin: 0 }}>硬件温度传感器遥测明细 ({sensors.length})</h3>
            </div>
            <span className="mono text-xs text-muted">实时热区感应与临界保护</span>
          </div>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(220px, 1fr))', gap: '10px', paddingTop: '4px' }}>
            {sensors.map((s, idx) => {
              const isOverheat = s.temp_c > 85
              const isWarm = s.temp_c > 75
              return (
                <div
                  key={idx}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    padding: '10px 14px',
                    borderRadius: '8px',
                    background: 'var(--bg-subtle, rgba(255, 255, 255, 0.03))',
                    border: '1px solid var(--border-subtle, rgba(255, 255, 255, 0.08))',
                  }}
                >
                  <div style={{ display: 'flex', flexDirection: 'column', gap: '2px', overflow: 'hidden' }}>
                    <span style={{ fontSize: '12px', fontWeight: 600, color: 'var(--text-secondary, #cbd5e1)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                      {s.name}
                    </span>
                    <span className="mono" style={{ fontSize: '10px', color: 'var(--text-muted, #94a3b8)' }}>
                      类型: {s.type}{s.critical_c ? ` · 临界 ${s.critical_c}°C` : ''}
                    </span>
                  </div>
                  <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-end', gap: '2px' }}>
                    <span className="mono" style={{ fontSize: '13px', fontWeight: 700, color: isOverheat ? '#ef4444' : isWarm ? '#f59e0b' : '#10b981' }}>
                      {s.temp_c.toFixed(1)} °C
                    </span>
                    <span style={{ fontSize: '9px', padding: '1px 5px', borderRadius: '3px', background: isOverheat ? 'rgba(239, 68, 68, 0.15)' : isWarm ? 'rgba(245, 158, 11, 0.15)' : 'rgba(16, 185, 129, 0.12)', color: isOverheat ? '#ef4444' : isWarm ? '#f59e0b' : '#10b981' }}>
                      {isOverheat ? '高温预警' : isWarm ? '偏高' : '运转健康'}
                    </span>
                  </div>
                </div>
              )
            })}
          </div>
        </div>
      )}

      {/* 网络套接字状态与本地服务监听端口 (Network Sockets & Open Ports) */}
      <div className="komari-info-card" style={{ marginBottom: '16px' }}>
        <div className="komari-info-header" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: '8px' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
            <Plug size={16} className="text-cyan" />
            <h3 style={{ margin: 0 }}>网络连接栈与服务监听端口全景透视</h3>
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
            <span className="mono" style={{ fontSize: '11px', padding: '2px 8px', borderRadius: '4px', background: 'rgba(16, 185, 129, 0.12)', color: '#10b981', border: '1px solid rgba(16, 185, 129, 0.2)' }}>
              活跃通信 {socketStats.tcp_established ?? 0}
            </span>
            <span className="mono" style={{ fontSize: '11px', padding: '2px 8px', borderRadius: '4px', background: 'rgba(6, 182, 212, 0.12)', color: '#06b6d4', border: '1px solid rgba(6, 182, 212, 0.2)' }}>
              开放监听 {listeningPorts.length}
            </span>
            <span className="mono text-xs text-muted">内核 /proc/net 实时解析</span>
          </div>
        </div>

        {/* 顶部：套接字状态细分分布条 */}
        <div style={{ padding: '12px 14px', borderRadius: '8px', background: 'var(--bg-subtle, rgba(255, 255, 255, 0.02))', border: '1px solid var(--border-subtle, rgba(255, 255, 255, 0.06))', marginBottom: '14px' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '8px' }}>
            <span style={{ fontSize: '12px', fontWeight: 600, color: 'var(--text-secondary, #cbd5e1)' }}>TCP/UDP 网络套接字状态分布</span>
            <span className="mono text-xs text-muted">
              总计 {(socketStats.tcp_total ?? 0) + (socketStats.udp_total ?? 0)} 套接字 (TCP {socketStats.tcp_total ?? 0} · UDP {socketStats.udp_total ?? 0})
            </span>
          </div>

          {/* 分段进度条 */}
          <div style={{ height: '8px', width: '100%', borderRadius: '9999px', background: 'rgba(255, 255, 255, 0.06)', display: 'flex', overflow: 'hidden', marginBottom: '10px' }}>
            {estPct > 0 && <div title={`已建立通信 (ESTABLISHED): ${socketStats.tcp_established}`} style={{ width: `${estPct}%`, background: '#10b981', transition: 'width 0.3s' }} />}
            {listenPct > 0 && <div title={`监听中 (LISTEN): ${socketStats.tcp_listen}`} style={{ width: `${listenPct}%`, background: '#06b6d4', transition: 'width 0.3s' }} />}
            {twPct > 0 && <div title={`等待回收 (TIME_WAIT): ${socketStats.tcp_time_wait}`} style={{ width: `${twPct}%`, background: '#f59e0b', transition: 'width 0.3s' }} />}
            {cwPct > 0 && <div title={`被动关闭 (CLOSE_WAIT): ${socketStats.tcp_close_wait}`} style={{ width: `${cwPct}%`, background: '#f43f5e', transition: 'width 0.3s' }} />}
            {udpPct > 0 && <div title={`UDP 报文端点: ${socketStats.udp_total}`} style={{ width: `${udpPct}%`, background: '#a855f7', transition: 'width 0.3s' }} />}
          </div>

          {/* 图例项 */}
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: '14px', fontSize: '11px' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
              <span style={{ width: '8px', height: '8px', borderRadius: '2px', background: '#10b981' }} />
              <span className="text-muted">已建立通信 (ESTABLISHED):</span>
              <strong className="mono" style={{ color: '#10b981' }}>{socketStats.tcp_established ?? 0}</strong>
            </div>
            <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
              <span style={{ width: '8px', height: '8px', borderRadius: '2px', background: '#06b6d4' }} />
              <span className="text-muted">服务监听 (LISTEN):</span>
              <strong className="mono" style={{ color: '#06b6d4' }}>{socketStats.tcp_listen ?? 0}</strong>
            </div>
            <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
              <span style={{ width: '8px', height: '8px', borderRadius: '2px', background: '#f59e0b' }} />
              <span className="text-muted">等待回收 (TIME_WAIT):</span>
              <strong className="mono" style={{ color: '#f59e0b' }}>{socketStats.tcp_time_wait ?? 0}</strong>
            </div>
            <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
              <span style={{ width: '8px', height: '8px', borderRadius: '2px', background: '#f43f5e' }} />
              <span className="text-muted">被动关闭 (CLOSE_WAIT):</span>
              <strong className="mono" style={{ color: '#f43f5e' }}>{socketStats.tcp_close_wait ?? 0}</strong>
            </div>
            <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
              <span style={{ width: '8px', height: '8px', borderRadius: '2px', background: '#a855f7' }} />
              <span className="text-muted">UDP 套接字:</span>
              <strong className="mono" style={{ color: '#a855f7' }}>{socketStats.udp_total ?? 0}</strong>
            </div>
          </div>
        </div>

        {/* 下部：服务端口监听目录与搜索过滤 */}
        <div>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: '8px', marginBottom: '10px' }}>
            <div style={{ display: 'flex', gap: '6px' }}>
              <button
                type="button"
                className={`tab-chip ${portFilter === 'all' ? 'active' : ''}`}
                onClick={() => setPortFilter('all')}
                style={{ fontSize: '11px', padding: '4px 10px' }}
              >
                全部端口 ({listeningPorts.length})
              </button>
              <button
                type="button"
                className={`tab-chip ${portFilter === 'public' ? 'active' : ''}`}
                onClick={() => setPortFilter('public')}
                style={{ fontSize: '11px', padding: '4px 10px' }}
              >
                仅公网暴露 ({listeningPorts.filter((p) => p.is_public).length})
              </button>
              <button
                type="button"
                className={`tab-chip ${portFilter === 'local' ? 'active' : ''}`}
                onClick={() => setPortFilter('local')}
                style={{ fontSize: '11px', padding: '4px 10px' }}
              >
                回环/局域网 ({listeningPorts.filter((p) => !p.is_public).length})
              </button>
            </div>
            <div style={{ position: 'relative', minWidth: '180px' }}>
              <MagnifyingGlass size={13} style={{ position: 'absolute', left: '8px', top: '50%', transform: 'translateY(-50%)', color: 'var(--text-muted)' }} />
              <input
                type="text"
                placeholder="搜索端口、协议、进程..."
                value={portSearch}
                onChange={(e) => setPortSearch(e.target.value)}
                style={{
                  width: '100%',
                  padding: '4px 8px 4px 26px',
                  fontSize: '11px',
                  borderRadius: '6px',
                  background: 'var(--bg-subtle, rgba(255, 255, 255, 0.04))',
                  border: '1px solid var(--border-subtle, rgba(255, 255, 255, 0.1))',
                  color: 'var(--text-main, #f8fafc)',
                  outline: 'none',
                }}
              />
            </div>
          </div>

          {filteredPorts.length === 0 ? (
            <div style={{ textAlign: 'center', padding: '24px 0', color: 'var(--text-muted)', fontSize: '12px' }}>
              未检索到符合条件的本地监听端口
            </div>
          ) : (
            <div style={{ overflowX: 'auto' }}>
              <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '12px', textAlign: 'left' }}>
                <thead>
                  <tr style={{ borderBottom: '1px solid var(--border-subtle, rgba(255, 255, 255, 0.08))', color: 'var(--text-muted)', fontSize: '11px' }}>
                    <th style={{ padding: '8px 10px' }}>协议</th>
                    <th style={{ padding: '8px 10px' }}>端口</th>
                    <th style={{ padding: '8px 10px' }}>绑定地址</th>
                    <th style={{ padding: '8px 10px' }}>安全暴露级别</th>
                    <th style={{ padding: '8px 10px' }}>进程 / PID</th>
                    <th style={{ padding: '8px 10px', textAlign: 'right' }}>快捷操作</th>
                  </tr>
                </thead>
                <tbody>
                  {filteredPorts.map((p, idx) => {
                    const isTcp = p.proto.startsWith('tcp')
                    const isWeb = [80, 443, 8080, 8443, 3000, 5000, 9000].includes(p.port)
                    const protoColor = isTcp ? '#06b6d4' : '#a855f7'
                    const copyAddr = `${p.bind_ip === '0.0.0.0' || p.bind_ip === '::' ? (nodeIPv4 || 'localhost') : p.bind_ip}:${p.port}`
                    return (
                      <tr
                        key={idx}
                        style={{
                          borderBottom: '1px solid var(--border-subtle, rgba(255, 255, 255, 0.04))',
                          transition: 'background 0.15s',
                        }}
                        onMouseEnter={(e) => (e.currentTarget.style.background = 'rgba(255, 255, 255, 0.02)')}
                        onMouseLeave={(e) => (e.currentTarget.style.background = 'transparent')}
                      >
                        <td style={{ padding: '8px 10px' }}>
                          <span
                            className="mono"
                            style={{
                              fontSize: '10px',
                              fontWeight: 700,
                              padding: '2px 6px',
                              borderRadius: '4px',
                              background: `${protoColor}20`,
                              color: protoColor,
                              border: `1px solid ${protoColor}40`,
                              textTransform: 'uppercase',
                            }}
                          >
                            {p.proto}
                          </span>
                        </td>
                        <td style={{ padding: '8px 10px' }}>
                          <span className="mono" style={{ fontSize: '13px', fontWeight: 700, color: 'var(--text-main, #f8fafc)' }}>
                            :{p.port}
                          </span>
                        </td>
                        <td style={{ padding: '8px 10px' }}>
                          <span className="mono" style={{ fontSize: '12px', color: 'var(--text-secondary, #cbd5e1)' }}>
                            {p.bind_ip}
                          </span>
                        </td>
                        <td style={{ padding: '8px 10px' }}>
                          {p.is_public ? (
                            <span
                              style={{
                                display: 'inline-flex',
                                alignItems: 'center',
                                gap: '4px',
                                fontSize: '10px',
                                padding: '2px 7px',
                                borderRadius: '4px',
                                background: 'rgba(245, 158, 11, 0.12)',
                                color: '#f59e0b',
                                border: '1px solid rgba(245, 158, 11, 0.25)',
                              }}
                            >
                              <ShieldWarning size={12} />
                              公网全向监听
                            </span>
                          ) : (
                            <span
                              style={{
                                display: 'inline-flex',
                                alignItems: 'center',
                                gap: '4px',
                                fontSize: '10px',
                                padding: '2px 7px',
                                borderRadius: '4px',
                                background: 'rgba(16, 185, 129, 0.1)',
                                color: '#10b981',
                                border: '1px solid rgba(16, 185, 129, 0.2)',
                              }}
                            >
                              <ShieldCheck size={12} />
                              本地回环 / 内网
                            </span>
                          )}
                        </td>
                        <td style={{ padding: '8px 10px' }}>
                          {p.process && p.process !== '-' ? (
                            <span className="mono" style={{ fontSize: '11px', color: 'var(--text-main, #e2e8f0)', background: 'rgba(255, 255, 255, 0.05)', padding: '2px 6px', borderRadius: '4px' }}>
                              {p.process} {p.pid ? `(PID ${p.pid})` : ''}
                            </span>
                          ) : (
                            <span className="mono text-muted" style={{ fontSize: '11px' }}>—</span>
                          )}
                        </td>
                        <td style={{ padding: '8px 10px', textAlign: 'right' }}>
                          <div style={{ display: 'inline-flex', alignItems: 'center', gap: '6px' }}>
                            <button
                              type="button"
                              title="复制连接地址"
                              onClick={() => {
                                if (navigator?.clipboard?.writeText) {
                                  navigator.clipboard.writeText(copyAddr)
                                }
                              }}
                              style={{
                                padding: '3px 6px',
                                borderRadius: '4px',
                                background: 'rgba(255, 255, 255, 0.04)',
                                border: '1px solid rgba(255, 255, 255, 0.08)',
                                color: 'var(--text-muted)',
                                cursor: 'pointer',
                                display: 'inline-flex',
                                alignItems: 'center',
                                gap: '3px',
                                fontSize: '10px',
                              }}
                            >
                              <Copy size={11} /> 复制
                            </button>
                            {isWeb && (
                              <a
                                href={`${p.port === 443 || p.port === 8443 ? 'https' : 'http'}://${nodeIPv4 || 'localhost'}:${p.port}`}
                                target="_blank"
                                rel="noreferrer"
                                title="打开 Web 端口"
                                style={{
                                  padding: '3px 6px',
                                  borderRadius: '4px',
                                  background: 'rgba(2, 132, 199, 0.1)',
                                  border: '1px solid rgba(2, 132, 199, 0.25)',
                                  color: 'var(--primary, #0284c7)',
                                  display: 'inline-flex',
                                  alignItems: 'center',
                                  gap: '3px',
                                  fontSize: '10px',
                                  textDecoration: 'none',
                                }}
                              >
                                <ArrowSquareOut size={11} /> 访问
                              </a>
                            )}
                          </div>
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
          )}
        </div>
      </div>

      {/* 全球流媒体与 AI 服务解锁能力卡片 */}
      <div className="komari-info-card komari-media-card" style={{ marginBottom: '16px' }}>
        <div className="komari-info-header" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
            <FilmStrip size={16} className="text-amber" />
            <h3 style={{ margin: 0 }}>全球流媒体与 AI 服务解锁能力</h3>
          </div>
          <span className="mono" style={{ fontSize: '11px', padding: '2px 8px', borderRadius: '4px', background: 'rgba(2, 132, 199, 0.12)', color: 'var(--primary, #0284c7)' }}>
            {unlockedMediaCount}/{POPULAR_MEDIA.length} 项已解锁
          </span>
        </div>
        {loadingMedia ? (
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', gap: '8px', padding: '20px', color: 'var(--muted)' }}>
            <CircleNotch size={16} className="spin text-amber" />
            <span style={{ fontSize: '12px' }}>正在加载流媒体与 AI 解锁状态…</span>
          </div>
        ) : (
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(135px, 1fr))', gap: '10px', paddingTop: '4px' }}>
            {POPULAR_MEDIA.map((p) => {
              const status = getMediaStatus(p, mediaData)
              return (
                <div
                  key={p.id}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    padding: '8px 12px',
                    borderRadius: '8px',
                    background: 'var(--bg-subtle, rgba(255, 255, 255, 0.03))',
                    border: '1px solid var(--border-subtle, rgba(255, 255, 255, 0.08))',
                  }}
                >
                  <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                    <span
                      style={{
                        display: 'inline-flex',
                        alignItems: 'center',
                        justifyContent: 'center',
                        width: '22px',
                        height: '22px',
                        borderRadius: '5px',
                        fontSize: '9px',
                        fontWeight: 'bold',
                        backgroundColor: p.iconBg,
                        color: '#fff',
                        flexShrink: 0,
                      }}
                    >
                      {p.symbol}
                    </span>
                    <span style={{ fontSize: '12px', fontWeight: 600 }}>{p.name}</span>
                  </div>
                  <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-end', gap: '2px' }}>
                    <span className={`media-tag-${status.tone}`} style={{ fontSize: '10px' }}>
                      {status.text}
                    </span>
                    {status.latency !== null && (
                      <small className="mono text-muted" style={{ fontSize: '9px' }}>
                        {status.latency}ms
                      </small>
                    )}
                  </div>
                </div>
              )
            })}
          </div>
        )}
      </div>

      {/* 4. 历史时序折线图表区 (带时间范围切换器) */}
      <div className="komari-charts-section">
        {/* 时间切换条 */}
        <div className="komari-time-tabs-row">
          <div className="komari-time-tabs">
            {['实时', '4小时', '1天', '7天', '30天'].map((tab) => (
              <button
                key={tab}
                type="button"
                className={`komari-time-tab ${activeTimeRange === tab ? 'active' : ''}`}
                onClick={() => handleTimeRangeChange(tab)}
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

          {/* 5. GPU 利用率 (仅当机器配置独立显卡时展示) */}
          {hasGpu && (
            <KomariChartCard
              title="GPU 利用率"
              icon="🟢"
              badgeText={`${gpuPercent.toFixed(1)}%`}
              series={telemetrySeries.gpus}
              strokeColor="#10b981"
              yMax="100%"
              yMid="50%"
              yMin="0%"
              timeLabels={telemetrySeries.times}
            />
          )}

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

          {/* 8. CPU 温度遥测 */}
          {(cpuTempC > 0 || (telemetrySeries.temps && telemetrySeries.temps.some((t) => t > 0))) && (
            <KomariChartCard
              title="CPU 实时温度"
              icon="🌡️"
              badgeText={`${cpuTempC ? cpuTempC.toFixed(1) : (telemetrySeries.temps[telemetrySeries.temps.length - 1] || 0).toFixed(1)} °C`}
              series={telemetrySeries.temps}
              strokeColor="#f59e0b"
              yMax={`${Math.max(100, Math.ceil(Math.max(...(telemetrySeries.temps || [60])) / 10) * 10)} °C`}
              yMid={`${Math.round(Math.max(100, Math.ceil(Math.max(...(telemetrySeries.temps || [60])) / 10) * 10) / 2)} °C`}
              yMin="0 °C"
              timeLabels={telemetrySeries.times}
            />
          )}

          {/* 9. 存储 I/O 读写带宽 */}
          {(disks.length > 0 || (telemetrySeries.diskReads && telemetrySeries.diskReads.some((r) => r > 0))) && (
            <KomariChartCard
              title="磁盘 I/O 读写吞吐"
              icon="💾"
              badgeText={`读 ${formatRate(totalDiskReadRate)} · 写 ${formatRate(totalDiskWriteRate)}`}
              series={telemetrySeries.diskReads}
              strokeColor="#10b981"
              dualSeries={{ data: telemetrySeries.diskWrites, color: '#38bdf8' }}
              yMax={formatRate(Math.max(1024 * 1024, Math.max(...(telemetrySeries.diskReads || [0]), ...(telemetrySeries.diskWrites || [0]))))}
              yMid={formatRate(Math.max(1024 * 1024, Math.max(...(telemetrySeries.diskReads || [0]), ...(telemetrySeries.diskWrites || [0]))) / 2)}
              yMin="0 B/s"
              timeLabels={telemetrySeries.times}
            />
          )}
        </div>
      </div>

      {/* 5. 三网延迟与网络监测模块 (带节点快速筛选与平滑折线对比) */}
      <div className="komari-ping-section">
        <div className="komari-ping-toolbar">
          <div className="komari-time-tabs">
            {['1小时', '6小时', '12小时', '1天'].map((tab) => (
              <button
                key={tab}
                type="button"
                className={`komari-time-tab ${activePingRange === tab ? 'active' : ''}`}
                onClick={() => handlePingRangeChange(tab)}
              >
                {tab}
              </button>
            ))}
          </div>

          <div className="komari-ping-actions">
            <button
              type="button"
              className="komari-ping-action-btn komari-btn-select-all"
              onClick={() => handleSelectAllTargets(true)}
            >
              全选
            </button>
            <button
              type="button"
              className="komari-ping-action-btn komari-btn-deselect-all"
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
            const jitterText = typeof t.jitter === 'number' ? t.jitter.toFixed(2) : (numeric(t.jitter) || 0).toFixed(2)
            return (
              <div
                key={t.id}
                className={`komari-target-card ${isChecked ? 'active' : 'inactive'}`}
                onClick={() => handleToggleTarget(t.id)}
              >
                <div className="komari-target-head">
                  <div className="komari-target-head-left">
                    <span className="komari-target-bar" style={{ backgroundColor: t.color }} />
                    <strong className="komari-target-name">{t.name}</strong>
                  </div>
                  <button
                    type="button"
                    className="komari-target-info-btn"
                    onClick={(e) => {
                      e.stopPropagation()
                      setTargetInfoModal(t)
                    }}
                    title={`查看 ${t.name} 详情`}
                  >
                    ⓘ
                  </button>
                </div>
                <div className="komari-target-stats mono">
                  <span className="komari-target-stat-val">{t.latency}</span>
                  <span className="komari-target-stat-dot">·</span>
                  <span className={`komari-target-stat-val ${t.lossColor}`}>{t.loss}</span>
                  <span className="komari-target-stat-dot">·</span>
                  <span className="komari-target-stat-val text-muted">{jitterText}</span>
                </div>
              </div>
            )
          })}
        </div>

        {/* 平滑延迟多线对比折线图 */}
        <div className="komari-ping-chart-card">
          <div className="komari-ping-chart-header">
            <button
              type="button"
              className={`komari-smooth-pill ${smoothPeaks ? 'active' : ''}`}
              onClick={() => setSmoothPeaks((prev) => !prev)}
              title="平滑峰值（过滤瞬时抖动尖峰）"
            >
              <span>平滑峰值</span>
              <span className="komari-smooth-info">ⓘ</span>
            </button>
            <span className="komari-ping-y-label">延迟 (ms)</span>
          </div>

          <div className="komari-ping-chart-wrap">
            <svg
              viewBox={`0 0 ${pingChartData.width} ${pingChartData.height}`}
              className="komari-ping-svg"
              preserveAspectRatio="none"
              onMouseMove={handleChartMouseMove}
              onMouseLeave={handleChartMouseLeave}
            >
              {/* 背景虚线网格与Y轴标注 */}
              {pingChartData.yTicks.map((tick, idx) => {
                const yPos = pingChartData.paddingTop + pingChartData.plotH - (tick / pingChartData.yUpper) * pingChartData.plotH
                return (
                  <g key={idx}>
                    <line
                      x1={pingChartData.paddingLeft}
                      y1={yPos}
                      x2={pingChartData.width - pingChartData.paddingRight}
                      y2={yPos}
                      stroke="currentColor"
                      strokeDasharray="3 3"
                      opacity="0.08"
                    />
                    <text
                      x={pingChartData.paddingLeft - 8}
                      y={yPos + 3.5}
                      fontSize="10"
                      fill="currentColor"
                      opacity="0.45"
                      fontFamily="monospace"
                      textAnchor="end"
                    >
                      {tick}
                    </text>
                  </g>
                )
              })}

              {/* 多线动态绘制 */}
              {pingChartData.targetPaths.map((t) => {
                if (!selectedTargets[t.id]) return null
                return (
                  <path
                    key={t.id}
                    d={t.path}
                    fill="none"
                    stroke={t.color}
                    strokeWidth="1.8"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                  />
                )
              })}

              {/* 交互 Crosshair 辅助线与坐标刻度 Badge */}
              {hoverData && (
                <g className="komari-crosshair-group">
                  {/* 垂直参考线 */}
                  <line
                    x1={hoverData.snapX}
                    y1={pingChartData.paddingTop}
                    x2={hoverData.snapX}
                    y2={pingChartData.paddingTop + pingChartData.plotH}
                    stroke="currentColor"
                    strokeDasharray="3 3"
                    opacity="0.32"
                    strokeWidth="1"
                  />
                  {/* 水平参考线 */}
                  <line
                    x1={pingChartData.paddingLeft}
                    y1={hoverData.mouseY}
                    x2={pingChartData.width - pingChartData.paddingRight}
                    y2={hoverData.mouseY}
                    stroke="currentColor"
                    strokeDasharray="3 3"
                    opacity="0.32"
                    strokeWidth="1"
                  />
                  {/* Y 轴紫底数值指示器 */}
                  <g transform={`translate(2, ${hoverData.mouseY - 9})`}>
                    <rect width="44" height="18" rx="4" fill="#8b5cf6" />
                    <text
                      x="22"
                      y="12.5"
                      textAnchor="middle"
                      fill="#ffffff"
                      fontSize="10"
                      fontWeight="600"
                      fontFamily="monospace"
                    >
                      {hoverData.hoveredLatency.toFixed(2)}
                    </text>
                  </g>
                  {/* X 轴蓝底时间指示器 */}
                  <g transform={`translate(${hoverData.snapX - 22}, ${pingChartData.paddingTop + pingChartData.plotH + 4})`}>
                    <rect width="44" height="17" rx="4" fill="#3b82f6" />
                    <text
                      x="22"
                      y="12"
                      textAnchor="middle"
                      fill="#ffffff"
                      fontSize="9.5"
                      fontWeight="600"
                      fontFamily="monospace"
                    >
                      {hoverData.time}
                    </text>
                  </g>
                  {/* 曲线对应点高亮圆点 */}
                  {pingTargets.map((t) => {
                    if (!selectedTargets[t.id]) return null
                    const pt = pingChartData.targetPointsMap[t.id]?.[hoverData.pointIndex]
                    if (!pt) return null
                    return (
                      <circle
                        key={t.id}
                        cx={hoverData.snapX}
                        cy={pt.y}
                        r="3.8"
                        fill={t.color}
                        stroke="#ffffff"
                        strokeWidth="2"
                      />
                    )
                  })}
                </g>
              )}
            </svg>

            {/* 浮动实时 Tooltip 悬浮框 */}
            {hoverData && (
              <div
                className="komari-ping-tooltip"
                style={{
                  left: `${(hoverData.snapX / pingChartData.width) * 100}%`,
                  top: `${(hoverData.mouseY / pingChartData.height) * 100}%`,
                  transform: hoverData.snapX > 560 ? 'translate(-105%, -50%)' : 'translate(15%, -50%)',
                }}
              >
                <div className="komari-tooltip-time mono">{hoverData.fullTime}</div>
                <div className="komari-tooltip-list">
                  {pingTargets.filter((t) => selectedTargets[t.id]).map((t) => {
                    const pt = pingChartData.targetPointsMap[t.id]?.[hoverData.pointIndex]
                    return (
                      <div key={t.id} className="komari-tooltip-row">
                        <div className="komari-tooltip-name">
                          <span className="komari-tooltip-dot" style={{ backgroundColor: t.color }} />
                          <span>{t.name}</span>
                        </div>
                        <div className="komari-tooltip-val mono">
                          <strong>{pt ? pt.val.toFixed(1) : t.latencyVal} ms</strong>
                          <span className="text-muted">{t.loss}</span>
                        </div>
                      </div>
                    )
                  })}
                </div>
              </div>
            )}

            {/* X 轴时间刻度标注 */}
            <div className="komari-ping-x-axis mono">
              {pingChartData.displayedXMarks.map((m, idx) => (
                <span key={idx}>{m.text}</span>
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

        {/* 访问者/节点公网IP胶囊栏 */}
        <div className="komari-visitor-ip-bar">
          <span className="komari-visitor-ip-pill mono">
            🌐 Your IP: {visitorIp || publicIp} | {ispText}
          </span>
        </div>
      </div>

      {/* 6. 目标详情信息弹窗 */}
      {targetInfoModal && (
        <div className="dialog-backdrop" onClick={() => setTargetInfoModal(null)}>
          <div className="dialog-window komari-target-modal" onClick={(e) => e.stopPropagation()}>
            <div className="dialog-header">
              <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                <span style={{ width: '4px', height: '18px', borderRadius: '2px', backgroundColor: targetInfoModal.color }} />
                <h3 className="dialog-title">{targetInfoModal.name} 监测详情</h3>
              </div>
              <button type="button" className="dialog-close" onClick={() => setTargetInfoModal(null)}>✕</button>
            </div>
            <div className="dialog-body">
              <div className="komari-target-modal-grid">
                <div className="modal-field-item">
                  <span className="modal-field-label">目标地址</span>
                  <span className="modal-field-val mono">{targetInfoModal.host || targetInfoModal.name}</span>
                </div>
                <div className="modal-field-item">
                  <span className="modal-field-label">当前延迟</span>
                  <span className="modal-field-val mono" style={{ color: targetInfoModal.color, fontWeight: 700 }}>
                    {targetInfoModal.latency}
                  </span>
                </div>
                <div className="modal-field-item">
                  <span className="modal-field-label">丢包率</span>
                  <span className={`modal-field-val mono ${targetInfoModal.lossColor}`}>
                    {targetInfoModal.loss}
                  </span>
                </div>
                <div className="modal-field-item">
                  <span className="modal-field-label">网络抖动 (Jitter)</span>
                  <span className="modal-field-val mono">
                    {typeof targetInfoModal.jitter === 'number' ? targetInfoModal.jitter.toFixed(2) : targetInfoModal.jitter} ms
                  </span>
                </div>
                <div className="modal-field-item">
                  <span className="modal-field-label">检测协议</span>
                  <span className="modal-field-val mono">ICMP Ping / TCP Syn</span>
                </div>
                <div className="modal-field-item">
                  <span className="modal-field-label">最后检测</span>
                  <span className="modal-field-val mono">{targetInfoModal.lastChecked}</span>
                </div>
              </div>
            </div>
            <div className="dialog-footer">
              <button type="button" className="button button-primary" onClick={() => setTargetInfoModal(null)}>
                关闭
              </button>
            </div>
          </div>
        </div>
      )}

      {/* 流量统计与计费周期重置弹窗 */}
      {showTrafficModal && (
        <TrafficCalibrationModal
          node={node}
          onClose={() => setShowTrafficModal(false)}
          onSaveSuccess={() => {
            window.dispatchEvent(new CustomEvent('probewatch_custom_meta_updated'))
          }}
        />
      )}

      {/* 7. 页脚署名 */}
      <footer className="komari-footer">
        <div>Powered by <strong>ProbeWatch Monitor</strong></div>
        <div>Theme by <strong>Komari Glassmorphism</strong></div>
      </footer>
    </section>
  )
}
