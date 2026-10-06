import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  ArrowLeft,
  CaretLeft,
  CaretRight,
  Star,
  Check,
  Copy,
  ClipboardText,
  Rows,
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
  Lightning,
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
  Shield,
  Gauge,
  Play,
} from '@phosphor-icons/react'

// Favorites are session-only by design. Node identifiers must not be written
// to browser storage, and this keeps the preference private to this tab.
const inMemoryFavoriteNodes = new Set()
import {
  formatBytes,
  formatRate,
  relativeHeartbeat,
  safeText,
  formatLoad,
  numeric,
  cleanTargetLabel,
} from '../lib/format.js'
import {
  calculateRemainingValue,
  getNodeBilling,
  getNodeCustomMeta,
  parseColoredTags,
} from '../lib/billing.js'
import { DistroIcon } from './Common.jsx'
import { TrafficCalibrationModal } from './TrafficCalibrationModal.jsx'
import { PosterModal } from './PosterModal.jsx'
import { fetchPublicNodeDetail } from '../lib/api.js'

// Helper for generating smooth SVG bezier paths
function generateSplinePath(points, width = 450, height = 110, padding = 12) {
  const validPoints = (points || []).filter((value) => typeof value === 'number' && Number.isFinite(value))
  if (validPoints.length === 0) return { line: '', area: '' }
  points = validPoints
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

function formatIPQualityType(type) {
  if (!type) return '未知 / 未识别'
  const t = String(type).trim().toLowerCase()
  if (t === 'hosting' || t === 'datacenter') return '机房'
  if (t === 'isp') return 'ISP'
  if (t === 'residential') return '家宽'
  if (t === 'unknown') return '未知 / 未识别'
  return type
}

const IP_QUALITY_SOURCE_MAP = {
  risk_score: '综合风险',
  fraud_score: '欺诈风险',
  abuse_score: '滥用风险',
  threat_score: '威胁分',
  scamalytics: 'Scamalytics',
  ip2location: 'IP2Location',
  abuseipdb: 'AbuseIPDB',
  ipqs: 'IPQS',
  dbip: 'DB-IP',
}

function formatIPQualitySourceName(key) {
  if (!key) return '—'
  if (IP_QUALITY_SOURCE_MAP[key]) return IP_QUALITY_SOURCE_MAP[key]
  return String(key).replace(/_/g, ' ')
}

function formatIPQualityDateTime(timestamp) {
  if (!timestamp) return '未知'
  const ms = Number(timestamp) > 1e11 ? Number(timestamp) : Number(timestamp) * 1000
  const d = new Date(ms)
  if (isNaN(d.getTime())) return '未知'
  const pad = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

function formatIPQualityShortTime(timestamp) {
  if (!timestamp) return '—'
  const ms = Number(timestamp) > 1e11 ? Number(timestamp) : Number(timestamp) * 1000
  const d = new Date(ms)
  if (isNaN(d.getTime())) return '—'
  const pad = (n) => String(n).padStart(2, '0')
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
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

          {(!series || !series.some((v) => typeof v === 'number' && Number.isFinite(v))) && (
            <div style={{ position: 'absolute', inset: 0, display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: '11px', color: 'var(--text-muted, #94a3b8)', pointerEvents: 'none' }}>
              暂无历史采样数据
            </div>
          )}

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
    const dName = (m.detector || m.result?.detector || '').toLowerCase()
    return aliases.some((a) => dId.includes(a) || dName.includes(a))
  })
  if (!match) return { text: '未测试', tone: 'muted', latency: null }
  const res = match.result || match || {}
  const status = res.status || match.status
  const latency = res.latency_ms ?? match.latency_ms ?? null
  const region = res.region || match.region
  const reason = res.reason || match.reason

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
      text: reason === 'body exceeds limit' ? '仅自制剧' : '超时/异常',
      tone: 'warning',
      latency,
    }
  }
  return { text: status || '未知', tone: 'muted', latency }
}

export function NodeDetailPage({
  node,
  nodeUuid: propNodeUuid,
  nodes = [],
  onSelectNode,
  loading = false,
  history = [],
  historyLoading = false,
  historyTimeRange = '实时',
  onHistoryTimeRangeChange,
  checksSummary = null,
  pingHistory = [],
  checksLoading = false,
  pingTimeRange = '1小时',
  onPingTimeRangeChange,
  checksError = null,
  traffic = null,
  trafficLoading = false,
  trafficError = null,
  trafficPeriod = 'day',
  onTrafficPeriodChange,
  historyError = null,
  onBack,
  rates = {},
  clientInfo = null,
  isPublic = false,
  onNavigate,
}) {
  const [copied, setCopied] = useState(false)
  const [posterCopied, setPosterCopied] = useState(false)
  const [markdownCopied, setMarkdownCopied] = useState(false)
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
  const [showPublicNodeCard, setShowPublicNodeCard] = useState(true)
  const [mobileCompact, setMobileCompact] = useState(false)
  // 1-second live clock ticker for dynamic real-time uptime, heartbeats, and chart axes
  const [nowTick, setNowTick] = useState(() => Date.now())
  useEffect(() => {
    const timer = setInterval(() => setNowTick(Date.now()), 1000)
    return () => clearInterval(timer)
  }, [])

  const nodeUuid = propNodeUuid || node?.uuid || node?.id || ''
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

  const handleSharePoster = () => {
    setShowPosterModal(true)
  }

  const handleCopyShareMarkdown = async () => {
    const name = customMeta.customName || node.name || 'ProbeWatch 节点'
    const status = isOnline ? '在线' : '离线'
    const cpu = cpuPercent === null ? '暂无' : `${Math.round(cpuPercent)}%`
    const memory = memTotal ? `${Math.round((memUsed / memTotal) * 100)}%` : '暂无'
    const disk = diskTotal ? `${Math.round((diskUsed / diskTotal) * 100)}%` : '暂无'
    const mediaBadges = POPULAR_MEDIA.map((item) => {
      const result = getMediaStatus(item, mediaData)
      return `${result.tone === 'available' ? '✅' : '❌'} ${item.label || item.name}`
    }).join(' · ')
    const markdown = [
      `### ${node.flag || '🌐'} ${name}`,
      `**状态：** ${status} · ${heartbeatText}`,
      '',
      `| CPU | 内存 | 磁盘 | 剩余价值 | 流媒体解锁 |`,
      `|---:|---:|---:|---:|---:|`,
      `| ${cpu} | ${memory} | ${disk} | ${remainingValue} | ${unlockedMediaCount}/${POPULAR_MEDIA.length} |`,
      '',
      `**解锁：** ${mediaBadges}`,
      '',
      `> ProbeWatch · 生成于 ${new Date().toLocaleString('zh-CN')}`,
    ].join('\n')
    try {
      await navigator.clipboard?.writeText(markdown)
      setMarkdownCopied(true)
      setTimeout(() => setMarkdownCopied(false), 2400)
    } catch {
      const blob = new Blob([markdown], { type: 'text/markdown;charset=utf-8' })
      const url = URL.createObjectURL(blob)
      const anchor = document.createElement('a')
      anchor.href = url
      anchor.download = `probewatch-${name.replace(/[^\w\u4e00-\u9fff-]+/g, '-')}.md`
      anchor.click()
      URL.revokeObjectURL(url)
      setMarkdownCopied(true)
      setTimeout(() => setMarkdownCopied(false), 2400)
    }
  }

  // Keep the favorite state in memory for the current console session.
  useEffect(() => {
    setIsFavorite(Boolean(nodeUuid && inMemoryFavoriteNodes.has(nodeUuid)))
  }, [nodeUuid])

  const toggleFavorite = () => {
    if (!nodeUuid) return
    if (isFavorite) inMemoryFavoriteNodes.delete(nodeUuid)
    else inMemoryFavoriteNodes.add(nodeUuid)
    setIsFavorite(inMemoryFavoriteNodes.has(nodeUuid))
  }

  const [mediaData, setMediaData] = useState([])
  const [showPosterModal, setShowPosterModal] = useState(false)
  const [publicDetail, setPublicDetail] = useState(null)
  const [publicDetailLoading, setPublicDetailLoading] = useState(Boolean(isPublic && nodeUuid))
  const [publicDetailError, setPublicDetailError] = useState(null)
  const [lastSyncTime, setLastSyncTime] = useState(null)
  const [showAllMedia, setShowAllMedia] = useState(false)

  // Track recently visited nodes for quick access
  useEffect(() => {
    if (!nodeUuid) return
    try {
      const raw = localStorage.getItem('probewatch:recent-nodes') || '[]'
      const list = JSON.parse(raw).filter((id) => id !== nodeUuid)
      list.unshift(nodeUuid)
      localStorage.setItem('probewatch:recent-nodes', JSON.stringify(list.slice(0, 10)))
    } catch {}
  }, [nodeUuid])

  const [loadingMedia, setLoadingMedia] = useState(false)
  const [ipQuality, setIpQuality] = useState(null)
  const [loadingIpQuality, setLoadingIpQuality] = useState(false)

  const loadPublicDetail = useCallback(async (signal) => {
    if (!nodeUuid) return
    setPublicDetailLoading(true)
    setPublicDetailError(null)
    try {
      const data = await fetchPublicNodeDetail(nodeUuid, signal)
      if (data && typeof data === 'object') {
        setPublicDetail(data)
        setLastSyncTime(new Date())
        if (Array.isArray(data.media)) {
          setMediaData(data.media)
        }
        const quality = data.ip_quality
        if (quality && typeof quality === 'object') {
          setIpQuality(quality)
        } else {
          setIpQuality(null)
        }
      }
    } catch (err) {
      if (err?.name !== 'AbortError') {
        const is404 = err?.status === 404 || String(err?.message || '').includes('404')
        const is401 = err?.status === 401 || String(err?.message || '').includes('401')
        setPublicDetailError(is404 ? 'not_found' : is401 ? 'unauthorized' : 'request_failed')
      }
    } finally {
      setPublicDetailLoading(false)
    }
  }, [nodeUuid])

  useEffect(() => {
    if (isPublic && nodeUuid) {
      setPublicDetail(null)
      setIpQuality(null)
      setPublicDetailError(null)
      setPublicDetailLoading(true)
      const controller = new AbortController()
      loadPublicDetail(controller.signal)
      return () => controller.abort()
    }
  }, [isPublic, nodeUuid, loadPublicDetail])

  // Admin-only direct IP quality poll (in guest mode, publicDetail supplies it directly)
  useEffect(() => {
    if (isPublic) return
    if (!nodeUuid) {
      setIpQuality(null)
      setLoadingIpQuality(false)
      return undefined
    }
    const controller = new AbortController()
    setLoadingIpQuality(true)
    fetch(`/api/nodes/${encodeURIComponent(nodeUuid)}/resource`, { credentials: 'same-origin', signal: controller.signal })
      .then((res) => {
        if (res.status === 401) throw new Error('unauthorized')
        if (!res.ok) throw new Error(`ip-quality:${res.status}`)
        return res.json()
      })
      .then((payload) => {
        if (controller.signal.aborted) return
        const value = payload?.resource?.ip_quality || payload?.ip_quality || null
        if (value && typeof value === 'object') {
          setIpQuality(value)
        } else {
          setIpQuality(null)
        }
      })
      .catch((error) => {
        if (error?.name !== 'AbortError') setIpQuality(null)
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoadingIpQuality(false)
      })
    return () => controller.abort()
  }, [nodeUuid, isPublic])

  // Admin-only direct media results poll (in guest mode, publicDetail supplies it directly)
  useEffect(() => {
    if (isPublic) return
    if (!nodeUuid) {
      setMediaData([])
      setLoadingMedia(false)
      return undefined
    }
    const controller = new AbortController()
    setLoadingMedia(true)
    fetch(`/api/nodes/${encodeURIComponent(nodeUuid)}/media`, { credentials: 'same-origin', signal: controller.signal })
      .then((res) => {
        if (res.status === 401) throw new Error('unauthorized')
        if (!res.ok) throw new Error(`media:${res.status}`)
        return res.json()
      })
      .then((list) => {
        if (!controller.signal.aborted && Array.isArray(list)) {
          setMediaData(list)
        }
      })
      .catch((error) => {
        if (error?.name !== 'AbortError') setMediaData([])
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoadingMedia(false)
      })
    return () => controller.abort()
  }, [nodeUuid, isPublic])

  const unlockedMediaCount = POPULAR_MEDIA.filter(
    (p) => getMediaStatus(p, mediaData).tone === 'available'
  ).length

  const effectiveNode = node || (publicDetail ? {
    uuid: publicDetail.uuid || nodeUuid,
    id: publicDetail.uuid || nodeUuid,
    name: publicDetail.name || 'ProbeWatch 节点',
    status: publicDetail.status || 'offline',
    lastReportedAt: publicDetail.last_reported_at || null,
  } : null)

  const resource = {
    ...(effectiveNode?.resource || {}),
    ...(publicDetail?.resource || {}),
  }
  // Public detail responses expose compact status rows. Reuse the newest historical
  // resource snapshot and publicDetail snapshot so hardware and network cards stay aligned.
  const latestHistoryResource = Array.isArray(history)
    ? [...history].reverse().find((sample) => sample?.resource && typeof sample.resource === 'object')?.resource || {}
    : {}
  const detailResource = {
    ...latestHistoryResource,
    ...(publicDetail?.resource || {}),
  }
  Object.entries(resource).forEach(([key, value]) => {
    const usable = value !== null && value !== undefined && value !== '' && value !== '—' && !(Array.isArray(value) && value.length === 0)
    if (usable && !(key in detailResource)) detailResource[key] = value
  })
  const suppliedRate = rates[nodeUuid] || {}
  const latestHistoryRate = Array.isArray(history)
    ? [...history].reverse().find((sample) => numeric(sample?.downRate) !== null || numeric(sample?.upRate) !== null) || null
    : null
  // Public/guest detail pages do not receive the management console's live
  // rate map. Reuse the newest historical sample so the summary and charts
  // do not disagree while the next live sample is pending.
  const rate = {
    down: numeric(suppliedRate.down) ?? numeric(latestHistoryRate?.downRate),
    up: numeric(suppliedRate.up) ?? numeric(latestHistoryRate?.upRate),
  }

  const memUsed = effectiveNode?.memUsed ?? numeric(detailResource.memory_used_bytes)
  const memTotal = effectiveNode?.memTotal ?? numeric(detailResource.memory_total_bytes)
  const swapUsed = effectiveNode?.swapUsed ?? numeric(detailResource.swap_used_bytes)
  const swapTotal = effectiveNode?.swapTotal ?? numeric(detailResource.swap_total_bytes)
  const diskUsed = effectiveNode?.diskUsed ?? numeric(detailResource.filesystem_used_bytes)
  const diskTotal = effectiveNode?.diskTotal ?? numeric(detailResource.filesystem_total_bytes)

  const rawTx = numeric(effectiveNode?.tx) ?? numeric(detailResource.network_tx_bytes)
  const rawRx = numeric(effectiveNode?.rx) ?? numeric(detailResource.network_rx_bytes)
  const totalTraffic = rawTx !== null || rawRx !== null ? (rawTx || 0) + (rawRx || 0) : null

  const cpuPercent = numeric(effectiveNode?.cpu) ?? numeric(detailResource.cpu_percent)
  const cpuModel = detailResource.cpu_name || detailResource.cpu_model || effectiveNode?.cpuModel || customMeta.cpuModel || '—'
  const cleanedCpuModel = (cpuModel || '')
    .replace(/\s*\(\s*\d+\s*(?:vCPU|vCPUs|核|core|cores)\s*\)/gi, '')
    .trim()
  const cpuBenchmarkUrl = cpuModel !== '—'
    ? `https://www.cpubenchmark.net/cpu_lookup.php?cpu=${encodeURIComponent(cleanedCpuModel || cpuModel)}`
    : 'https://www.cpubenchmark.net/cpu_lookup.php'
  // Agent snapshots commonly expose the address as ipv4 while the compact
  // public node row leaves ip empty. Prefer either address before falling
  // back to the custom metadata so every public IP label stays consistent.
  const publicIp = detailResource.ip || detailResource.ipv4 || effectiveNode?.hostname || customMeta.ip || '—'
  const visitorIp = clientInfo?.ip || '—'
  const nodeIPv4 = !isPublic ? (effectiveNode?.ipv4 || detailResource.ipv4 || (publicIp !== '—' && !publicIp.includes(':') ? publicIp : '')) : ''
  const nodeIPv6 = !isPublic ? (effectiveNode?.ipv6 || detailResource.ipv6 || (publicIp !== '—' && publicIp.includes(':') ? publicIp : '')) : ''
  const hasDualStack = !isPublic
    ? Boolean((effectiveNode?.ipv4 || detailResource.ipv4) && (effectiveNode?.ipv6 || detailResource.ipv6))
    : Boolean(publicDetail?.resource?.dual_stack || (publicDetail?.resource?.has_ipv4 && publicDetail?.resource?.has_ipv6))
  const interfaces = !isPublic && Array.isArray(effectiveNode?.interfaces) && effectiveNode.interfaces.length > 0
    ? effectiveNode.interfaces
    : (!isPublic && Array.isArray(detailResource.interfaces) ? detailResource.interfaces : [])
  const cores = numeric(detailResource.cpu_cores) ?? numeric(effectiveNode?.cpu_cores)
  const cpuMhz = numeric(detailResource.cpu_mhz) || numeric(effectiveNode?.cpu_mhz) || null
  const cpuTempC = numeric(detailResource.cpu_temp_c) ?? numeric(effectiveNode?.cpu_temp_c) ?? null
  const sensors = Array.isArray(detailResource.sensors) ? detailResource.sensors : (Array.isArray(effectiveNode?.sensors) ? effectiveNode.sensors : [])
  const disks = Array.isArray(detailResource.disks) ? detailResource.disks : (Array.isArray(effectiveNode?.disks) ? effectiveNode.disks : [])
  const mounts = Array.isArray(detailResource.mounts) ? detailResource.mounts : (Array.isArray(effectiveNode?.mounts) ? effectiveNode.mounts : [])
  const socketStats = detailResource.socket_stats || effectiveNode?.socketStats || effectiveNode?.socket_stats || {}
  const listeningPorts = Array.isArray(detailResource.listening_ports) ? detailResource.listening_ports : (Array.isArray(effectiveNode?.listeningPorts) ? effectiveNode.listeningPorts : (Array.isArray(effectiveNode?.listening_ports) ? effectiveNode.listening_ports : []))
  const healthInfo = publicDetail?.health_info || detailResource.health_info || effectiveNode?.healthInfo || effectiveNode?.health_info || null

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

  const tcpTotal = numeric(socketStats.tcp_total)
  const tcpEstablished = numeric(socketStats.tcp_established)
  const tcpListen = numeric(socketStats.tcp_listen)
  const tcpTimeWait = numeric(socketStats.tcp_time_wait)
  const tcpCloseWait = numeric(socketStats.tcp_close_wait)
  const udpTotal = numeric(socketStats.udp_total)
  const socketDisplay = (value) => value === null ? '—' : value
  const knownSocketStates = [tcpEstablished, tcpListen, tcpTimeWait, tcpCloseWait].every((value) => value !== null)
  const sumSockets = knownSocketStates && udpTotal !== null
    ? tcpEstablished + tcpListen + tcpTimeWait + tcpCloseWait + udpTotal
    : (tcpTotal !== null && udpTotal !== null ? tcpTotal + udpTotal : null)

  const estPct = sumSockets > 0 && tcpEstablished !== null ? (tcpEstablished / sumSockets) * 100 : 0
  const listenPct = sumSockets > 0 && tcpListen !== null ? (tcpListen / sumSockets) * 100 : 0
  const twPct = sumSockets > 0 && tcpTimeWait !== null ? (tcpTimeWait / sumSockets) * 100 : 0
  const cwPct = sumSockets > 0 && tcpCloseWait !== null ? (tcpCloseWait / sumSockets) * 100 : 0
  const udpPct = sumSockets > 0 && udpTotal !== null ? (udpTotal / sumSockets) * 100 : 0

  const totalDiskReadRate = disks.reduce((sum, d) => sum + (numeric(d.read_bytes_per_sec) || 0), 0)
  const totalDiskWriteRate = disks.reduce((sum, d) => sum + (numeric(d.write_bytes_per_sec) || 0), 0)
  const totalReadIOPS = disks.reduce((sum, d) => sum + (numeric(d.read_iops) || 0), 0)
  const totalWriteIOPS = disks.reduce((sum, d) => sum + (numeric(d.write_iops) || 0), 0)
  const maxIOWait = disks.reduce((max, d) => Math.max(max, numeric(d.io_wait_ms) || 0), 0)
  const maxDiskUtil = disks.reduce((max, d) => Math.max(max, numeric(d.util_percent) || 0), 0)

  const arch = effectiveNode?.arch || resource.arch || customMeta.arch || 'kvm'
  const os = effectiveNode?.os || resource.os || customMeta.os || 'Linux'
  const kernel = effectiveNode?.kernel || resource.kernel || customMeta.kernel || '—'
  const ispText = customMeta.merchant || customMeta.isp || effectiveNode?.region || '—'

  // Dynamic ticking uptime
  const startedAt = numeric(effectiveNode?.startedAt) ?? numeric(resource.started_at)
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
    return effectiveNode?.uptime || '—'
  }, [startedAt, nowTick, effectiveNode?.uptime])

  // Heartbeat status
  const lastReportedAt = effectiveNode?.lastReportedAt || effectiveNode?.last_reported_at || resource.reported_at
  const isOnline = useMemo(() => {
    if (!lastReportedAt) return effectiveNode?.status === 'online'
    const ms = typeof lastReportedAt === 'number' ? (lastReportedAt < 1e12 ? lastReportedAt * 1000 : lastReportedAt) : new Date(lastReportedAt).getTime()
    return (nowTick - ms) <= 120000
  }, [lastReportedAt, nowTick, effectiveNode?.status])

  const heartbeatText = useMemo(() => {
    if (!lastReportedAt) return isOnline ? '在线' : '离线'
    return relativeHeartbeat(lastReportedAt)
  }, [lastReportedAt, nowTick, isOnline])

  const rawTcp = numeric(resource.tcp_conn_count)
  const rawUdp = numeric(resource.udp_conn_count)
  const rawProc = numeric(resource.process_count)
  const tcpCount = rawTcp !== null && rawTcp >= 0 ? rawTcp : null
  const udpCount = rawUdp !== null && rawUdp >= 0 ? rawUdp : null
  const totalConnections = tcpCount !== null || udpCount !== null ? (tcpCount || 0) + (udpCount || 0) : null
  const processCount = rawProc !== null && rawProc >= 0 ? rawProc : null

  const gpuPercent = numeric(resource.gpu_percent)
  const hasGpu = Boolean(node?.gpu || resource.gpu_name || (numeric(resource.gpu_percent) && numeric(resource.gpu_percent) > 0))

  const trafficQuotaBytes = numeric(customMeta.trafficQuotaBytes)
  const trafficQuotaText = customMeta.trafficQuota || '未配置'
  const trafficPercent = trafficQuotaBytes > 0 && totalTraffic !== null
    ? Math.min(100, Math.max(0, ((totalTraffic / trafficQuotaBytes) * 100))).toFixed(1)
    : null

  // Top metric values
  const priceDisplay = billing.price !== null && billing.price !== undefined ? `${calc.symbol || '$'}${billing.price}` : '—'
  const monthlyExpense = billing.price !== null && billing.price !== undefined && billing.cycle ? `${calc.symbol || '$'}${((billing.price || 0) / (billing.cycle === 'annual' ? 12 : 1)).toFixed(2)}` : '—'
  const remainingDays = calc.daysRemaining !== null && calc.daysRemaining !== undefined ? calc.daysRemaining : '—'
  const remainingValue = calc.remainingValueCNY !== undefined ? `¥${calc.remainingValueCNY.toFixed(2)}` : '—'

  // Daily traffic
  const dayRx = traffic?.rx_bytes !== undefined && traffic?.rx_bytes !== null ? traffic.rx_bytes : null
  const dayTx = traffic?.tx_bytes !== undefined && traffic?.tx_bytes !== null ? traffic.tx_bytes : null
  const dailyTrafficText = (dayRx !== null || dayTx !== null)
    ? `~ ${formatBytes(dayRx || 0)} · ~ ${formatBytes(dayTx || 0)}`
    : `~ ${formatRate(rate?.down)} · ~ ${formatRate(rate?.up)}`

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
    const liveMemRatio = memTotal > 0 && memUsed !== null ? (memUsed / memTotal) * 100 : null
    const liveSwapRatio = swapTotal > 0 && swapUsed !== null ? (swapUsed / swapTotal) * 100 : null
    const liveDiskRatio = diskTotal > 0 && diskUsed !== null ? (diskUsed / diskTotal) * 100 : null
    const liveDown = numeric(rate?.down)
    const liveUp = numeric(rate?.up)
    const liveConn = totalConnections
    const liveProc = processCount
    const liveGpu = gpuPercent
    const liveTemp = cpuTempC
    const liveDiskRead = totalDiskReadRate
    const liveDiskWrite = totalDiskWriteRate

    const hasHistory = Array.isArray(history) && history.length > 0

    if (hasHistory) {
      const valueOrNull = (value) => value === null || value === undefined ? null : Number(value)
      const cpus = history.map((h) => valueOrNull(h.cpu))
      const mems = history.map((h) => valueOrNull(h.mem))
      const swaps = history.map((h) => valueOrNull(h.swap))
      const disks = history.map((h) => valueOrNull(h.disk))
      const downs = history.map((h) => valueOrNull(h.downRate))
      const ups = history.map((h) => valueOrNull(h.upRate))
      const conns = history.map((h) => valueOrNull(h.conn ?? (h.tcp !== undefined || h.udp !== undefined ? (h.tcp || 0) + (h.udp || 0) : null)))
      const procs = history.map((h) => valueOrNull(h.proc))
      const gpus = history.map((h) => valueOrNull(h.gpu))
      const temps = history.map((h) => valueOrNull(h.temp))
      const diskReads = history.map((h) => valueOrNull(h.diskReadRate))
      const diskWrites = history.map((h) => valueOrNull(h.diskWriteRate))

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
    const cpus = liveCpu === null ? [] : Array.from({ length: count }, () => liveCpu)
    const mems = liveMemRatio === null ? [] : Array.from({ length: count }, () => liveMemRatio)

    const swaps = liveSwapRatio === null ? [] : Array.from({ length: count }, () => liveSwapRatio)
    const disks = liveDiskRatio === null ? [] : Array.from({ length: count }, () => liveDiskRatio)

    const downs = liveDown === null ? [] : Array.from({ length: count }, () => liveDown)
    const ups = liveUp === null ? [] : Array.from({ length: count }, () => liveUp)
    const conns = liveConn === null ? [] : Array.from({ length: count }, () => liveConn)
    const procs = liveProc === null ? [] : Array.from({ length: count }, () => liveProc)

    const gpus = liveGpu === null ? [] : Array.from({ length: count }, () => liveGpu)

    const temps = liveTemp === null ? [] : Array.from({ length: count }, () => liveTemp)
    const diskReads = liveDiskRead === null ? [] : Array.from({ length: count }, () => liveDiskRead)
    const diskWrites = liveDiskWrite === null ? [] : Array.from({ length: count }, () => liveDiskWrite)

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

  const effectiveChecksSummary = (Array.isArray(checksSummary) && checksSummary.length > 0)
    ? checksSummary
    : (Array.isArray(publicDetail?.checks) && publicDetail.checks.length > 0 ? publicDetail.checks : null)

  // Dynamic Ping Targets mapped directly from checksSummary API or publicDetail
  const pingTargets = useMemo(() => {
    if (Array.isArray(effectiveChecksSummary)) {
      return effectiveChecksSummary.map((item, idx) => {
        const color = TARGET_COLORS[idx % TARGET_COLORS.length]
        const id = item.target_id || `target-${idx}`
        const name = cleanTargetLabel(item.name || item.host, `目标 ${idx + 1}`)
        const latencyAvg = item.latency_avg_ms !== undefined && item.latency_avg_ms !== null ? item.latency_avg_ms : null
        const latency = latencyAvg !== null ? `${Math.round(latencyAvg)}ms` : '—'
        const lossRate = item.loss_rate !== undefined && item.loss_rate !== null ? item.loss_rate * 100 : null
        const loss = lossRate === null ? '—' : `${lossRate.toFixed(2)}%`
        const lossColor = lossRate === null ? 'text-muted' : lossRate > 5 ? 'text-rose' : lossRate > 0 ? 'text-amber' : 'text-mint'
        const jitter = item.jitter_ms ?? null
        const lastChecked = item.last_checked_at ? relativeHeartbeat(item.last_checked_at) : '等待采样'
        return {
          id,
          name,
          host: item.host || item.target || cleanTargetLabel(item.name) || '',
          latency,
          latencyVal: latencyAvg ?? 0,
          loss,
          lossVal: lossRate,
          lossColor,
          color,
          jitter,
          lastChecked,
        }
      })
    }

    return []
  }, [effectiveChecksSummary])

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

    // checks/summary provides the latest aggregate only. Do not fabricate a
    // historical curve from one sample; render the chart only when real
    // history is available.
    if (!Array.isArray(pingHistory) || pingHistory.length === 0) {
      return {
        width, height, paddingLeft, paddingRight, paddingTop, paddingBottom, plotW, plotH,
        yUpper, yTicks, pointsCount, pointsTimes, pointsFullTimes, targetPaths: [], targetPointsMap: {}, displayedXMarks: [],
      }
    }

    pingTargets.forEach((t) => {
      const samples = pingHistory
        .filter((item) => (item.target_id || item.id) === t.id)
        .sort((a, b) => new Date(a.checked_at || 0) - new Date(b.checked_at || 0))
        .map((item) => {
          const result = item.result || {}
          const value = numeric(result.latency_ms ?? item.latency_ms ?? item.latency)
          return value !== null && value >= 0 ? { value, time: item.checked_at } : null
        })
        .filter(Boolean)
      if (!samples.length) return

      const pts = samples.map((sample, i) => {
        const x = paddingLeft + (i / Math.max(samples.length - 1, 1)) * plotW
        const norm = Math.min(1, Math.max(0, sample.value / yUpper))
        const y = paddingTop + plotH - norm * plotH
        return { x, y, val: sample.value, time: sample.time ? new Date(sample.time).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false }) : pointsTimes[i] || '—', fullTime: sample.time ? new Date(sample.time).toLocaleString('zh-CN', { hour12: false }) : pointsFullTimes[i] || '—' }
      })

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
  }, [pingTargets, pingHistory, selectedTargets, pingRangeConfig, smoothPeaks, nowTick])

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

  const isSyncing = !effectiveNode && (publicDetailLoading || loading || (isPublic && nodeUuid && !publicDetailError))

  if (isSyncing) {
    return (
      <section className="subpage node-detail-page">
        <div className="panel" style={{ textAlign: 'center', padding: '48px 24px', maxWidth: '540px', margin: '40px auto', borderRadius: '12px' }}>
          <div style={{ marginBottom: '16px', display: 'flex', justifyContent: 'center' }}>
            <CircleNotch size={36} className="spin text-blue" />
          </div>
          <h3 style={{ margin: '0 0 10px', fontSize: '18px', fontWeight: 600 }}>正在加载节点详情…</h3>
          <p style={{ color: 'var(--text-3, #94a3b8)', margin: 0, fontSize: '13px' }}>
            正在拉取节点资源快照与实时监控指标。
          </p>
        </div>
      </section>
    )
  }

  if (
    (isPublic && publicDetailError === 'not_found') ||
    (!isPublic && !effectiveNode && !loading) ||
    (!nodeUuid && !effectiveNode)
  ) {
    return (
      <section className="subpage node-detail-page">
        <div className="panel" style={{ textAlign: 'center', padding: '48px 24px', maxWidth: '540px', margin: '40px auto', borderRadius: '12px' }}>
          <div style={{ marginBottom: '16px', display: 'flex', justifyContent: 'center' }}>
            <WarningCircle size={40} className="text-amber" />
          </div>
          <h3 style={{ margin: '0 0 10px', fontSize: '18px', fontWeight: 600 }}>未找到指定节点</h3>
          <p style={{ color: 'var(--text-3, #94a3b8)', marginBottom: '24px', fontSize: '13px', lineHeight: 1.6 }}>
            该节点不存在、已被移除或当前访客模式暂未公开。
          </p>
          <button type="button" className="button button-primary" onClick={onBack} style={{ display: 'inline-flex', alignItems: 'center', gap: '6px', margin: '0 auto' }}>
            <ArrowLeft size={16} /> <span>{isPublic ? '返回大屏' : '返回节点列表'}</span>
          </button>
        </div>
      </section>
    )
  }

  if (isPublic && publicDetailError === 'request_failed' && !effectiveNode) {
    return (
      <section className="subpage node-detail-page">
        <div className="panel" style={{ textAlign: 'center', padding: '48px 24px', maxWidth: '540px', margin: '40px auto', borderRadius: '12px' }}>
          <div style={{ marginBottom: '16px', display: 'flex', justifyContent: 'center' }}>
            <WarningCircle size={40} className="text-rose" />
          </div>
          <h3 style={{ margin: '0 0 10px', fontSize: '18px', fontWeight: 600 }}>同步节点详情失败</h3>
          <p style={{ color: 'var(--text-3, #94a3b8)', marginBottom: '24px', fontSize: '13px', lineHeight: 1.6 }}>
            网络连接出现异常，未能获取最新指标，请检查网络后重试。
          </p>
          <button type="button" className="button button-primary" onClick={() => loadPublicDetail()} style={{ display: 'inline-flex', alignItems: 'center', gap: '6px', margin: '0 auto' }}>
            <ArrowsClockwise size={16} /> <span>重新加载</span>
          </button>
        </div>
      </section>
    )
  }

  return (
    <section className={`subpage komari-detail-page${mobileCompact ? ' mobile-compact-mode' : ''}`}>
      <div className="node-detail-summary-strip">
        <div><span>状态</span><strong className={isOnline ? 'text-mint' : 'text-rose'}>{isOnline ? '在线' : '离线'}</strong></div>
        <div><span>CPU</span><strong>{cpuPercent === null ? '—' : `${Math.round(cpuPercent)}%`}</strong></div>
        <div><span>内存</span><strong>{memTotal ? `${Math.round((memUsed / memTotal) * 100)}%` : '—'}</strong></div>
        <div><span>下行</span><strong>{rate?.down == null ? '等待采样' : formatRate(rate.down)}</strong></div>
        <div><span>上行</span><strong>{rate?.up == null ? '等待采样' : formatRate(rate.up)}</strong></div>
        <div><span>解锁</span><strong>{Array.isArray(mediaData) && mediaData.length > 0 ? `${mediaData.filter(m => (m.status || m.result?.status || '').toLowerCase() === 'available').length}/${mediaData.length}` : `${unlockedMediaCount}/${POPULAR_MEDIA.length}`}</strong></div>
        <div><span>剩余价值</span><strong>{remainingValue}</strong></div>
      </div>
      {/* 1. 顶部导航与控制条 */}
      <div className="komari-nav-bar">
        <div className="komari-nav-left">
          <button type="button" className="komari-back-btn" onClick={onBack} title="返回服务器列表">
            <ArrowLeft size={16} />
          </button>
          <span className="komari-server-flag">{customMeta.customFlag !== '自动识别' ? customMeta.customFlag : (effectiveNode?.flag || '🌐')}</span>
          <h1 className="komari-server-title">{customMeta.customName || effectiveNode?.name || 'ProbeWatch 节点'}</h1>
          <span className={`komari-status-tag status-pill ${isOnline ? 'online' : 'offline'}`}>
            <span className={`status-dot-pulse ${isOnline ? '' : 'offline'}`} /> {isOnline ? '在线' : '离线'} · {heartbeatText}
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
          <button type="button" className="komari-icon-btn" onClick={handleSharePoster} title="生成出机/测速海报并复制或下载">
            <ShareNetwork size={16} />
            <span className="poster-action-label">{posterCopied ? '已复制' : '出机海报'}</span>
          </button>
          <button type="button" className="komari-icon-btn" onClick={handleCopyShareMarkdown} title="复制适合 NodeSeek / Hostloc 的 Markdown 分享卡片">
            <ClipboardText size={16} />
            <span className="poster-action-label">{markdownCopied ? '已复制' : 'Markdown'}</span>
          </button>
          {/* 访客同步状态与刷新按钮 */}
          {isPublic && (
            <div style={{ display: 'inline-flex', alignItems: 'center', gap: '6px', marginRight: '4px' }}>
              {publicDetailLoading ? (
                <span className="badge badge-neutral" style={{ display: 'inline-flex', alignItems: 'center', gap: '4px', fontSize: '11px', padding: '3px 8px' }}>
                  <CircleNotch size={12} className="spin text-blue" /> 同步中
                </span>
              ) : lastSyncTime ? (
                <span className="badge badge-neutral mono text-muted" style={{ fontSize: '11px', padding: '3px 8px' }} title={`最近同步：${lastSyncTime.toLocaleString('zh-CN')}`}>
                  {lastSyncTime.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit' })}
                </span>
              ) : null}
              <button
                type="button"
                className="komari-icon-btn"
                onClick={() => loadPublicDetail()}
                title="刷新节点详情数据"
              >
                <ArrowsClockwise size={15} />
              </button>
            </div>
          )}
          {/* 打开远程终端 (管理员模式可见) */}
          {!isPublic && onNavigate && (
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
        <div className="komari-stat-card card-panel mjj-card">
          <div className="komari-stat-head">
            <span className="komari-stat-label">节点价格</span>
            <Tag size={15} className="komari-stat-icon text-muted" />
          </div>
          <div className="komari-stat-value mono mono-stat">
            {priceDisplay} {billing.price ? <small className="text-muted">/ 月</small> : null}
          </div>
        </div>

        {/* 2. 月均支出 */}
        <div className="komari-stat-card card-panel mjj-card">
          <div className="komari-stat-head">
            <span className="komari-stat-label">月均支出</span>
            <Coins size={15} className="komari-stat-icon text-muted" />
          </div>
          <div className="komari-stat-value mono mono-stat">
            {monthlyExpense} {billing.price ? <small className="text-muted">/ 月</small> : null}
          </div>
        </div>

        {/* 3. 剩余时间 */}
        <div className="komari-stat-card card-panel mjj-card">
          <div className="komari-stat-head">
            <span className="komari-stat-label">剩余时间</span>
            <CalendarBlank size={15} className="komari-stat-icon text-muted" />
          </div>
          <div className="komari-stat-value mono text-mint font-bold">
            {remainingDays} {remainingDays !== '—' ? <small className="text-mint font-normal">天</small> : null}
          </div>
        </div>

        {/* 4. 剩余价值 */}
        <div className="komari-stat-card card-panel mjj-card">
          <div className="komari-stat-head">
            <span className="komari-stat-label">剩余价值</span>
            <Wallet size={15} className="komari-stat-icon text-muted" />
          </div>
          <div className="komari-stat-value mono font-bold">
            {remainingValue}
          </div>
        </div>

        {/* 5. 累计流量 */}
        <div className="komari-stat-card card-panel mjj-card">
          <div className="komari-stat-head">
            <span className="komari-stat-label">累计流量</span>
            <TrendUp size={15} className="komari-stat-icon text-muted" />
          </div>
          <div className="komari-stat-value mono mono-stat">
            {formatBytes(totalTraffic)}
          </div>
        </div>

        {/* 6. 流量配额 */}
        <div className="komari-stat-card card-panel mjj-card">
          <div className="komari-stat-head">
            <span className="komari-stat-label">流量配额</span>
            <ChartPieSlice size={15} className="komari-stat-icon text-muted" />
          </div>
          <div className="komari-stat-value mono mono-stat">
            {trafficPercent === null ? '—' : trafficPercent} {trafficPercent !== null && <small className="text-muted">%</small>}
          </div>
        </div>

        {/* 7. 运行时间 */}
        <div className="komari-stat-card card-panel mjj-card">
          <div className="komari-stat-head">
            <span className="komari-stat-label">运行时间</span>
            <Clock size={15} className="komari-stat-icon text-muted" />
          </div>
          <div className="komari-stat-value mono mono-stat">
            {uptimeText}
          </div>
        </div>

        {/* 8. 连接数 */}
        <div className="komari-stat-card card-panel mjj-card">
          <div className="komari-stat-head">
            <span className="komari-stat-label">连接数</span>
            <ShareNetwork size={15} className="komari-stat-icon text-muted" />
          </div>
          <div className="komari-stat-value mono mono-stat">
            {totalConnections === null ? '—' : totalConnections}
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
              <span className="komari-info-val mono">{cores === null ? '—' : `${cores} 核`}</span>
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

        {/* 卡片 3: IP 质量（白名单数据安全展示） */}
        <div className="komari-info-card komari-ip-quality-card">
          <div className="komari-info-header komari-ip-quality-summary" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
            <span className="komari-ip-quality-title" style={{ display: 'inline-flex', alignItems: 'center', gap: '6px' }}><ShieldCheck size={16} className="text-mint" /> <h3>IP 质量</h3></span>
            <span className={`badge ${
              ipQuality?.risk === 'high' ? 'badge-rose' :
              ipQuality?.risk === 'medium' ? 'badge-amber' :
              ipQuality?.risk === 'low' ? 'badge-mint' :
              'badge-neutral'
            }`}>
              {publicDetailLoading || loadingIpQuality
                ? '检测中'
                : ipQuality?.risk === 'high'
                ? '高风险'
                : ipQuality?.risk === 'medium'
                ? '中风险'
                : ipQuality?.risk === 'low'
                ? '低风险'
                : ipQuality
                ? '未知风险'
                : publicDetailError === 'unauthorized'
                ? '未授权'
                : publicDetailError
                ? '请求失败'
                : '等待检测'}
            </span>
          </div>
          <div className="komari-ip-quality-body" style={{ paddingTop: '8px' }}>
            {publicDetailLoading || loadingIpQuality ? (
              <div style={{ display: 'flex', alignItems: 'center', gap: '8px', padding: '16px 12px', color: 'var(--text-muted, #94a3b8)', fontSize: '12px' }}>
                <CircleNotch size={16} className="spin text-blue" />
                <span>正在同步 IP 质量检测样本…</span>
              </div>
            ) : ipQuality ? (
              <>
                {publicDetailError && (
                  <div className="ip-quality-stale-alert">
                    检测服务暂时不可用，显示最近一次成功结果
                  </div>
                )}
                {/* 第一行：4 个紧凑摘要格 */}
                <div className="ip-quality-summary-grid">
                  <div className="ip-quality-summary-item risk">
                    <span className="label">综合风险</span>
                    <span className={`value risk-${ipQuality.risk === 'low' ? 'low' : ipQuality.risk === 'medium' ? 'medium' : ipQuality.risk === 'high' ? 'high' : 'unknown'}`}>
                      {ipQuality.risk === 'low' ? (
                        <ShieldCheck size={14} className="text-mint" />
                      ) : ipQuality.risk === 'medium' ? (
                        <ShieldWarning size={14} className="text-amber" />
                      ) : ipQuality.risk === 'high' ? (
                        <ShieldWarning size={14} className="text-rose" />
                      ) : (
                        <Shield size={14} className="text-muted" />
                      )}
                      <span>
                        {ipQuality.risk === 'low'
                          ? '低风险'
                          : ipQuality.risk === 'medium'
                          ? '中风险'
                          : ipQuality.risk === 'high'
                          ? '高风险'
                          : '未知风险'}
                      </span>
                    </span>
                  </div>

                  <div className="ip-quality-summary-item">
                    <span className="label">IP 类型</span>
                    <span className="value">{formatIPQualityType(ipQuality.ip_type)}</span>
                  </div>

                  <div className="ip-quality-summary-item">
                    <span className="label">地区</span>
                    <span className="value" title={[ipQuality.region, ipQuality.country].filter(Boolean).join(' · ') || '—'}>
                      {[ipQuality.region, ipQuality.country].filter(Boolean).join(' · ') || '—'}
                    </span>
                  </div>

                  <div className="ip-quality-summary-item">
                    <span className="label">ASN</span>
                    <span className="value" title={[ipQuality.asn, ipQuality.organization].filter(Boolean).join(' ') || '—'}>
                      {[ipQuality.asn, ipQuality.organization].filter(Boolean).join(' ') || '—'}
                    </span>
                  </div>
                </div>

                {/* 第二行：两个信息面板 */}
                <div className="ip-quality-detail-grid ip-quality-detail-grid-two">
                  {/* 面板 1: 风险评分 */}
                  <section className="ip-quality-panel">
                    <div className="ip-quality-panel-title">
                      <span className="panel-title-text">
                        <Gauge size={14} />
                        <span>风险评分（越低越好）</span>
                      </span>
                    </div>
                    <div className="ip-quality-panel-body">
                      {ipQuality.sources && Object.keys(ipQuality.sources).length > 0 ? (
                        <div className="ip-quality-score-list">
                          {Object.entries(ipQuality.sources).map(([sourceKey, scoreVal]) => {
                            const scoreNum = Number(scoreVal)
                            const isNum = Number.isFinite(scoreNum)
                            const displayVal = isNum ? String(Math.round(scoreNum * 100) / 100) : '—'
                            const pct = isNum ? Math.min(100, Math.max(0, scoreNum)) : 0
                            const tone = !isNum ? 'unknown' : scoreNum < 25 ? 'low' : scoreNum < 75 ? 'medium' : 'high'
                            const label = formatIPQualitySourceName(sourceKey)
                            return (
                              <div key={sourceKey} className="ip-quality-score-row">
                                <span className="ip-quality-score-label" title={label}>{label}</span>
                                <div className="ip-quality-score-track">
                                  <div
                                    className={`ip-quality-score-bar score-${tone}`}
                                    style={{ width: `${pct}%` }}
                                  />
                                </div>
                                <span className={`ip-quality-score-value score-text-${tone} mono`}>
                                  {displayVal}
                                </span>
                              </div>
                            )
                          })}
                        </div>
                      ) : (
                        <div className="ip-quality-empty-inline">
                          <span>暂无多来源评分</span>
                        </div>
                      )}
                    </div>
                  </section>

                  {/* 面板 2: 数据库标记 */}
                  <section className="ip-quality-panel">
                    <div className="ip-quality-panel-title">
                      <span className="panel-title-text">
                        <Database size={14} />
                        <span>数据库标记（命中 / 有结论的库）</span>
                      </span>
                    </div>
                    <div className="ip-quality-panel-body">
                      <div className="ip-quality-flag-grid">
                        {[
                          ['代理', ipQuality.proxy],
                          ['VPN', ipQuality.vpn],
                          ['Tor', ipQuality.tor],
                          ['滥用', ipQuality.abuse],
                          ['机房', !ipQuality.ip_type || ipQuality.ip_type === 'unknown' ? null : (ipQuality.ip_type === 'hosting' || ipQuality.ip_type === 'datacenter')],
                        ].map(([label, val]) => {
                          const status = val === true ? 'yes' : val === false ? 'no' : 'unknown'
                          return (
                            <div key={label} className="ip-quality-flag-item">
                              <span className="flag-label">{label}</span>
                              <span className={`flag-status flag-${status}`}>
                                {val === true ? '是' : val === false ? '否' : '未知'}
                              </span>
                            </div>
                          )
                        })}
                      </div>
                      <div className="ip-quality-panel-footer">
                        <small className="ip-quality-check-time">
                          检测时间：{formatIPQualityDateTime(ipQuality.checked_at)} · 来源未提供统计
                        </small>
                      </div>
                    </div>
                  </section>
                </div>
              </>
            ) : (
              <div className="komari-ip-quality-empty" style={{ display: 'flex', alignItems: 'center', gap: '8px', padding: '16px 12px', color: 'var(--text-muted, #94a3b8)', fontSize: '12px' }}>
                <ShieldWarning size={16} className="text-amber" />
                <span>{publicDetailError === 'unauthorized' ? '访客模式未开放此项指标' : publicDetailError ? 'IP 质量数据请求失败，正在等待自动重试' : '等待 Agent 首次质量检测'}</span>
              </div>
            )}
          </div>
        </div>

        {/* 卡片 4: 存储信息 */}
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
                {!isPublic && nodeIPv4 && <span className="mono text-xs text-primary" title={`IPv4: ${nodeIPv4}`}>{nodeIPv4}</span>}
                {!isPublic && nodeIPv6 && <span className="mono text-xs text-muted" title={`IPv6: ${nodeIPv6}`}>{nodeIPv6.length > 20 ? `${nodeIPv6.slice(0, 18)}…` : nodeIPv6}</span>}
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
                ^ {formatRate(rate?.up)} · v {formatRate(rate?.down)}
              </span>
            </div>
          </div>
        </div>
      </div>

      {/* 物理与虚拟网卡矩阵 (Per-NIC Metrics) */}
      {!isPublic && interfaces.length > 0 && (
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
                      {formatBytes(iface.rx_bytes)}
                    </td>
                    <td style={{ padding: '8px 12px', color: 'var(--blue, #38bdf8)' }}>
                      {formatBytes(iface.tx_bytes)}
                    </td>
                    <td style={{ padding: '8px 12px', color: 'var(--text-muted, #94a3b8)' }}>
                      {iface.rx_packets === null || iface.rx_packets === undefined ? '—' : Number(iface.rx_packets).toLocaleString()} / {iface.tx_packets === null || iface.tx_packets === undefined ? '—' : Number(iface.tx_packets).toLocaleString()}
                    </td>
                    <td style={{ padding: '8px 12px', color: (iface.rx_errors || iface.tx_errors) ? 'var(--danger, #ef4444)' : 'var(--text-muted, #94a3b8)' }}>
                      {iface.rx_errors === null || iface.rx_errors === undefined || iface.tx_errors === null || iface.tx_errors === undefined ? '—' : Number(iface.rx_errors) + Number(iface.tx_errors)}
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
                  const usedPct = numeric(m.used_percent)
                  const inodePct = numeric(m.inodes_percent)
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
                            <div style={{ width: `${usedPct === null ? 0 : Math.min(100, usedPct)}%`, height: '100%', background: usedPct !== null && usedPct > 90 ? '#ef4444' : usedPct !== null && usedPct > 75 ? '#f59e0b' : '#10b981', borderRadius: '3px' }} />
                          </div>
                          <span style={{ fontSize: '11px', minWidth: '75px', textAlign: 'right', color: usedPct !== null && usedPct > 90 ? '#ef4444' : '#cbd5e1' }}>
                            {formatBytes(m.used_bytes)} ({usedPct === null ? '—' : usedPct.toFixed(1) + '%'})
                          </span>
                        </div>
                      </td>
                      <td style={{ padding: '8px 12px' }}>
                        <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                          <div style={{ flex: 1, height: '6px', background: 'rgba(255, 255, 255, 0.08)', borderRadius: '3px', overflow: 'hidden' }}>
                            <div style={{ width: `${inodePct === null ? 0 : Math.min(100, inodePct)}%`, height: '100%', background: inodePct !== null && inodePct > 90 ? '#ef4444' : inodePct !== null && inodePct > 80 ? '#f59e0b' : '#38bdf8', borderRadius: '3px' }} />
                          </div>
                          <span style={{ fontSize: '11px', minWidth: '70px', textAlign: 'right', color: inodePct !== null && inodePct > 85 ? '#ef4444' : 'var(--text-muted, #94a3b8)' }}>
                            {inodePct === null ? '—' : inodePct.toFixed(1) + '%'} {inodePct !== null && inodePct > 85 ? '⚠️ 告警' : ''}
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
            {isPublic ? (
              <span className="mono" style={{ fontSize: '11px', padding: '2px 8px', borderRadius: '4px', background: 'rgba(245, 158, 11, 0.12)', color: '#f59e0b', border: '1px solid rgba(245, 158, 11, 0.25)' }}>
                访客安全受限
              </span>
            ) : (
              <>
                <span className="mono" style={{ fontSize: '11px', padding: '2px 8px', borderRadius: '4px', background: 'rgba(16, 185, 129, 0.12)', color: '#10b981', border: '1px solid rgba(16, 185, 129, 0.2)' }}>
                  活跃通信 {socketDisplay(tcpEstablished)}
                </span>
                <span className="mono" style={{ fontSize: '11px', padding: '2px 8px', borderRadius: '4px', background: 'rgba(6, 182, 212, 0.12)', color: '#06b6d4', border: '1px solid rgba(6, 182, 212, 0.2)' }}>
                  开放监听 {listeningPorts.length}
                </span>
                <span className="mono text-xs text-muted">内核 /proc/net 实时解析</span>
              </>
            )}
          </div>
        </div>

        {isPublic ? (
          <div style={{ padding: '28px 16px', textAlign: 'center', background: 'var(--bg-subtle, rgba(255, 255, 255, 0.02))', borderRadius: '8px', border: '1px dashed var(--border-subtle, rgba(255, 255, 255, 0.1))' }}>
            <ShieldCheck size={28} style={{ color: '#06b6d4', margin: '0 auto 10px', display: 'block' }} />
            <div style={{ fontSize: '13px', fontWeight: 600, color: 'var(--text-main, #f8fafc)', marginBottom: '6px' }}>
              隐私与网络安全保护已生效
            </div>
            <div style={{ fontSize: '12px', color: 'var(--text-muted, #94a3b8)', maxWidth: '480px', margin: '0 auto', lineHeight: '1.6' }}>
              公网访客模式下已隐藏节点内部监听端口列表、套接字连接状态及进程详情。管理员登录后可查看内核级网络连接全景。
            </div>
          </div>
        ) : (
          <>
            {/* 顶部：套接字状态细分分布条 */}
        <div style={{ padding: '12px 14px', borderRadius: '8px', background: 'var(--bg-subtle, rgba(255, 255, 255, 0.02))', border: '1px solid var(--border-subtle, rgba(255, 255, 255, 0.06))', marginBottom: '14px' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '8px' }}>
            <span style={{ fontSize: '12px', fontWeight: 600, color: 'var(--text-secondary, #cbd5e1)' }}>TCP/UDP 网络套接字状态分布</span>
            <span className="mono text-xs text-muted">
              总计 {sumSockets === null ? '—' : sumSockets} 套接字 (TCP {socketDisplay(tcpTotal)} · UDP {socketDisplay(udpTotal)})
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
              <strong className="mono" style={{ color: '#10b981' }}>{socketDisplay(tcpEstablished)}</strong>
            </div>
            <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
              <span style={{ width: '8px', height: '8px', borderRadius: '2px', background: '#06b6d4' }} />
              <span className="text-muted">服务监听 (LISTEN):</span>
              <strong className="mono" style={{ color: '#06b6d4' }}>{socketDisplay(tcpListen)}</strong>
            </div>
            <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
              <span style={{ width: '8px', height: '8px', borderRadius: '2px', background: '#f59e0b' }} />
              <span className="text-muted">等待回收 (TIME_WAIT):</span>
              <strong className="mono" style={{ color: '#f59e0b' }}>{socketDisplay(tcpTimeWait)}</strong>
            </div>
            <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
              <span style={{ width: '8px', height: '8px', borderRadius: '2px', background: '#f43f5e' }} />
              <span className="text-muted">被动关闭 (CLOSE_WAIT):</span>
              <strong className="mono" style={{ color: '#f43f5e' }}>{socketDisplay(tcpCloseWait)}</strong>
            </div>
            <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
              <span style={{ width: '8px', height: '8px', borderRadius: '2px', background: '#a855f7' }} />
              <span className="text-muted">UDP 套接字:</span>
              <strong className="mono" style={{ color: '#a855f7' }}>{socketDisplay(udpTotal)}</strong>
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
            <div style={{ textAlign: 'center', padding: '24px 0', color: 'var(--text-muted, #94a3b8)', fontSize: '12px' }}>
              {portSearch.trim() || portFilter !== 'all' ? '未检索到符合条件的本地监听端口' : '暂无服务监听端口样本（等待 Agent 上报）'}
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
          </>
        )}
      </div>

      {/* 主机多维健康评分引擎、系统安全补丁雷达与守护进程诊断 (Host Health Scoring & Maintenance Radar) */}
      <div className="komari-info-card" style={{ marginBottom: '16px' }}>
        <div className="komari-info-header" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: '8px' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
            {healthInfo && healthInfo.health_score !== undefined && healthInfo.health_score < 75 ? (
              <ShieldWarning size={16} className="text-amber" />
            ) : (
              <ShieldCheck size={16} style={{ color: '#10b981' }} />
            )}
            <h3 style={{ margin: 0 }}>主机多维健康评分与系统维护诊断</h3>
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
            {healthInfo && healthInfo.health_score !== undefined ? (
              <>
                <span
                  className="mono"
                  style={{
                    fontSize: '11px',
                    padding: '2px 8px',
                    borderRadius: '4px',
                    fontWeight: 700,
                    background: healthInfo.health_score >= 90 ? 'rgba(16, 185, 129, 0.12)' : healthInfo.health_score >= 75 ? 'rgba(6, 182, 212, 0.12)' : healthInfo.health_score >= 60 ? 'rgba(245, 158, 11, 0.12)' : 'rgba(239, 68, 68, 0.12)',
                    color: healthInfo.health_score >= 90 ? '#10b981' : healthInfo.health_score >= 75 ? '#06b6d4' : healthInfo.health_score >= 60 ? '#f59e0b' : '#ef4444',
                    border: `1px solid ${healthInfo.health_score >= 90 ? 'rgba(16, 185, 129, 0.25)' : healthInfo.health_score >= 75 ? 'rgba(6, 182, 212, 0.25)' : healthInfo.health_score >= 60 ? 'rgba(245, 158, 11, 0.25)' : 'rgba(239, 68, 68, 0.25)'}`,
                  }}
                >
                  评分 {healthInfo.health_score} / 100 · {healthInfo.health_status === 'optimal' ? '极佳' : healthInfo.health_status === 'good' ? '良好' : healthInfo.health_status === 'degraded' ? '亚健康' : '严重风险'}
                </span>
                {healthInfo.reboot_required && (
                  <span className="mono" style={{ fontSize: '11px', padding: '2px 8px', borderRadius: '4px', background: 'rgba(239, 68, 68, 0.15)', color: '#ef4444', border: '1px solid rgba(239, 68, 68, 0.3)', fontWeight: 600 }}>
                    ⚠️ 内核待重启生效
                  </span>
                )}
              </>
            ) : (
              <span className="mono text-xs text-muted">{isPublic && publicDetailLoading ? '正在同步主机健康诊断…' : '探针未上报系统维护诊断'}</span>
            )}
            <span className="mono text-xs text-muted">启发式多维健康引擎</span>
          </div>
        </div>

        {healthInfo && healthInfo.health_score !== undefined ? (
          <div>
            {/* 顶部指标四宫格 */}
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))', gap: '12px', marginBottom: '14px' }}>
              {/* 1. 综合健康评分 */}
              <div style={{ padding: '12px 14px', borderRadius: '8px', background: 'var(--bg-subtle, rgba(255, 255, 255, 0.02))', border: '1px solid var(--border-subtle, rgba(255, 255, 255, 0.06))' }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '6px' }}>
                  <span style={{ fontSize: '12px', color: 'var(--text-secondary, #94a3b8)' }}>综合健康指数</span>
                  <span className="mono" style={{ fontSize: '11px', fontWeight: 600, color: healthInfo.health_score >= 90 ? '#10b981' : healthInfo.health_score >= 75 ? '#06b6d4' : healthInfo.health_score >= 60 ? '#f59e0b' : '#ef4444' }}>
                    {healthInfo.health_status?.toUpperCase() || 'NORMAL'}
                  </span>
                </div>
                <div style={{ display: 'flex', alignItems: 'baseline', gap: '4px', marginBottom: '8px' }}>
                  <span className="mono" style={{ fontSize: '24px', fontWeight: 800, color: healthInfo.health_score >= 90 ? '#10b981' : healthInfo.health_score >= 75 ? '#06b6d4' : healthInfo.health_score >= 60 ? '#f59e0b' : '#ef4444' }}>
                    {healthInfo.health_score}
                  </span>
                  <span className="mono text-muted text-xs">/ 100 分</span>
                </div>
                <div style={{ height: '5px', width: '100%', background: 'rgba(255, 255, 255, 0.08)', borderRadius: '3px', overflow: 'hidden' }}>
                  <div
                    style={{
                      height: '100%',
                      width: `${Math.min(100, Math.max(0, healthInfo.health_score))}%`,
                      background: healthInfo.health_score >= 90 ? '#10b981' : healthInfo.health_score >= 75 ? '#06b6d4' : healthInfo.health_score >= 60 ? '#f59e0b' : '#ef4444',
                      borderRadius: '3px',
                      transition: 'width 0.4s ease',
                    }}
                  />
                </div>
              </div>

              {/* 2. 系统待重启状态 */}
              <div style={{ padding: '12px 14px', borderRadius: '8px', background: 'var(--bg-subtle, rgba(255, 255, 255, 0.02))', border: '1px solid var(--border-subtle, rgba(255, 255, 255, 0.06))' }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '6px' }}>
                  <span style={{ fontSize: '12px', color: 'var(--text-secondary, #94a3b8)' }}>系统内核维护</span>
                  {healthInfo.reboot_required ? (
                    <WarningCircle size={14} style={{ color: '#ef4444' }} />
                  ) : (
                    <CheckCircle size={14} style={{ color: '#10b981' }} />
                  )}
                </div>
                <div style={{ display: 'flex', alignItems: 'baseline', gap: '4px', marginBottom: '4px' }}>
                  <span className="mono" style={{ fontSize: '18px', fontWeight: 700, color: healthInfo.reboot_required ? '#ef4444' : '#10b981' }}>
                    {healthInfo.reboot_required ? '需要系统重启' : '内核运行良好'}
                  </span>
                </div>
                <div style={{ fontSize: '11px', color: 'var(--muted, #64748b)' }}>
                  {healthInfo.reboot_required ? '检测到内核/关键底层库更新待重启' : '无挂起的重启更新待办'}
                </div>
              </div>

              {/* 3. 安全更新补丁雷达 */}
              <div style={{ padding: '12px 14px', borderRadius: '8px', background: 'var(--bg-subtle, rgba(255, 255, 255, 0.02))', border: '1px solid var(--border-subtle, rgba(255, 255, 255, 0.06))' }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '6px' }}>
                  <span style={{ fontSize: '12px', color: 'var(--text-secondary, #94a3b8)' }}>系统安全更新雷达</span>
                  <span className="mono" style={{ fontSize: '11px', padding: '1px 6px', borderRadius: '3px', background: healthInfo.security_updates > 0 ? 'rgba(245, 158, 11, 0.15)' : 'rgba(16, 185, 129, 0.12)', color: healthInfo.security_updates > 0 ? '#f59e0b' : '#10b981' }}>
                    {healthInfo.security_updates > 0 ? `${healthInfo.security_updates} 待加固` : '补丁最新'}
                  </span>
                </div>
                <div style={{ display: 'flex', alignItems: 'baseline', gap: '4px', marginBottom: '4px' }}>
                  <span className="mono" style={{ fontSize: '20px', fontWeight: 700, color: healthInfo.security_updates > 0 ? '#f59e0b' : 'var(--text-main, #f8fafc)' }}>
                    {healthInfo.security_updates || 0}
                  </span>
                  <span className="mono text-muted text-xs">个安全补丁 (总共 {healthInfo.total_updates || 0} 个更新)</span>
                </div>
                <div style={{ fontSize: '11px', color: 'var(--muted, #64748b)' }}>
                  {healthInfo.security_updates > 0 ? '建议及时运行 apt upgrade 或 dnf update' : '当前无未修补的已知漏洞组件'}
                </div>
              </div>

              {/* 4. 系统守护服务诊断 */}
              <div style={{ padding: '12px 14px', borderRadius: '8px', background: 'var(--bg-subtle, rgba(255, 255, 255, 0.02))', border: '1px solid var(--border-subtle, rgba(255, 255, 255, 0.06))' }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '6px' }}>
                  <span style={{ fontSize: '12px', color: 'var(--text-secondary, #94a3b8)' }}>系统守护进程 (Systemd)</span>
                  <span className="mono" style={{ fontSize: '11px', padding: '1px 6px', borderRadius: '3px', background: (healthInfo.failed_services?.length || 0) > 0 ? 'rgba(239, 68, 68, 0.15)' : 'rgba(16, 185, 129, 0.12)', color: (healthInfo.failed_services?.length || 0) > 0 ? '#ef4444' : '#10b981' }}>
                    {(healthInfo.failed_services?.length || 0) > 0 ? '异常' : '正常'}
                  </span>
                </div>
                <div style={{ display: 'flex', alignItems: 'baseline', gap: '4px', marginBottom: '4px' }}>
                  <span className="mono" style={{ fontSize: '20px', fontWeight: 700, color: (healthInfo.failed_services?.length || 0) > 0 ? '#ef4444' : '#10b981' }}>
                    {healthInfo.failed_services?.length || 0}
                  </span>
                  <span className="mono text-muted text-xs">个崩溃/失败服务</span>
                </div>
                <div style={{ fontSize: '11px', color: 'var(--muted, #64748b)' }}>
                  {(healthInfo.failed_services?.length || 0) > 0 ? 'systemctl --failed 存在异常退出单元' : '核心守护进程均处于 active (running)'}
                </div>
              </div>
            </div>

            {/* 扣分诊断项明细与优化建议 */}
            {healthInfo.health_deductions && healthInfo.health_deductions.length > 0 ? (
              <div style={{ padding: '12px 14px', borderRadius: '8px', background: 'rgba(245, 158, 11, 0.06)', border: '1px solid rgba(245, 158, 11, 0.2)', marginBottom: (healthInfo.failed_services?.length || 0) > 0 ? '12px' : 0 }}>
                <div style={{ fontSize: '12px', fontWeight: 700, color: '#f59e0b', marginBottom: '8px', display: 'flex', alignItems: 'center', gap: '6px' }}>
                  <WarningCircle size={14} /> 启发式健康扣分诊断归因 (共 {healthInfo.health_deductions.length} 项)
                </div>
                <div style={{ display: 'flex', flexWrap: 'wrap', gap: '8px' }}>
                  {healthInfo.health_deductions.map((deduction, idx) => (
                    <span
                      key={idx}
                      className="mono"
                      style={{
                        fontSize: '11.5px',
                        padding: '3px 8px',
                        borderRadius: '4px',
                        background: 'rgba(245, 158, 11, 0.12)',
                        color: 'var(--text-main, #f8fafc)',
                        border: '1px solid rgba(245, 158, 11, 0.25)',
                        display: 'inline-flex',
                        alignItems: 'center',
                        gap: '4px',
                      }}
                    >
                      <span style={{ color: '#ef4444', fontWeight: 700 }}>•</span> {deduction}
                    </span>
                  ))}
                </div>
              </div>
            ) : (
              <div style={{ padding: '10px 14px', borderRadius: '8px', background: 'rgba(16, 185, 129, 0.06)', border: '1px solid rgba(16, 185, 129, 0.18)', display: 'flex', alignItems: 'center', gap: '8px', color: '#10b981', fontSize: '12px' }}>
                <CheckCircle size={15} /> 各维度运行指标均处于极佳区间 (Optimal)，CPU、内存、磁盘、Inode、温度及后台服务无瓶颈或风险。
              </div>
            )}

            {/* 失败服务详细列表 (如果有) */}
            {healthInfo.failed_services && healthInfo.failed_services.length > 0 && (
              <div style={{ marginTop: '12px', padding: '12px 14px', borderRadius: '8px', background: 'rgba(239, 68, 68, 0.06)', border: '1px solid rgba(239, 68, 68, 0.2)' }}>
                <div style={{ fontSize: '12px', fontWeight: 700, color: '#ef4444', marginBottom: '8px', display: 'flex', alignItems: 'center', gap: '6px' }}>
                  <WarningCircle size={14} /> 异常/崩溃服务单元 (systemd --failed)
                </div>
                <div style={{ display: 'flex', flexWrap: 'wrap', gap: '8px' }}>
                  {healthInfo.failed_services.map((svc, idx) => (
                    <span
                      key={idx}
                      className="mono"
                      style={{
                        fontSize: '11px',
                        padding: '3px 8px',
                        borderRadius: '4px',
                        background: 'rgba(239, 68, 68, 0.12)',
                        color: '#fca5a5',
                        border: '1px solid rgba(239, 68, 68, 0.25)',
                      }}
                    >
                      {svc}
                    </span>
                  ))}
                </div>
              </div>
            )}
          </div>
        ) : (
          <div style={{ padding: '24px', textAlign: 'center', color: 'var(--muted, #94a3b8)', fontSize: '12px' }}>
            <p style={{ margin: '0 0 6px', fontWeight: 600, color: 'var(--text-main, #f8fafc)' }}>探针未上报系统维护诊断</p>
            <p className="mono text-xs" style={{ margin: 0, color: 'var(--text-muted)' }}>节点探针运行良好，但当前上报快照中未包含系统安全补丁与守护单元诊断指标。</p>
          </div>
        )}
      </div>

      {/* 全球流媒体与 AI 服务解锁能力横向卡片 */}
      <section className="komari-media-strip-card">
        <div className="komari-media-strip-header">
          <div className="komari-media-strip-title">
            <Play size={15} className="text-mint" />
            <h3>全球流媒体与 AI 服务解锁能力</h3>
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
            {Array.isArray(mediaData) && mediaData.length > 0 && (
              <span className="komari-media-strip-summary mono">
                {mediaData.filter((m) => (m.status || m.result?.status || '').toLowerCase() === 'available').length}/{mediaData.length} 已解锁
              </span>
            )}
            {Array.isArray(mediaData) && mediaData.length > 8 && (
              <button
                type="button"
                className="komari-media-strip-toggle-btn"
                onClick={() => setShowAllMedia(!showAllMedia)}
              >
                {showAllMedia ? '收起' : `查看全部 (${mediaData.length})`}
              </button>
            )}
          </div>
        </div>

        {(loadingMedia || (isPublic && publicDetailLoading)) && (!mediaData || mediaData.length === 0) ? (
          <div className="komari-media-strip-loading">
            <CircleNotch size={16} className="spin text-blue" />
            <span>正在同步流媒体 / AI 检测结果…</span>
          </div>
        ) : !mediaData || mediaData.length === 0 ? (
          <div className="komari-media-strip-empty">
            <Play size={18} className="text-muted" style={{ opacity: 0.5 }} />
            <span>暂无流媒体 / AI 检测结果</span>
          </div>
        ) : (
          <div className="komari-media-strip-list">
            {(showAllMedia ? mediaData : mediaData.slice(0, 8)).map((m, idx) => {
              const rawName = m.detector || m.detector_id || m.target_id || `item-${idx}`
              const matchPopular = POPULAR_MEDIA.find((p) => {
                const aliases = p.alias || [p.id]
                const dId = (m.detector_id || m.target_id || '').toLowerCase()
                const dName = (m.detector || m.result?.detector || '').toLowerCase()
                return aliases.some((a) => dId.includes(a) || dName.includes(a))
              })
              const name = matchPopular?.name || rawName.replace(/^media[-_]/i, '').replace(/[-_]/g, ' ')
              const symbol = matchPopular?.symbol || name.slice(0, 2).toUpperCase()
              const iconBg = matchPopular?.iconBg || 'rgba(56, 189, 248, 0.25)'

              const rawStatus = (m.status || m.result?.status || '').toLowerCase()
              let tone = 'unknown'
              let statusText = '未知'
              if (rawStatus === 'available') {
                tone = 'available'
                statusText = '已解锁'
              } else if (rawStatus === 'unavailable') {
                tone = 'unavailable'
                statusText = '未解锁'
              } else if (rawStatus === 'blocked') {
                tone = 'blocked'
                statusText = '已封锁'
              } else if (rawStatus === 'error') {
                tone = 'error'
                statusText = '异常'
              } else if (rawStatus === 'timeout') {
                tone = 'timeout'
                statusText = '超时'
              }

              const rawRegion = m.region || m.result?.region
              const regionText = rawRegion ? String(rawRegion).trim().toUpperCase() : '—'

              const lat = m.latency_ms ?? m.result?.latency_ms ?? null
              const latencyText = (lat !== null && lat !== undefined && lat !== '' && !isNaN(Number(lat)) && Number(lat) > 0)
                ? `${Math.round(Number(lat))}ms`
                : '—'

              return (
                <div key={m.detector_id || `${name}-${idx}`} className="komari-media-service-card">
                  <div className="komari-media-service-icon" style={{ backgroundColor: iconBg }}>
                    {symbol}
                  </div>
                  <div className="komari-media-service-name" title={name}>
                    {name}
                  </div>
                  <div className={`komari-media-service-status status-${tone}`}>
                    {statusText}
                  </div>
                  <div className="komari-media-service-region mono" title={regionText !== '—' ? `检测地区: ${regionText}` : undefined}>
                    {regionText}
                  </div>
                  <div className="komari-media-service-latency mono">
                    {latencyText}
                  </div>
                </div>
              )
            })}
          </div>
        )}
      </section>

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
            badgeText={cpuPercent === null ? '—' : `${cpuPercent.toFixed(1)}%`}
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
            yMid={memTotal === null ? '—' : formatBytes(memTotal / 2)}
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
            yMid={diskTotal === null ? '—' : formatBytes(diskTotal / 2)}
            yMin="0 B"
            timeLabels={telemetrySeries.times}
          />

          {/* 4. 实时网络 */}
          <KomariChartCard
            title="实时网络"
            icon="🔵"
            badgeText={`^ ${formatRate(rate?.up)}  v ${formatRate(rate?.down)}`}
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
            badgeText={gpuPercent === null ? '—' : `${gpuPercent.toFixed(1)}%`}
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
            badgeText={`TCP: ${tcpCount === null ? '—' : tcpCount}  UDP: ${udpCount === null ? '—' : udpCount}`}
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
            badgeText={processCount === null ? '—' : `${processCount}`}
            series={telemetrySeries.procs}
            strokeColor="#6366f1"
            yMax={`${procYMax}`}
            yMid={`${procYMid}`}
            yMin="0"
            timeLabels={telemetrySeries.times}
          />

          {/* 8. CPU 温度遥测 */}
          {(cpuTempC !== null || (telemetrySeries.temps && telemetrySeries.temps.some((t) => typeof t === 'number' && Number.isFinite(t)))) && (
            <KomariChartCard
              title="CPU 实时温度"
              icon="🌡️"
              badgeText={`${cpuTempC !== null ? cpuTempC.toFixed(1) : (() => { const values = (telemetrySeries.temps || []).filter((value) => typeof value === 'number' && Number.isFinite(value)); const last = values[values.length - 1]; return last === undefined ? '—' : last.toFixed(1) })()} °C`}
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
          {pingTargets.length === 0 ? (
            <div className="komari-targets-empty">
              {checksLoading ? (
                <>
                  <CircleNotch size={18} className="spin text-blue" />
                  <div><strong>正在同步三网检测数据…</strong><span>正在拉取最新延迟、丢包和抖动指标。</span></div>
                </>
              ) : checksError === 'unauthorized' ? (
                <>
                  <ShieldWarning size={18} className="text-amber" />
                  <div><strong>三网检测指标需要管理员权限</strong><span>公网访客模式下仅可查看公开概要。</span></div>
                </>
              ) : checksError === 'request_failed' ? (
                <>
                  <WarningCircle size={18} className="text-rose" />
                  <div><strong>三网检测数据请求失败</strong><span>服务端异常或网络波动，请稍后刷新重试。</span></div>
                </>
              ) : (
                <>
                  <ShareNetwork size={18} />
                  <div><strong>当前节点暂未配置检测目标</strong><span>可在管理员后台配置电信、联通、移动目标以展示实时延迟、丢包与抖动。</span></div>
                  {!isPublic && onNavigate && <button type="button" onClick={() => onNavigate('monitoring')}>去添加检测目标</button>}
                </>
              )}
            </div>
          ) : pingTargets.map((t) => {
            const isChecked = Boolean(selectedTargets[t.id])
            const jitterText = t.jitter === null || t.jitter === undefined || t.jitter === ''
              ? '—'
              : `${Number(t.jitter).toFixed(2)} ms`
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
            {pingTargets.length === 0 ? (
              <div className="komari-ping-empty-state komari-ping-empty-config">
                {checksError === 'unauthorized' ? (
                  <><ShieldWarning size={22} /><strong>三网检测受限</strong><span>公网访客模式下未授权查看历史采样曲线。</span></>
                ) : checksError === 'request_failed' ? (
                  <><WarningCircle size={22} /><strong>检测数据请求异常</strong><span>请稍后刷新重试。</span></>
                ) : (
                  <><ShareNetwork size={22} /><strong>暂无三网检测目标</strong><span>先添加电信、联通、移动检测目标，延迟曲线会在真实采样后自动出现。</span></>
                )}
              </div>
            ) : pingChartData.targetPaths.length === 0 ? (
              <div className="komari-ping-empty-state komari-ping-empty-config"><Clock size={22} /><strong>等待真实延迟采样</strong><span>检测目标已配置，探针回传数据后会显示曲线。</span></div>
            ) : null}
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
      <div className="node-detail-mobile-actions">
        <button type="button" onClick={onBack}><ArrowLeft size={15} /> 返回</button>
        <button type="button" className={mobileCompact ? 'active' : ''} onClick={() => setMobileCompact((value) => !value)} title="切换手机极简视图"><Rows size={15} /> {mobileCompact ? '完整' : '极简'}</button>
        <button type="button" onClick={handleSharePoster}><ShareNetwork size={15} /> 海报</button>
        <button type="button" onClick={handleCopyShareMarkdown}><ClipboardText size={15} /> Markdown</button>
        {onNavigate && <button type="button" onClick={() => onNavigate('terminal')}><Terminal size={15} /> 终端</button>}
      </div>
      {showPosterModal && (
        <PosterModal
          node={node}
          mediaData={mediaData}
          pingHistory={pingHistory}
          onClose={() => setShowPosterModal(false)}
        />
      )}
    </section>
  )
}
