import { TrendDown, TrendUp } from '@phosphor-icons/react'
import { numeric, safeArray } from '../lib/format.js'

export function StatusDot({ status }) { return <span className={`status-dot status-${status}`} aria-label={status === 'online' ? '在线' : status === 'attention' ? '需要关注' : status === 'offline' ? '离线' : '未知'} /> }

export function EmptyState({ title = '暂无数据', detail = '当前没有可展示的 API 数据。' }) { return <div className="empty-state"><strong>{title}</strong><span>{detail}</span></div> }

export function ProgressBar({ value, tone = 'mint' }) { return <div className="progress-track"><span className={`progress-fill progress-${tone}`} style={{ width: `${Math.max(0, Math.min(100, value ?? 0))}%` }} /></div> }

export function Sparkline({ tone = 'mint', points = [] }) {
  const safePoints = safeArray(points).map(numeric).filter((point) => point !== null)
  if (safePoints.length < 2) return <div className="sparkline-empty" aria-hidden="true" />
  const max = Math.max(...safePoints); const min = Math.min(...safePoints)
  const path = safePoints.map((point, index) => `${index ? 'L' : 'M'} ${(index / Math.max(safePoints.length - 1, 1)) * 100} ${88 - ((point - min) / Math.max(max - min, 1)) * 64}`).join(' ')
  return <svg className={`sparkline sparkline-${tone}`} viewBox="0 0 100 100" preserveAspectRatio="none" aria-hidden="true"><path className="sparkline-fill" d={`${path} L 100 100 L 0 100 Z`} /><path className="sparkline-line" d={path} /></svg>
}

export function StatCard({ icon: Icon, label, value, detail, trend, tone = 'mint', series = [] }) {
  return <article className={`stat-card stat-${tone}`}>
    <div className="stat-topline">
      <span className={`metric-icon metric-icon-${tone}`}><Icon size={16} weight="duotone" /></span>
      <span className="stat-label">{label}</span>
      {trend && <span className={trend.startsWith('+') ? 'trend trend-up' : 'trend trend-down'}>{trend.startsWith('+') ? <TrendUp size={12} /> : <TrendDown size={12} />}{trend}</span>}
    </div>
    <div className="stat-value">{value}</div>
    <div className="stat-detail">{detail}</div>
    <Sparkline tone={tone} points={series} />
  </article>
}

export function RingGauge({ label, value, tone = 'mint', detail }) {
  const clamped = value === null || value === undefined ? null : Math.max(0, Math.min(100, value))
  const radius = 34; const circumference = 2 * Math.PI * radius
  const offset = clamped === null ? circumference : circumference * (1 - clamped / 100)
  return <div className={`ring-gauge ring-${tone}`} role="img" aria-label={`${label} ${clamped === null ? '暂无数据' : `${Math.round(clamped)}%`}`}>
    <svg viewBox="0 0 88 88" aria-hidden="true">
      <circle className="ring-track" cx="44" cy="44" r={radius} />
      <circle className="ring-fill" cx="44" cy="44" r={radius} strokeDasharray={circumference} strokeDashoffset={offset} />
    </svg>
    <div className="ring-center"><strong>{clamped === null ? '—' : `${Math.round(clamped * 10) / 10}%`}</strong><span>{label}</span></div>
    {detail && <div className="ring-detail">{detail}</div>}
  </div>
}

export function DualLineChart({ points = [] }) {
  const safePoints = safeArray(points).filter((point) => point && (point.cpu !== null || point.mem !== null))
  if (safePoints.length < 2) return <EmptyState title="暂无历史数据" detail="资源历史 API 尚未返回足够的采样点。" />
  const build = (pick) => {
    const values = safePoints.map(pick)
    const valid = values.filter((value) => value !== null)
    if (!valid.length) return null
    const max = Math.max(...valid, 10); const min = Math.min(...valid, 0)
    return safePoints.map((value, index) => value === null ? null : `${index ? 'L' : 'M'} ${(index / Math.max(safePoints.length - 1, 1)) * 100} ${92 - ((value - min) / Math.max(max - min, 1)) * 80}`).filter(Boolean).join(' ')
  }
  const cpuPath = build((point) => point.cpu)
  const memPath = build((point) => point.mem)
  if (!cpuPath && !memPath) return <EmptyState title="暂无历史数据" detail="资源历史 API 尚未返回足够的采样点。" />
  return <div className="dual-chart">
    <svg className="dual-chart-svg" viewBox="0 0 100 100" preserveAspectRatio="none" role="img" aria-label="CPU 与内存资源历史曲线">
      {cpuPath && <path className="dual-area dual-area-cpu" d={`${cpuPath} L 100 100 L 0 100 Z`} />}
      {memPath && <path className="dual-area dual-area-mem" d={`${memPath} L 100 100 L 0 100 Z`} />}
      {cpuPath && <path className="dual-line dual-line-cpu" d={cpuPath} />}
      {memPath && <path className="dual-line dual-line-mem" d={memPath} />}
    </svg>
    <div className="dual-legend" aria-hidden="true"><span className="legend-cpu"><i />CPU</span><span className="legend-mem"><i />内存</span></div>
  </div>
}
