import { useState, useMemo } from 'react'
import {
  ArrowsDownUp,
  ChartLine,
  CheckCircle,
  Clock,
  Funnel,
  GlobeHemisphereWest,
  MagnifyingGlass,
  Rows,
  ShieldCheck,
  Sparkle,
  TrendUp,
  WarningCircle,
  WifiHigh,
} from '@phosphor-icons/react'
import {
  numeric,
  safeArray,
  safeObject,
  safeText,
  dash,
  formatLossPercent,
  formatNumber,
  formatAlertTime,
} from '../lib/format.js'
import { EmptyState } from './Common.jsx'

export function ChecksLatencyLines({ rows, samplingSec = 30 }) {
  const [hoveredIdx, setHoveredIdx] = useState(null)
  const targets = safeArray(rows).filter((r) => r && r.name)
  if (!targets.length) return null

  const validLatencies = targets.map((r) => r.latency).filter((l) => l !== null && l >= 0)
  const maxLat = Math.max(...validLatencies, 30)
  const count = targets.length
  const slot = 100 / Math.max(count, 1)

  // 构建时延走势曲线与面积路径
  const latPoints = targets.map((r, i) => {
    const x = i * slot + slot / 2
    const lat = r.latency !== null && r.latency >= 0 ? r.latency : 0
    const y = 88 - (lat / maxLat) * 72
    return { x, y, target: r }
  })

  const linePath = latPoints
    .map((p, i) => `${i ? 'L' : 'M'} ${p.x.toFixed(2)} ${p.y.toFixed(2)}`)
    .join(' ')
  const areaPath = `${linePath} L ${(count > 1 ? (count - 1) * slot + slot / 2 : 100).toFixed(2)} 99 L ${(slot / 2).toFixed(2)} 99 Z`

  const active = hoveredIdx !== null && latPoints[hoveredIdx] ? latPoints[hoveredIdx] : null

  return (
    <div className="checks-chart-wrap" onMouseLeave={() => setHoveredIdx(null)}>
      {/* 顶部悬浮动态卡片 */}
      <div className={`traffic-hover-banner checks-hover-banner ${active ? 'active' : ''}`}>
        {active ? (
          <div className="hover-badge-content">
            <span className="hover-stat mono" style={{ fontWeight: 600 }}>{active.target.name}</span>
            <span className="hover-sep">·</span>
            <span className="hover-stat text-mint mono">
              协议: <b>{active.target.kind}</b>
            </span>
            <span className="hover-sep">·</span>
            <span className="hover-stat text-blue mono">
              平均延迟: <b>{active.target.latency !== null ? `${formatNumber(active.target.latency)} ms` : '—'}</b>
            </span>
            <span className="hover-sep">·</span>
            <span className="hover-stat text-1 mono">
              丢包率: <b>{active.target.lossRate !== null ? formatLossPercent(active.target.lossRate) : '0%'}</b>
            </span>
            {active.target.jitter !== null && (
              <>
                <span className="hover-sep">·</span>
                <span className="hover-stat text-amber mono">
                  抖动: <b>{formatNumber(active.target.jitter)} ms</b>
                </span>
              </>
            )}
          </div>
        ) : (
          <div className="hover-badge-hint mono">
            <span>移动鼠标至探测目标曲线节点，查看瞬时时延与链路丢包</span>
          </div>
        )}
      </div>

      <div className="checks-svg-canvas">
        <svg
          className="checks-line-svg"
          viewBox="0 0 100 100"
          preserveAspectRatio="none"
          role="img"
          aria-label="探测目标质量与延迟曲线"
        >
          <defs>
            <linearGradient id="checks-lat-grad" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor="var(--blue)" stopOpacity="0.25" />
              <stop offset="100%" stopColor="var(--blue)" stopOpacity="0.0" />
            </linearGradient>
          </defs>

          {/* 参考水平基准线 */}
          <line className="traffic-grid-line" x1="0" y1="16" x2="100" y2="16" />
          <line className="traffic-grid-line" x1="0" y1="52" x2="100" y2="52" />
          <line className="traffic-grid-line" x1="0" y1="88" x2="100" y2="88" />

          {/* 渐变面积 */}
          {validLatencies.length > 0 && (
            <path className="checks-area-fill" d={areaPath} fill="url(#checks-lat-grad)" />
          )}

          {/* 延迟曲线 */}
          {validLatencies.length > 0 && (
            <path
              className="checks-stroke-line"
              d={linePath}
              fill="none"
              stroke="var(--blue)"
              strokeWidth="1.8"
              strokeLinecap="round"
              strokeLinejoin="round"
            />
          )}

          {/* 数据点与交互捕获 */}
          {latPoints.map((p, index) => {
            const isHovered = hoveredIdx === index
            const hasLoss = p.target.lossRate !== null && p.target.lossRate > 0
            const dotTone = hasLoss
              ? 'var(--rose)'
              : p.target.latency !== null && p.target.latency < 50
              ? 'var(--mint)'
              : 'var(--blue)'

            return (
              <g key={`check-pt-${index}`}>
                {isHovered && (
                  <>
                    <line
                      className="traffic-crosshair"
                      x1={p.x}
                      y1="0"
                      x2={p.x}
                      y2="100"
                      stroke="rgba(255, 255, 255, 0.35)"
                      strokeDasharray="2 2"
                      strokeWidth="0.8"
                    />
                    <circle
                      cx={p.x}
                      cy={p.y}
                      r="4.5"
                      fill={dotTone}
                      style={{ filter: 'drop-shadow(0 0 6px rgba(94, 106, 210, 0.6))' }}
                    />
                  </>
                )}
                <circle
                  cx={p.x}
                  cy={p.y}
                  r={isHovered ? 3.5 : 2}
                  fill={dotTone}
                  stroke="var(--bg-panel)"
                  strokeWidth="0.8"
                />
                {/* 鼠标捕获区 */}
                <rect
                  x={index * slot}
                  y="0"
                  width={slot}
                  height="100"
                  fill="transparent"
                  style={{ cursor: 'crosshair' }}
                  onMouseEnter={() => setHoveredIdx(index)}
                  onMouseMove={() => setHoveredIdx(index)}
                />
              </g>
            )
          })}
        </svg>
      </div>

      {/* X 轴目标名称指示 */}
      <div className="checks-time-axis mono">
        {targets.map((t, idx) => (
          <span
            key={`x-${idx}`}
            className={`axis-target-name ${hoveredIdx === idx ? 'axis-active' : ''}`}
            style={{ width: `${slot}%`, textAlign: 'center' }}
          >
            {t.name}
          </span>
        ))}
      </div>
    </div>
  )
}

export function ChecksSummaryPanel({ rows, loading = false }) {
  const [searchTerm, setSearchTerm] = useState('')
  const [kindFilter, setKindFilter] = useState('all')
  const [sortField, setSortField] = useState('lossRate')
  const [sortAsc, setSortAsc] = useState(false)
  const [viewMode, setViewMode] = useState('dual') // 'dual' (线条走势 + 清单) | 'table'
  const [samplingSec, setSamplingSec] = useState(30) // 默认 30 秒基准时间

  const normalized = useMemo(() => {
    return safeArray(rows).map((row, index) => {
      const source = safeObject(row)
      return {
        key: `${safeText(source.target_id)}-${index}`,
        name: safeText(source.name) || safeText(source.target_id) || '—',
        kind: safeText(source.kind) || '—',
        total: numeric(source.total),
        success: numeric(source.success),
        failure: numeric(source.failure),
        lossRate: numeric(source.loss_rate),
        latency: numeric(source.latency_avg_ms),
        jitter: numeric(source.jitter_ms),
        lastChecked: source.last_checked_at ?? null,
      }
    })
  }, [rows])

  // 顶部汇总统计 KPI
  const kpis = useMemo(() => {
    const totalTargets = normalized.length
    const totalChecks = normalized.reduce((acc, r) => acc + (r.total || 0), 0)
    const totalFailures = normalized.reduce((acc, r) => acc + (r.failure || 0), 0)
    const validLatencies = normalized.map((r) => r.latency).filter((l) => l !== null)
    const avgLatency =
      validLatencies.length > 0
        ? (validLatencies.reduce((a, b) => a + b, 0) / validLatencies.length).toFixed(1)
        : null
    const lossTargets = normalized.filter((r) => r.lossRate !== null && r.lossRate > 0).length
    const availability =
      totalChecks > 0 ? (((totalChecks - totalFailures) / totalChecks) * 100).toFixed(1) : null

    return {
      totalTargets,
      totalChecks,
      avgLatency,
      lossTargets,
      availability,
    }
  }, [normalized])

  // 过滤与排序
  const displayRows = useMemo(() => {
    let result = normalized.slice()

    // 1. 关键字搜索
    if (searchTerm.trim()) {
      const q = searchTerm.toLowerCase().trim()
      result = result.filter(
        (r) => r.name.toLowerCase().includes(q) || r.kind.toLowerCase().includes(q)
      )
    }

    // 2. 协议与状态筛选
    if (kindFilter === 'warn') {
      result = result.filter((r) => r.lossRate !== null && r.lossRate > 0)
    } else if (kindFilter === 'http') {
      result = result.filter((r) => r.kind.toLowerCase().includes('http'))
    } else if (kindFilter === 'tcp') {
      result = result.filter((r) => r.kind.toLowerCase() === 'tcp')
    } else if (kindFilter === 'dns') {
      result = result.filter((r) => r.kind.toLowerCase() === 'dns')
    } else if (kindFilter === 'icmp') {
      result = result.filter((r) => r.kind.toLowerCase() === 'icmp')
    }

    // 3. 排序 (默认 lossRate 降序)
    result.sort((a, b) => {
      let vA = a[sortField]
      let vB = b[sortField]

      if (sortField === 'lossRate') {
        vA = a.lossRate ?? -1
        vB = b.lossRate ?? -1
      } else if (sortField === 'latency') {
        vA = a.latency ?? (sortAsc ? 999999 : -1)
        vB = b.latency ?? (sortAsc ? 999999 : -1)
      } else if (sortField === 'total') {
        vA = a.total ?? -1
        vB = b.total ?? -1
      } else if (sortField === 'name') {
        vA = a.name.toLowerCase()
        vB = b.name.toLowerCase()
        return sortAsc ? vA.localeCompare(vB) : vB.localeCompare(vA)
      }

      if (vA < vB) return sortAsc ? -1 : 1
      if (vA > vB) return sortAsc ? 1 : -1
      return 0
    })

    return result
  }, [normalized, searchTerm, kindFilter, sortField, sortAsc])

  if (loading) return <EmptyState title="正在加载检测统计" />
  if (!normalized.length) return <EmptyState title="暂无数据" detail="checks/summary 暂无检测目标数据。" />

  const handleSort = (field) => {
    if (sortField === field) {
      setSortAsc(!sortAsc)
    } else {
      setSortField(field)
      setSortAsc(false)
    }
  }

  return (
    <div className="checks-summary-container">
      {/* 顶部指标看板 */}
      <div className="checks-kpi-ribbon">
        <div className="checks-kpi-card">
          <div className="kpi-card-header">
            <GlobeHemisphereWest size={15} className="text-mint" />
            <span>监控目标总数</span>
          </div>
          <div className="kpi-card-val mono text-1">
            {kpis.totalTargets} <small>项</small>
          </div>
          <div className="kpi-card-sub muted">全链路聚合探测节点</div>
        </div>

        <div className="checks-kpi-card">
          <div className="kpi-card-header">
            <ShieldCheck size={15} className="text-blue" />
            <span>网络平均可用率</span>
          </div>
          <div className="kpi-card-val mono text-mint">
            {kpis.availability !== null ? `${kpis.availability}%` : '—'}
          </div>
          <div className="kpi-card-sub muted">
            累计 {dash(kpis.totalChecks)} 次探测采样
          </div>
        </div>

        <div className="checks-kpi-card">
          <div className="kpi-card-header">
            <WifiHigh size={15} className="text-violet" />
            <span>全网平均延迟</span>
          </div>
          <div className="kpi-card-val mono text-blue">
            {kpis.avgLatency !== null ? `${kpis.avgLatency} ms` : '—'}
          </div>
          <div className="kpi-card-sub muted">有效响应目标加权均值</div>
        </div>

        <div className="checks-kpi-card">
          <div className="kpi-card-header">
            <WarningCircle size={15} className={kpis.lossTargets > 0 ? 'text-rose' : 'text-mint'} />
            <span>链路丢包异常</span>
          </div>
          <div className={`kpi-card-val mono ${kpis.lossTargets > 0 ? 'text-rose' : 'text-mint'}`}>
            {kpis.lossTargets > 0 ? `${kpis.lossTargets} 项丢包` : '链路全优 0 丢包'}
          </div>
          <div className="kpi-card-sub muted">
            {kpis.lossTargets > 0 ? '存在丢包隐患，需关注' : '所有目标均保持 100% 畅通'}
          </div>
        </div>
      </div>

      {/* 搜索、协议筛选与展示模式工具栏 */}
      <div className="checks-toolbar">
        <div className="checks-search-box">
          <MagnifyingGlass size={14} className="search-icon muted" />
          <input
            type="text"
            className="checks-search-input"
            placeholder="搜索探测目标名称或协议..."
            value={searchTerm}
            onChange={(e) => setSearchTerm(e.target.value)}
          />
          {searchTerm && (
            <button
              type="button"
              className="clear-search-btn"
              onClick={() => setSearchTerm('')}
            >
              ×
            </button>
          )}
        </div>

        <div className="checks-filter-tabs">
          {[
            { id: 'all', label: '全部' },
            { id: 'warn', label: `仅丢包${kpis.lossTargets > 0 ? ` (${kpis.lossTargets})` : ''}` },
            { id: 'http', label: 'HTTP(S)' },
            { id: 'tcp', label: 'TCP' },
            { id: 'dns', label: 'DNS' },
            { id: 'icmp', label: 'ICMP' },
          ].map((f) => (
            <button
              key={f.id}
              type="button"
              className={`checks-tab-btn ${kindFilter === f.id ? 'active' : ''}`}
              onClick={() => setKindFilter(f.id)}
            >
              {f.label}
            </button>
          ))}
        </div>

        {/* 线条走势与表格视图切换 */}
        <div className="checks-toolbar-right">
          <div className="view-mode-toggles" role="group" aria-label="目标展示视图">
            <button
              type="button"
              className={`view-toggle-btn ${viewMode === 'dual' ? 'active' : ''}`}
              onClick={() => setViewMode('dual')}
              title="线条曲线走势与详细清单"
            >
              <ChartLine size={13} weight="bold" />
              <span>线条走势</span>
            </button>
            <button
              type="button"
              className={`view-toggle-btn ${viewMode === 'table' ? 'active' : ''}`}
              onClick={() => setViewMode('table')}
              title="纯表格数据"
            >
              <Rows size={13} weight="bold" />
              <span>纯表格</span>
            </button>
          </div>

          <div
            className="traffic-period-badge mono"
            title="点击切换采样时间基准"
            onClick={() => setSamplingSec((s) => (s === 30 ? 60 : s === 60 ? 10 : 30))}
            style={{ cursor: 'pointer' }}
          >
            <Clock size={12} className="text-mint" />
            <span>基准时间: {samplingSec}秒</span>
          </div>
        </div>
      </div>

      {/* 线条走势视图 */}
      {viewMode === 'dual' && (
        <ChecksLatencyLines rows={displayRows} samplingSec={samplingSec} />
      )}

      {/* 目标质量表格（始终严格保留满足契约测试要求） */}
      <div className="table-scroll">
        <table className="checks-table modern-checks-table">
          <thead>
            <tr>
              <th className="sortable-th" onClick={() => handleSort('name')}>
                名称 {sortField === 'name' ? (sortAsc ? '↑' : '↓') : ''}
              </th>
              <th>类型</th>
              <th className="sortable-th" onClick={() => handleSort('total')}>
                总数 {sortField === 'total' ? (sortAsc ? '↑' : '↓') : ''}
              </th>
              <th>成功</th>
              <th>失败</th>
              <th className="sortable-th" onClick={() => handleSort('lossRate')}>
                丢包率 {sortField === 'lossRate' ? (sortAsc ? '↑' : '↓') : ''}
              </th>
              <th className="sortable-th" onClick={() => handleSort('latency')}>
                平均延迟ms {sortField === 'latency' ? (sortAsc ? '↑' : '↓') : ''}
              </th>
              <th>抖动ms</th>
              <th>最近检测</th>
            </tr>
          </thead>
          <tbody>
            {displayRows.map((row) => {
              const isWarning = row.lossRate !== null && row.lossRate > 0
              return (
                <tr
                  key={row.key}
                  className={isWarning ? 'checks-row-warning' : ''}
                >
                  <td className="checks-col-name">{row.name}</td>
                  <td>
                    <span
                      className={`check-kind-pill kind-${(row.kind || '')
                        .toLowerCase()
                        .replace(/[^a-z0-9]/g, '')}`}
                    >
                      {row.kind}
                    </span>
                  </td>
                  <td className="mono">{dash(row.total)}</td>
                  <td className="mono text-mint">{dash(row.success)}</td>
                  <td className="mono text-rose">{dash(row.failure)}</td>
                  <td className="mono">
                    {row.lossRate !== null ? (
                      <span className={`loss-pill ${isWarning ? 'loss-warn' : 'loss-clean'}`}>
                        {formatLossPercent(row.lossRate)}
                      </span>
                    ) : (
                      '—'
                    )}
                  </td>
                  <td className="mono">
                    {row.latency !== null ? (
                      <span
                        className={`lat-pill ${
                          row.latency < 50
                            ? 'lat-fast'
                            : row.latency < 120
                            ? 'lat-norm'
                            : 'lat-slow'
                        }`}
                      >
                        {formatNumber(row.latency)}
                      </span>
                    ) : (
                      '—'
                    )}
                  </td>
                  <td className="mono">{formatNumber(row.jitter)}</td>
                  <td className="mono text-muted">{formatAlertTime(row.lastChecked)}</td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
    </div>
  )
}
