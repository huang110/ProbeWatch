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
function generateSplinePath(points, width = 400, height = 120, padding = 10) {
  if (!points || points.length === 0) return { line: '', area: '' }
  const plotWidth = width - padding * 2
  const plotHeight = height - padding * 2

  const minVal = Math.min(...points)
  const maxVal = Math.max(...points)
  const range = maxVal - minVal > 0 ? maxVal - minVal : 1

  const coords = points.map((val, idx) => {
    const x = padding + (idx / Math.max(points.length - 1, 1)) * plotWidth
    const norm = (val - minVal) / range
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
  timeLabels = ['16:05', '16:06', '16:07', '16:08', '16:09', '16:10'],
  dualSeries = null,
}) {
  const { line, area } = useMemo(() => generateSplinePath(series, 450, height, 12), [series, height])
  const dual = useMemo(() => {
    if (!dualSeries) return null
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
  const [selectedTargets, setSelectedTargets] = useState({
    cq_ct: true,
    sc_ct: true,
    cq_cu: true,
    sc_cu: true,
    cq_cm: true,
    sc_cm: true,
  })

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

  const resource = node.resource || {}
  const rate = rates[nodeUuid] || {}

  const memUsed = node.memUsed ?? resource.memory_used_bytes ?? 188219392
  const memTotal = node.memTotal ?? resource.memory_total_bytes ?? 1002700800
  const swapUsed = node.swapUsed ?? resource.swap_used_bytes ?? 0
  const swapTotal = node.swapTotal ?? resource.swap_total_bytes ?? 2147483648
  const diskUsed = node.diskUsed ?? resource.filesystem_used_bytes ?? 7730941132
  const diskTotal = node.diskTotal ?? resource.filesystem_total_bytes ?? 26199300096

  const rawTx = numeric(node.tx) || 0
  const rawRx = numeric(node.rx) || 0
  const totalTraffic = (rawTx + rawRx) || 72.2 * 1024 * 1024 * 1024

  const cpuPercent = numeric(node.cpu) || 0.0
  const cpuModel = resource.cpu_name || resource.cpu_model || node.cpuModel || customMeta.cpuModel || 'Intel(R) Xeon(R) CPU E5-2680 v3 @ 2.50GHz (1 vCPU)'
  const cleanedCpuModel = (cpuModel || '')
    .replace(/\s*\(\s*\d+\s*(?:vCPU|vCPUs|核|core|cores)\s*\)/gi, '')
    .trim()
  const cpuBenchmarkUrl = `https://www.cpubenchmark.net/cpu_lookup.php?cpu=${encodeURIComponent(cleanedCpuModel || cpuModel)}`
  const publicIp = resource.ip || node.hostname || '103.159.207.11'
  const cores = resource.cpu_cores || 1
  const arch = node.arch || resource.arch || 'kvm'
  const os = node.os || resource.os || 'Ubuntu 26.04 LTS'
  const kernel = node.kernel || resource.kernel || '7.0.0-31-generic'
  const uptimeText = node.uptime || '13 天 17 小时 4 分钟'
  const ispText = customMeta.merchant || 'China Mobile (CMI) / Emagine Concept, Inc. - AS31972'

  const tcpCount = resource.tcp_conn_count || 67
  const udpCount = resource.udp_conn_count || 5
  const totalConnections = tcpCount + udpCount
  const processCount = resource.process_count || 106

  const trafficQuotaBytes = 1024 * 1024 * 1024 * 1024 // 1 TB
  const trafficPercent = Math.min(100, Math.max(0.1, ((totalTraffic / trafficQuotaBytes) * 100))).toFixed(1)

  // Top metric values
  const priceDisplay = `${calc.symbol || '$'}${billing.price || '5.03'}`
  const monthlyExpense = `${calc.symbol || '$'}${((billing.price || 5.03) / (billing.cycle === 'annual' ? 12 : 1)).toFixed(2)}`
  const remainingDays = calc.daysRemaining !== undefined ? calc.daysRemaining : 16
  const remainingValue = `¥${calc.remainingValueCNY ? calc.remainingValueCNY.toFixed(2) : '18.03'}`

  // Mock series points for realistic smooth curves
  const cpuSeries = [2, 5, 8, 25, 48, 52, 49, 45, 42, 38, 32, 28, 24, 20, 15, 8, 4, 1, 0, 0]
  const memSeries = [18, 18.2, 18.5, 18.3, 18.4, 18.6, 18.5, 18.5, 18.4, 18.5, 18.5, 18.5]
  const swapSeries = [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0]
  const diskSeries = [29.5, 29.5, 29.5, 29.5, 29.5, 29.5, 29.5, 29.5, 29.5, 29.5]
  const netDownSeries = [10, 15, 25, 45, 12, 10, 8, 140, 280, 420, 120, 45, 20, 15, 10]
  const netUpSeries = [5, 8, 12, 18, 10, 6, 4, 80, 190, 260, 95, 30, 15, 8, 5]
  const gpuSeries = [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0]
  const connSeries = [48, 55, 62, 70, 68, 65, 62, 68, 72, 74, 72, 71, 72]
  const procSeries = [105, 106, 106, 106, 107, 106, 106, 106, 106, 106]

  // Ping Targets
  const pingTargets = [
    { id: 'cq_ct', name: '重庆电信', latency: '319ms', loss: '10.17%', lossColor: 'text-amber', color: '#f43f5e' },
    { id: 'sc_ct', name: '四川电信', latency: '355ms', loss: '8.47%', lossColor: 'text-amber', color: '#38bdf8' },
    { id: 'cq_cu', name: '重庆联通', latency: '345ms', loss: '0.00%', lossColor: 'text-mint', color: '#f59e0b' },
    { id: 'sc_cu', name: '四川联通', latency: '343ms', loss: '0.00%', lossColor: 'text-mint', color: '#a855f7' },
    { id: 'cq_cm', name: '重庆移动', latency: '233ms', loss: '1.69%', lossColor: 'text-mint', color: '#10b981' },
    { id: 'sc_cm', name: '四川移动', latency: '255ms', loss: '0.00%', lossColor: 'text-mint', color: '#ec4899' },
  ]

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

  return (
    <section className="subpage komari-detail-page">
      {/* 1. 顶部导航与面包屑控制条 */}
      <div className="komari-nav-bar">
        <div className="komari-nav-left">
          <button type="button" className="komari-back-btn" onClick={onBack} title="返回服务器列表">
            <ArrowLeft size={16} />
          </button>
          <span className="komari-server-flag">{customMeta.customFlag !== '自动识别' ? customMeta.customFlag : (node.flag || '🌐')}</span>
          <h1 className="komari-server-title">{customMeta.customName || node.name}</h1>
          <span className="komari-status-tag online">
            <span className="status-dot-pulse" /> 在線
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
            {priceDisplay} <small className="text-muted">/ 月</small>
          </div>
        </div>

        {/* 2. 月均支出 */}
        <div className="komari-stat-card">
          <div className="komari-stat-head">
            <span className="komari-stat-label">月均支出</span>
            <Coins size={15} className="komari-stat-icon text-muted" />
          </div>
          <div className="komari-stat-value mono">
            {monthlyExpense} <small className="text-muted">/ 月</small>
          </div>
        </div>

        {/* 3. 剩余时间 */}
        <div className="komari-stat-card">
          <div className="komari-stat-head">
            <span className="komari-stat-label">剩余时间</span>
            <CalendarBlank size={15} className="komari-stat-icon text-muted" />
          </div>
          <div className="komari-stat-value mono text-mint font-bold">
            {remainingDays} <small className="text-mint font-normal">天</small>
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
                {formatBytes(rawTx)} / {formatBytes(rawRx)} <span className="text-muted">(配额 1.00 TB)</span>
              </span>
            </div>
            <div className="komari-info-row">
              <span className="komari-info-label">
                <TrendUp size={14} /> 近一天下行/上行
              </span>
              <span className="komari-info-val mono">
                ~ {formatRate(rate?.down || 4613734)} · ~ {formatRate(rate?.up || 7654604)}
              </span>
            </div>
            <div className="komari-info-row">
              <span className="komari-info-label">
                <ArrowsClockwise size={14} /> 网络速率
              </span>
              <span className="komari-info-val mono">
                ^ {formatRate(rate?.up || 122)} · v {formatRate(rate?.down || 313)}
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
            series={cpuSeries}
            strokeColor="#f97316"
            yMax="100%"
            yMid="50%"
            yMin="0%"
          />

          {/* 2. 内存与 Swap */}
          <KomariChartCard
            title="内存与 Swap"
            icon="🟣"
            badgeText={`${formatBytes(memUsed)} / ${formatBytes(memTotal)}`}
            series={memSeries}
            strokeColor="#38bdf8"
            dualSeries={{ data: swapSeries, color: '#f59e0b' }}
            yMax={`${formatBytes(memTotal)}`}
            yMid={`${formatBytes(memTotal / 2)}`}
            yMin="0 B"
          />

          {/* 3. 磁盘 */}
          <KomariChartCard
            title="磁盘"
            icon="🟢"
            badgeText={`${formatBytes(diskUsed)} / ${formatBytes(diskTotal)}`}
            series={diskSeries}
            strokeColor="#10b981"
            yMax={`${formatBytes(diskTotal)}`}
            yMid={`${formatBytes(diskTotal / 2)}`}
            yMin="0 B"
          />

          {/* 4. 实时网络 */}
          <KomariChartCard
            title="实时网络"
            icon="🔵"
            badgeText={`^ ${formatRate(rate?.up || 143)}  v ${formatRate(rate?.down || 147)}`}
            series={netDownSeries}
            strokeColor="#0284c7"
            dualSeries={{ data: netUpSeries, color: '#a855f7' }}
            yMax="500 KB/s"
            yMid="250 KB/s"
            yMin="0 B/s"
          />

          {/* 5. GPU 利用率 */}
          <KomariChartCard
            title="GPU 利用率"
            icon="🟢"
            badgeText="0.0%"
            series={gpuSeries}
            strokeColor="#10b981"
            yMax="100%"
            yMid="50%"
            yMin="0%"
          />

          {/* 6. 网络连接 */}
          <KomariChartCard
            title="网络连接"
            icon="🔴"
            badgeText={`TCP: ${tcpCount}  UDP: ${udpCount}`}
            series={connSeries}
            strokeColor="#ef4444"
            yMax="100"
            yMid="50"
            yMin="0"
          />

          {/* 7. 进程 */}
          <KomariChartCard
            title="进程"
            icon="🔵"
            badgeText={`${processCount}`}
            series={procSeries}
            strokeColor="#6366f1"
            yMax="120"
            yMid="60"
            yMin="0"
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
                  <span className="text-muted">0:00</span>
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
              <text x="10" y="24" fontSize="10" fill="currentColor" opacity="0.4" fontFamily="monospace">500</text>
              <text x="10" y="79" fontSize="10" fill="currentColor" opacity="0.4" fontFamily="monospace">400</text>
              <text x="10" y="134" fontSize="10" fill="currentColor" opacity="0.4" fontFamily="monospace">300</text>
              <text x="10" y="189" fontSize="10" fill="currentColor" opacity="0.4" fontFamily="monospace">200</text>

              {/* 多线绘制 */}
              {selectedTargets.cq_ct && (
                <path
                  d="M 20 85 Q 120 70 200 95 T 380 90 T 560 110 T 740 85 T 880 95"
                  fill="none"
                  stroke="#f43f5e"
                  strokeWidth="1.8"
                />
              )}
              {selectedTargets.sc_ct && (
                <path
                  d="M 20 70 Q 140 85 240 75 T 440 80 T 620 90 T 780 75 T 880 80"
                  fill="none"
                  stroke="#38bdf8"
                  strokeWidth="1.8"
                />
              )}
              {selectedTargets.cq_cu && (
                <path
                  d="M 20 75 Q 160 90 280 80 T 480 85 T 660 70 T 800 80 T 880 75"
                  fill="none"
                  stroke="#f59e0b"
                  strokeWidth="1.8"
                />
              )}
              {selectedTargets.sc_cu && (
                <path
                  d="M 20 76 Q 130 75 250 82 T 450 78 T 630 85 T 790 76 T 880 78"
                  fill="none"
                  stroke="#a855f7"
                  strokeWidth="1.8"
                />
              )}
              {selectedTargets.cq_cm && (
                <path
                  d="M 20 145 Q 110 135 220 148 T 420 140 T 600 135 T 760 145 T 880 140"
                  fill="none"
                  stroke="#10b981"
                  strokeWidth="1.8"
                />
              )}
              {selectedTargets.sc_cm && (
                <path
                  d="M 20 135 Q 150 145 270 138 T 470 142 T 650 138 T 810 140 T 880 136"
                  fill="none"
                  stroke="#ec4899"
                  strokeWidth="1.8"
                />
              )}
            </svg>

            {/* X 轴时间轴 */}
            <div className="komari-ping-x-axis mono">
              <span>15:07</span>
              <span>15:13</span>
              <span>15:19</span>
              <span>15:25</span>
              <span>15:31</span>
              <span>15:37</span>
              <span>15:43</span>
              <span>15:49</span>
              <span>15:55</span>
              <span>16:01</span>
              <span>16:05</span>
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
