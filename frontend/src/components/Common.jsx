import { TrendDown, TrendUp } from '@phosphor-icons/react'
import { numeric, safeArray } from '../lib/format.js'

export function StatusDot({ status, size = 'md' }) {
  return (
    <span className={`status-dot-wrap status-${status} size-${size}`} title={status === 'online' ? '在线运行中' : status === 'attention' ? '需要关注' : '离线'}>
      <span className="status-dot-ping" aria-hidden="true" />
      <span className="status-dot-core" />
    </span>
  )
}

export function EmptyState({ title = '暂无数据', detail = '当前没有可展示的 API 数据。' }) {
  return (
    <div className="empty-state">
      <strong>{title}</strong>
      <span>{detail}</span>
    </div>
  )
}

export function ProgressBar({ value, tone = 'dynamic', height = 6 }) {
  const num = numeric(value) ?? 0
  const clamped = Math.max(0, Math.min(100, num))
  
  let appliedTone = tone
  if (tone === 'dynamic') {
    if (clamped >= 85) appliedTone = 'rose'
    else if (clamped >= 65) appliedTone = 'amber'
    else appliedTone = 'mint'
  }

  return (
    <div className="progress-track" style={{ height: `${height}px` }}>
      <span
        className={`progress-fill progress-${appliedTone}`}
        style={{ width: `${clamped}%` }}
      />
    </div>
  )
}

export function UptimeBars({ count = 28, uptimePercent = 100 }) {
  const bars = Array.from({ length: count }, (_, i) => {
    // Make mostly healthy bars with the very occasional variation if uptime < 100
    const isHealthy = uptimePercent >= 99 || (i % 9 !== 0)
    return isHealthy ? 'healthy' : 'warn'
  })

  return (
    <div className="uptime-bars" title={`SLA 可用率 ${uptimePercent}%`}>
      {bars.map((state, i) => (
        <span key={i} className={`uptime-bar uptime-${state}`} />
      ))}
    </div>
  )
}

export function Sparkline({ tone = 'mint', points = [] }) {
  const safePoints = safeArray(points).map(numeric).filter((point) => point !== null)
  if (safePoints.length < 2) return <div className="sparkline-empty" aria-hidden="true" />
  const max = Math.max(...safePoints, 1)
  const min = Math.min(...safePoints, 0)
  const range = Math.max(max - min, 0.001)

  const path = safePoints
    .map((point, index) => {
      const x = (index / Math.max(safePoints.length - 1, 1)) * 100
      const y = 88 - ((point - min) / range) * 68
      return `${index ? 'L' : 'M'} ${x.toFixed(1)} ${y.toFixed(1)}`
    })
    .join(' ')

  return (
    <svg className={`sparkline sparkline-${tone}`} viewBox="0 0 100 100" preserveAspectRatio="none" aria-hidden="true">
      <defs>
        <linearGradient id={`spark-${tone}`} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor="currentColor" stopOpacity="0.25" />
          <stop offset="100%" stopColor="currentColor" stopOpacity="0.0" />
        </linearGradient>
      </defs>
      <path className="sparkline-fill" fill={`url(#spark-${tone})`} d={`${path} L 100 100 L 0 100 Z`} />
      <path className="sparkline-line" d={path} />
    </svg>
  )
}

export function StatCard({ icon: Icon, label, value, detail, trend, tone = 'mint', series = [] }) {
  return (
    <article className={`stat-card stat-${tone}`}>
      <div className="stat-topline">
        <div className="stat-header-group">
          <span className={`metric-icon metric-icon-${tone}`}>
            <Icon size={17} weight="duotone" />
          </span>
          <span className="stat-label">{label}</span>
        </div>
        {trend && (
          <span className={trend.startsWith('+') ? 'trend trend-up' : 'trend trend-down'}>
            {trend.startsWith('+') ? <TrendUp size={12} /> : <TrendDown size={12} />}
            {trend}
          </span>
        )}
      </div>
      <div className="stat-value">{value}</div>
      <div className="stat-detail">{detail}</div>
      <Sparkline tone={tone} points={series} />
    </article>
  )
}

export function RingGauge({ label, value, tone = 'mint', detail }) {
  const clamped = value === null || value === undefined ? null : Math.max(0, Math.min(100, value))
  const radius = 34
  const circumference = 2 * Math.PI * radius
  const offset = clamped === null ? circumference : circumference * (1 - clamped / 100)
  return (
    <div className={`ring-gauge ring-${tone}`} role="img" aria-label={`${label} ${clamped === null ? '暂无数据' : `${Math.round(clamped)}%`}`}>
      <svg viewBox="0 0 88 88" aria-hidden="true">
        <circle className="ring-track" cx="44" cy="44" r={radius} />
        <circle className="ring-fill" cx="44" cy="44" r={radius} strokeDasharray={circumference} strokeDashoffset={offset} />
      </svg>
      <div className="ring-center">
        <strong>{clamped === null ? '—' : `${Math.round(clamped * 10) / 10}%`}</strong>
        <span>{label}</span>
      </div>
      {detail && <div className="ring-detail">{detail}</div>}
    </div>
  )
}

export function DualLineChart({ points = [] }) {
  const safePoints = safeArray(points).filter((point) => point && (point.cpu !== null || point.mem !== null))
  if (safePoints.length < 2) return <EmptyState title="暂无历史数据" detail="资源历史 API 尚未返回足够的采样点。" />
  const build = (pick) => {
    const values = safePoints.map(pick)
    const valid = values.filter((value) => value !== null)
    if (!valid.length) return null
    const max = Math.max(...valid, 10)
    const min = Math.min(...valid, 0)
    return safePoints
      .map((value, index) => (value === null ? null : `${index ? 'L' : 'M'} ${(index / Math.max(safePoints.length - 1, 1)) * 100} ${92 - ((value - min) / Math.max(max - min, 1)) * 80}`))
      .filter(Boolean)
      .join(' ')
  }
  const cpuPath = build((point) => point.cpu)
  const memPath = build((point) => point.mem)
  if (!cpuPath && !memPath) return <EmptyState title="暂无历史数据" detail="资源历史 API 尚未返回足够的采样点。" />
  return (
    <div className="dual-chart">
      <svg className="dual-chart-svg" viewBox="0 0 100 100" preserveAspectRatio="none" role="img" aria-label="CPU 与内存资源历史曲线">
        {cpuPath && <path className="dual-area dual-area-cpu" d={`${cpuPath} L 100 100 L 0 100 Z`} />}
        {memPath && <path className="dual-area dual-area-mem" d={`${memPath} L 100 100 L 0 100 Z`} />}
        {cpuPath && <path className="dual-line dual-line-cpu" d={cpuPath} />}
        {memPath && <path className="dual-line dual-line-mem" d={memPath} />}
      </svg>
      <div className="dual-legend" aria-hidden="true">
        <span className="legend-cpu"><i />CPU</span>
        <span className="legend-mem"><i />内存</span>
      </div>
    </div>
  )
}
