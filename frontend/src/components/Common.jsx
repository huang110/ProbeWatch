import { useState } from 'react'
import { TrendDown, TrendUp } from '@phosphor-icons/react'
import { numeric, safeArray, formatTimeOfDay } from '../lib/format.js'

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

export function ProgressBar({ value, tone = 'dynamic', height = 4 }) {
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

/**
 * 哪吒探针 / DStatus 胶囊分段点阵进度条
 * 真实还原 VPS 卡片中标志性的一粒粒小胶囊状态条
 */
export function SegmentedBar({
  value = 0,
  max = 100,
  segments = 16,
  activeColor = '#3b82f6',
  className = '',
}) {
  const percent = max > 0 ? Math.min(Math.max((value / max) * 100, 0), 100) : 0
  const activeCount = Math.round((percent / 100) * segments)

  return (
    <div className={`segmented-bar ${className}`} aria-hidden="true">
      {Array.from({ length: segments }).map((_, i) => (
        <span
          key={i}
          className={`seg-dot ${i < activeCount ? 'is-active' : ''}`}
          style={i < activeCount ? { backgroundColor: activeColor } : undefined}
        />
      ))}
    </div>
  )
}

/**
 * 发行版 Distro 标志图标组件 (Debian 经典红漩涡 / Ubuntu / CentOS / Alpine / Linux)
 */
export function DistroIcon({ os = '', className = '' }) {
  const lower = (os || '').toLowerCase()
  if (lower.includes('debian')) {
    return (
      <svg className={`distro-icon distro-debian ${className}`} viewBox="0 0 32 32" width="18" height="18" fill="none" aria-label="Debian">
        <circle cx="16" cy="16" r="14" fill="#d70a53" fillOpacity="0.15" stroke="#d70a53" strokeWidth="1.5" />
        <path fill="#d70a53" d="M16 8 C11.58 8 8 11.58 8 16 C8 20.42 11.58 24 16 24 C19.31 24 22.14 21.99 23.34 19.12 C21.84 19.86 20.12 20.25 18.25 20.25 C14.38 20.25 11.25 17.12 11.25 13.25 C11.25 11.38 11.64 9.66 12.38 8.16 C13.51 8.06 14.73 8 16 8 Z" />
      </svg>
    )
  }
  if (lower.includes('ubuntu')) {
    return (
      <svg className={`distro-icon distro-ubuntu ${className}`} viewBox="0 0 32 32" width="18" height="18" aria-label="Ubuntu">
        <circle cx="16" cy="16" r="14" fill="#e95420"/>
        <circle cx="16" cy="16" r="8" fill="#ffffff"/>
        <circle cx="16" cy="16" r="5" fill="#e95420"/>
      </svg>
    )
  }
  if (lower.includes('alpine')) {
    return (
      <svg className={`distro-icon distro-alpine ${className}`} viewBox="0 0 32 32" width="18" height="18" fill="#0d597f" aria-label="Alpine">
        <path d="M14 6L4 24h6l4-7.5L18 24h6L14 6zm5.5 10.5L23 24h5L21.5 13.5l-2 3z"/>
      </svg>
    )
  }
  if (lower.includes('centos') || lower.includes('redhat') || lower.includes('almalinux') || lower.includes('rocky')) {
    return (
      <svg className={`distro-icon distro-centos ${className}`} viewBox="0 0 32 32" width="18" height="18" aria-label="CentOS">
        <rect x="6" y="6" width="9" height="9" fill="#93227f" rx="1"/>
        <rect x="17" y="6" width="9" height="9" fill="#e4572e" rx="1"/>
        <rect x="6" y="17" width="9" height="9" fill="#f3a712" rx="1"/>
        <rect x="17" y="17" width="9" height="9" fill="#2e86ab" rx="1"/>
      </svg>
    )
  }
  return (
    <svg className={`distro-icon distro-linux ${className}`} viewBox="0 0 32 32" width="18" height="18" fill="currentColor" aria-label="Linux">
      <path fill="#e11d48" d="M16 3C8.82 3 3 8.82 3 16s5.82 13 13 13 13-5.82 13-13S23.18 3 16 3zm0 2c6.075 0 11 4.925 11 11s-4.925 11-11 11S5 22.075 5 16 9.925 5 16 5zm-1 4a7 7 0 0 0-7 7 7 7 0 0 0 7 7 7 7 0 0 0 7-7 7 7 0 0 0-7-7zm0 2a5 5 0 0 1 5 5 5 5 0 0 1-5 5 5 5 0 0 1-5-5 5 5 0 0 1 5-5z"/>
    </svg>
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

export function RingGauge({ label, value, tone = 'mint', detail, series = [] }) {
  const numVal = numeric(value)
  const clamped = numVal === null ? null : Math.max(0, Math.min(100, numVal))
  const radius = 34
  const circumference = 2 * Math.PI * radius
  const offset = clamped === null ? circumference : circumference * (1 - clamped / 100)

  // 处理横向走势折线数据点
  const rawPoints = safeArray(series).map(numeric).filter((p) => p !== null && Number.isFinite(p))
  const linePoints = rawPoints.length >= 2 ? rawPoints : clamped !== null ? [clamped * 0.95, clamped] : []
  const count = linePoints.length

  let sparkPath = ''
  let sparkArea = ''
  let lastPt = null
  if (count >= 2) {
    const minVal = Math.min(...linePoints, 0)
    const maxVal = Math.max(...linePoints, 100)
    const valRange = Math.max(maxVal - minVal, 1)
    const baselineY = 38
    const topY = 6
    const plotH = baselineY - topY
    const slot = 100 / Math.max(count - 1, 1)

    const pts = linePoints.map((pt, i) => {
      const x = i * slot
      const y = baselineY - Math.min(((pt - minVal) / valRange) * plotH, plotH)
      return { x, y }
    })
    lastPt = pts[pts.length - 1]

    let line = `M ${pts[0].x.toFixed(2)} ${pts[0].y.toFixed(2)}`
    for (let i = 0; i < pts.length - 1; i++) {
      const p0 = pts[Math.max(i - 1, 0)]
      const p1 = pts[i]
      const p2 = pts[i + 1]
      const p3 = pts[Math.min(i + 2, pts.length - 1)]

      const cp1x = p1.x + (p2.x - p0.x) / 8
      const cp1y = Math.max(topY - 2, Math.min(baselineY, p1.y + (p2.y - p0.y) / 8))
      const cp2x = p2.x - (p3.x - p1.x) / 8
      const cp2y = Math.max(topY - 2, Math.min(baselineY, p2.y - (p3.y - p1.y) / 8))

      line += ` C ${cp1x.toFixed(2)} ${cp1y.toFixed(2)}, ${cp2x.toFixed(2)} ${cp2y.toFixed(2)}, ${p2.x.toFixed(2)} ${p2.y.toFixed(2)}`
    }
    sparkPath = line
    sparkArea = `${line} L ${pts[pts.length - 1].x.toFixed(2)} ${baselineY} L ${pts[0].x.toFixed(2)} ${baselineY} Z`
  }

  const toneColors = {
    mint: '#10b981',
    blue: '#3b82f6',
    amber: '#f59e0b',
    rose: '#f43f5e',
  }
  const color = toneColors[tone] || '#10b981'
  const formattedPct = clamped === null ? '—' : `${Math.round(clamped * 10) / 10}%`

  return (
    <div
      className={`ring-gauge ring-${tone} gauge-card-horizontal`}
      role="img"
      aria-label={`${label} ${clamped === null ? '暂无数据' : `${Math.round(clamped)}%`}`}
    >
      <div className="gauge-card-header">
        <div className="gauge-meta-left">
          <div className="gauge-label-row">
            <span className={`gauge-dot gauge-dot-${tone}`} />
            <span className="gauge-label-title">{label}</span>
          </div>
          <div className="ring-center">
            <strong>{formattedPct}</strong>
            <span>{label}</span>
          </div>
          {detail && <div className="ring-detail mono text-muted">{detail}</div>}
        </div>

        {/* 紧凑微型环形徽章（严格保留原 DOM 保证契约测试） */}
        <div className="ring-circle-compact" aria-hidden="true">
          <svg viewBox="0 0 88 88">
            <circle className="ring-track" cx="44" cy="44" r={radius} />
            <circle
              className="ring-fill"
              cx="44"
              cy="44"
              r={radius}
              strokeDasharray={circumference}
              strokeDashoffset={offset}
            />
          </svg>
        </div>
      </div>

      {/* 核心特色：横向折线图（实时走势流线） */}
      <div className="gauge-sparkline-row" title={`${label} 实时时序走势`}>
        {sparkPath ? (
          <svg className="gauge-sparkline-svg" viewBox="0 0 100 44" preserveAspectRatio="none" aria-hidden="true">
            <defs>
              <linearGradient id={`gauge-grad-${tone}`} x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor={color} stopOpacity="0.22" />
                <stop offset="100%" stopColor={color} stopOpacity="0.01" />
              </linearGradient>
            </defs>
            <line x1="0" y1="38" x2="100" y2="38" stroke="currentColor" strokeOpacity="0.08" strokeWidth="0.8" />
            <path d={sparkArea} fill={`url(#gauge-grad-${tone})`} />
            <path d={sparkPath} fill="none" stroke={color} strokeWidth="1.1" strokeLinecap="round" />
            {lastPt && (
              <>
                <circle cx={lastPt.x} cy={lastPt.y} r="2.8" fill={color} fillOpacity="0.25" />
                <circle cx={lastPt.x} cy={lastPt.y} r="1.8" fill={color} stroke="#ffffff" strokeWidth="0.6" />
              </>
            )}
          </svg>
        ) : (
          <div className="gauge-sparkline-placeholder mono text-muted">
            <small>横向折线采样中…</small>
          </div>
        )}
      </div>
      <div className="gauge-sparkline-footer mono" aria-hidden="true">
        <span className="sparkline-footer-left">横向时序流</span>
        <span className="sparkline-footer-right">{count > 1 ? `${count} 点` : '就绪'}</span>
      </div>
    </div>
  )
}

export function DualLineChart({ points = [] }) {
  const [hoveredIdx, setHoveredIdx] = useState(null)
  const safePoints = safeArray(points).filter((point) => point && (point.cpu !== null || point.mem !== null))
  if (safePoints.length < 2) return <EmptyState title="暂无历史数据" detail="资源历史 API 尚未返回足够的采样点。" />

  // 适度下采样（最多 60 个高精度点，保持横向折线极细丝滑）
  const maxDisplay = 60
  let displayPoints = safePoints
  if (safePoints.length > maxDisplay) {
    const step = safePoints.length / maxDisplay
    displayPoints = []
    for (let i = 0; i < maxDisplay; i++) {
      const idx = Math.min(Math.floor(i * step), safePoints.length - 1)
      displayPoints.push(safePoints[idx])
    }
    if (displayPoints[displayPoints.length - 1] !== safePoints[safePoints.length - 1]) {
      displayPoints[displayPoints.length - 1] = safePoints[safePoints.length - 1]
    }
  }

  const baselineY = 92
  const topY = 12
  const plotHeight = baselineY - topY
  const count = displayPoints.length
  const slot = 100 / Math.max(count - 1, 1)

  // 坐标映射
  const mappedPoints = displayPoints.map((p, index) => {
    const x = index * slot
    const cpuVal = p.cpu !== null && p.cpu !== undefined ? Math.max(0, Math.min(100, Number(p.cpu))) : null
    const memVal = p.mem !== null && p.mem !== undefined ? Math.max(0, Math.min(100, Number(p.mem))) : null
    const cpuY = cpuVal !== null ? baselineY - (cpuVal / 100) * plotHeight : baselineY
    const memY = memVal !== null ? baselineY - (memVal / 100) * plotHeight : baselineY
    return {
      ...p,
      x,
      cpuVal,
      memVal,
      cpuY,
      memY,
      index,
    }
  })

  // 贝塞尔流线生成
  const generateSpline = (pts, getY) => {
    const valid = pts.filter((p) => p.cpuVal !== null || p.memVal !== null)
    if (!valid.length) return { line: '', area: '' }
    if (pts.length === 1) {
      const y = getY(pts[0])
      return { line: `M ${pts[0].x.toFixed(2)} ${y.toFixed(2)}`, area: '' }
    }
    let line = `M ${pts[0].x.toFixed(2)} ${getY(pts[0]).toFixed(2)}`
    for (let i = 0; i < pts.length - 1; i++) {
      const p0 = pts[Math.max(i - 1, 0)]
      const p1 = pts[i]
      const p2 = pts[i + 1]
      const p3 = pts[Math.min(i + 2, pts.length - 1)]

      const y0 = getY(p0)
      const y1 = getY(p1)
      const y2 = getY(p2)
      const y3 = getY(p3)

      const cp1x = p1.x + (p2.x - p0.x) / 8
      const cp1y = Math.max(topY - 2, Math.min(baselineY, y1 + (y2 - y0) / 8))
      const cp2x = p2.x - (p3.x - p1.x) / 8
      const cp2y = Math.max(topY - 2, Math.min(baselineY, y2 - (y3 - y1) / 8))

      line += ` C ${cp1x.toFixed(2)} ${cp1y.toFixed(2)}, ${cp2x.toFixed(2)} ${cp2y.toFixed(2)}, ${p2.x.toFixed(2)} ${y2.toFixed(2)}`
    }
    const lastX = pts[pts.length - 1].x.toFixed(2)
    const firstX = pts[0].x.toFixed(2)
    const area = `${line} L ${lastX} ${baselineY} L ${firstX} ${baselineY} Z`
    return { line, area }
  }

  const cpuSpline = generateSpline(mappedPoints, (p) => p.cpuY)
  const memSpline = generateSpline(mappedPoints, (p) => p.memY)

  const activePoint = hoveredIdx !== null && mappedPoints[hoveredIdx] ? mappedPoints[hoveredIdx] : null
  const latestPoint = mappedPoints[mappedPoints.length - 1]

  const validCpus = mappedPoints.map((p) => p.cpuVal).filter((v) => v !== null)
  const validMems = mappedPoints.map((p) => p.memVal).filter((v) => v !== null)
  const maxCpu = validCpus.length ? Math.max(...validCpus) : null
  const maxMem = validMems.length ? Math.max(...validMems) : null

  return (
    <div className="dual-chart nezha-dual-chart-wrap" onMouseLeave={() => setHoveredIdx(null)}>
      {/* 哪吒 2.0 简约单行悬浮指示 / 瞬时状态栏 */}
      <div className={`traffic-hover-banner nezha-hover-banner ${activePoint ? 'active' : ''}`}>
        {activePoint ? (
          <div className="hover-badge-content">
            <span className="hover-stat mono text-mint">
              CPU: <b>{activePoint.cpuVal !== null && Number.isFinite(activePoint.cpuVal) ? `${activePoint.cpuVal.toFixed(1)}%` : '—'}</b>
            </span>
            <span className="hover-sep" aria-hidden="true" />
            <span className="hover-stat text-blue mono">
              内存: <b>{activePoint.memVal !== null && Number.isFinite(activePoint.memVal) ? `${activePoint.memVal.toFixed(1)}%` : '—'}</b>
            </span>
            {activePoint.disk !== undefined && activePoint.disk !== null && Number.isFinite(Number(activePoint.disk)) && (
              <>
                <span className="hover-sep" aria-hidden="true" />
                <span className="hover-stat text-amber mono">
                  磁盘: <b>{Number(activePoint.disk).toFixed(1)}%</b>
                </span>
              </>
            )}
            <span className="hover-sep" aria-hidden="true" />
            <span className="hover-stat muted mono">
              采样点 #{activePoint.index + 1}
            </span>
            {activePoint.time && (
              <>
                <span className="hover-sep" aria-hidden="true" />
                <span className="hover-stat text-1 mono">
                  采样时间: <b>{formatTimeOfDay(activePoint.time)}</b>
                </span>
              </>
            )}
          </div>
        ) : (
          <div className="hover-badge-summary mono">
            <span className="summary-pill pill-mint">
              <i />CPU 瞬时: <b>{latestPoint?.cpuVal !== null && Number.isFinite(latestPoint?.cpuVal) ? `${latestPoint.cpuVal.toFixed(1)}%` : '—'}</b>
              {maxCpu !== null && <small className="muted"> (峰值 {maxCpu.toFixed(1)}%)</small>}
            </span>
            <span className="summary-pill pill-blue">
              <i />内存 瞬时: <b>{latestPoint?.memVal !== null && Number.isFinite(latestPoint?.memVal) ? `${latestPoint.memVal.toFixed(1)}%` : '—'}</b>
              {maxMem !== null && <small className="muted"> (峰值 {maxMem.toFixed(1)}%)</small>}
            </span>
            <span className="summary-pill pill-muted summary-pill-right">
              哪吒 2.0 横向折线 · {count} 采样点
            </span>
          </div>
        )}
      </div>

      <div className="dual-chart-svg-wrap">
        <svg
          className="dual-chart-svg nezha-spline-svg"
          viewBox="0 0 100 100"
          preserveAspectRatio="none"
          role="img"
          aria-label="CPU 与内存资源历史横向折线图"
        >
          <defs>
            <linearGradient id="nezha-dual-cpu-grad" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor="#10b981" stopOpacity="0.16" />
              <stop offset="100%" stopColor="#10b981" stopOpacity="0.0" />
            </linearGradient>
            <linearGradient id="nezha-dual-mem-grad" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor="#3b82f6" stopOpacity="0.14" />
              <stop offset="100%" stopColor="#3b82f6" stopOpacity="0.0" />
            </linearGradient>
          </defs>

          {/* 刻度辅助虚线 */}
          <line className="nezha-grid-line" x1="0" y1={topY} x2="100" y2={topY} />
          <line className="nezha-grid-line" x1="0" y1={topY + plotHeight * 0.25} x2="100" y2={topY + plotHeight * 0.25} />
          <line className="nezha-grid-line" x1="0" y1={topY + plotHeight * 0.50} x2="100" y2={topY + plotHeight * 0.50} />
          <line className="nezha-grid-line" x1="0" y1={topY + plotHeight * 0.75} x2="100" y2={topY + plotHeight * 0.75} />
          <line className="nezha-grid-line" x1="0" y1={baselineY} x2="100" y2={baselineY} />

          {/* 渐变半透明面积 */}
          {cpuSpline.area && (
            <path className="dual-area dual-area-cpu nezha-area-fill" d={cpuSpline.area} fill="url(#nezha-dual-cpu-grad)" />
          )}
          {memSpline.area && (
            <path className="dual-area dual-area-mem nezha-area-fill" d={memSpline.area} fill="url(#nezha-dual-mem-grad)" />
          )}

          {/* 极细 0.95px 横向折线条 */}
          {cpuSpline.line && (
            <path
              className="dual-line dual-line-cpu nezha-line-stroke"
              d={cpuSpline.line}
              fill="none"
              stroke="#10b981"
              strokeWidth="0.95"
              strokeLinecap="round"
              strokeLinejoin="round"
            />
          )}
          {memSpline.line && (
            <path
              className="dual-line dual-line-mem nezha-line-stroke"
              d={memSpline.line}
              fill="none"
              stroke="#3b82f6"
              strokeWidth="0.95"
              strokeLinecap="round"
              strokeLinejoin="round"
            />
          )}

          {/* 鼠标交互十字准星 */}
          {mappedPoints.map((pt, idx) => {
            const isHovered = hoveredIdx === idx
            return (
              <g key={`dual-pt-${idx}`}>
                {isHovered && (
                  <line
                    className="nezha-crosshair"
                    x1={pt.x}
                    y1={topY - 3}
                    x2={pt.x}
                    y2={baselineY}
                  />
                )}
                {/* 捕捉交互区 */}
                <rect
                  x={Math.max(0, pt.x - slot / 2)}
                  y="0"
                  width={slot}
                  height="100"
                  fill="transparent"
                  style={{ cursor: 'crosshair' }}
                  onMouseEnter={() => setHoveredIdx(idx)}
                  onMouseMove={() => setHoveredIdx(idx)}
                />
              </g>
            )
          })}
        </svg>

        {/* 交互真圆高亮指示点（HTML 像素渲染，彻底杜绝 SVG 非等比拉伸导致圆点被压扁拉长） */}
        {activePoint && (
          <div className="nezha-chart-dots" aria-hidden="true">
            {activePoint.cpuVal !== null && (
              <div
                className="nezha-indicator-dot dot-cpu"
                style={{ left: `${activePoint.x}%`, top: `${activePoint.cpuY}%` }}
              />
            )}
            {activePoint.memVal !== null && (
              <div
                className="nezha-indicator-dot dot-mem"
                style={{ left: `${activePoint.x}%`, top: `${activePoint.memY}%` }}
              />
            )}
          </div>
        )}

        {/* Y 轴刻度标注（标准 HTML 浮层，彻底避免 SVG 非等比拉伸导致数字变形变大） */}
        <div className="nezha-y-axis-labels mono" aria-hidden="true">
          <span style={{ top: `${topY}%` }}>100%</span>
          <span style={{ top: `${topY + plotHeight * 0.5}%` }}>50%</span>
          <span style={{ top: `${baselineY}%` }}>0%</span>
        </div>
      </div>

      {/* X 轴时间刻度指示 */}
      {mappedPoints.length > 1 && (
        <div className="dual-time-axis nezha-time-axis mono" aria-hidden="true">
          <span>{formatTimeOfDay(mappedPoints[0].time)}</span>
          <span>{formatTimeOfDay(mappedPoints[Math.floor(mappedPoints.length / 2)].time)}</span>
          <span>{formatTimeOfDay(mappedPoints[mappedPoints.length - 1].time)}</span>
        </div>
      )}

      <div className="dual-legend mono" aria-hidden="true">
        <span className="legend-cpu"><i />CPU 使用率</span>
        <span className="legend-mem"><i />内存使用率</span>
        <span className="text-muted" style={{ marginLeft: 'auto' }}>
          横向折线采样点: {safePoints.length}
        </span>
      </div>
    </div>
  )
}
