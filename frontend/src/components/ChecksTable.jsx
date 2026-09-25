import { useState, useMemo } from 'react'
import {
  ArrowsDownUp,
  CheckCircle,
  Clock,
  Funnel,
  GlobeHemisphereWest,
  MagnifyingGlass,
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
  formatTimeOfDay,
} from '../lib/format.js'
import { EmptyState } from './Common.jsx'

/**
 * Catmull-Rom 转三次贝塞尔平滑曲线
 */
function generateNezhaSpline(pts, bottomY = 88) {
  if (!pts || !pts.length) return { line: '', area: '' }
  if (pts.length === 1) {
    const p = pts[0]
    return {
      line: `M 0 ${p.y.toFixed(2)} L 100 ${p.y.toFixed(2)}`,
      area: `M 0 ${p.y.toFixed(2)} L 100 ${p.y.toFixed(2)} L 100 ${bottomY} L 0 ${bottomY} Z`,
    }
  }

  let line = `M ${pts[0].x.toFixed(2)} ${pts[0].y.toFixed(2)}`
  for (let i = 0; i < pts.length - 1; i++) {
    const p0 = pts[Math.max(i - 1, 0)]
    const p1 = pts[i]
    const p2 = pts[i + 1]
    const p3 = pts[Math.min(i + 2, pts.length - 1)]

    const cp1x = p1.x + (p2.x - p0.x) / 8
    const cp1y = Math.max(6, Math.min(bottomY - 1, p1.y + (p2.y - p0.y) / 8))
    const cp2x = p2.x - (p3.x - p1.x) / 8
    const cp2y = Math.max(6, Math.min(bottomY - 1, p2.y - (p3.y - p1.y) / 8))

    line += ` C ${cp1x.toFixed(2)} ${cp1y.toFixed(2)}, ${cp2x.toFixed(2)} ${cp2y.toFixed(2)}, ${p2.x.toFixed(2)} ${p2.y.toFixed(2)}`
  }

  const firstX = pts[0].x.toFixed(2)
  const lastX = pts[pts.length - 1].x.toFixed(2)
  const area = `${line} L ${lastX} ${bottomY} L ${firstX} ${bottomY} Z`
  return { line, area }
}

export function ChecksLatencyLines({ rows, networkHistory = [] }) {
  const [hoveredIdx, setHoveredIdx] = useState(null)
  const isTimeMode = Array.isArray(networkHistory) && networkHistory.length > 0
  const targets = safeArray(rows).filter((r) => r && r.name)

  if (!isTimeMode && !targets.length) return null

  const baselineY = 88
  const topY = 16
  const midY = (topY + baselineY) / 2
  const plotHeight = baselineY - topY

  let points = []
  let validLatencies = []
  let slot = 0
  let maxLat = 10

  if (isTimeMode) {
    // 按照检测采样时间正序排列（从早到晚）
    const sortedHistory = [...networkHistory].sort(
      (a, b) => new Date(a.checked_at || 0) - new Date(b.checked_at || 0)
    )

    validLatencies = sortedHistory
      .map((item) => {
        const res = item.result || {}
        return numeric(res.latency_ms ?? item.latency_ms ?? item.latency)
      })
      .filter((l) => l !== null && l >= 0 && Number.isFinite(l))

    const rawMax = validLatencies.length ? Math.max(...validLatencies) : 10
    maxLat = Math.max(Math.ceil(rawMax * 1.25), 5)
    const count = sortedHistory.length
    slot = 100 / Math.max(count, 1)

    points = sortedHistory.map((item, i) => {
      const res = item.result || {}
      const lat = numeric(res.latency_ms ?? item.latency_ms ?? item.latency)
      const hasLat = lat !== null && lat >= 0 && Number.isFinite(lat)
      const effectiveLat = hasLat ? lat : 0
      const x = i * slot + slot / 2
      const y = baselineY - Math.min((effectiveLat / maxLat) * plotHeight, plotHeight)
      const isOk = res.status === 'success' || (res.status_code && res.status_code < 400)
      const matched = targets.find(
        (t) => t.key?.startsWith(item.target_id) || t.name === item.target_id || t.target_id === item.target_id
      )
      const targetName = matched?.name || item.target_id || 'TCP 探测目标'
      const targetKind = (matched?.kind || 'TCP').toUpperCase()

      return {
        x,
        y,
        hasLat,
        latency: lat,
        time: item.checked_at,
        isOk,
        targetName,
        targetKind,
        error: res.error,
      }
    })
  } else {
    // 降级兼容：按目标列表横向排布
    validLatencies = targets
      .map((r) => r.latency)
      .filter((l) => l !== null && l >= 0 && Number.isFinite(l))
    const rawMax = validLatencies.length ? Math.max(...validLatencies) : 10
    maxLat = Math.max(Math.ceil(rawMax * 1.25), 5)
    const count = targets.length
    slot = 100 / Math.max(count, 1)

    points = targets.map((r, i) => {
      const x = i * slot + slot / 2
      const hasLat = r.latency !== null && r.latency >= 0 && Number.isFinite(r.latency)
      const lat = hasLat ? r.latency : 0
      const y = baselineY - Math.min((lat / maxLat) * plotHeight, plotHeight)
      return {
        x,
        y,
        hasLat,
        latency: r.latency,
        target: r,
        targetName: r.name,
        targetKind: r.kind,
        isOk: r.lossRate === 0 || r.lossRate === null,
        time: r.lastChecked,
      }
    })
  }

  const spline = generateNezhaSpline(
    points.map((p) => ({ x: p.x, y: p.y })),
    baselineY
  )

  const active = hoveredIdx !== null && points[hoveredIdx] ? points[hoveredIdx] : null

  return (
    <div className="checks-chart-wrap nezha-checks-wrap" onMouseLeave={() => setHoveredIdx(null)}>
      {/* 哪吒 2.0 简约单行悬浮指示 */}
      <div className={`traffic-hover-banner nezha-hover-banner ${active ? 'active' : ''}`}>
        {active ? (
          <div className="hover-badge-content">
            <span className="hover-stat mono" style={{ fontWeight: 600 }}>{active.targetName}</span>
            <span className="hover-sep" aria-hidden="true" />
            <span className="hover-stat text-mint mono">
              协议: <b>{active.targetKind}</b>
            </span>
            <span className="hover-sep" aria-hidden="true" />
            <span className="hover-stat text-blue mono">
              延迟: <b>{active.latency !== null ? `${formatNumber(active.latency)} ms` : '—'}</b>
            </span>
            <span className="hover-sep" aria-hidden="true" />
            <span className={`hover-stat mono ${active.isOk ? 'text-mint' : 'text-rose'}`}>
              状态: <b>{active.isOk ? '连通正常' : active.error || '连接异常'}</b>
            </span>
            {active.time && (
              <>
                <span className="hover-sep" aria-hidden="true" />
                <span className="hover-stat text-amber mono">
                  检测时间: <b>{formatTimeOfDay(active.time)}</b>
                </span>
              </>
            )}
            <span className="hover-sep" aria-hidden="true" />
            <span className="hover-stat muted mono">
              {isTimeMode ? '时序历史' : '统计窗口 24h'}
            </span>
          </div>
        ) : (
          <div className="hover-badge-hint mono">
            <Clock size={13} className="text-mint inline-icon" />
            <span>
              {isTimeMode
                ? '哪吒 2.0 TCP 延迟历史走势 · 鼠标滑过节点查看对应时间点的检测延迟与状态'
                : '哪吒 2.0 探测质量流线 · 鼠标滑过节点查看目标时延与丢包'}
            </span>
          </div>
        )}
      </div>

      <div className="checks-svg-canvas nezha-svg-canvas">
        <svg
          className="checks-line-svg nezha-spline-svg"
          viewBox="0 0 100 100"
          preserveAspectRatio="none"
          role="img"
          aria-label="TCP探测延迟走势图"
        >
          <defs>
            <linearGradient id="nezha-checks-lat-grad" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor="#6366f1" stopOpacity="0.12" />
              <stop offset="100%" stopColor="#6366f1" stopOpacity="0.0" />
            </linearGradient>
          </defs>

          {/* 参考水平极细虚线 */}
          <line className="nezha-grid-line" x1="0" y1={topY} x2="100" y2={topY} />
          <line className="nezha-grid-line" x1="0" y1={midY} x2="100" y2={midY} />
          <line className="nezha-grid-line" x1="0" y1={baselineY} x2="100" y2={baselineY} />

          {/* 渐变微透明面积 */}
          {validLatencies.length > 0 && (
            <path className="nezha-area-fill" d={spline.area} fill="url(#nezha-checks-lat-grad)" />
          )}

          {/* 哪吒 2.0 极细延迟线条 (0.95px) */}
          {validLatencies.length > 0 && (
            <path
              className="nezha-line-stroke nezha-line-tx"
              d={spline.line}
              fill="none"
              stroke="#6366f1"
              strokeWidth="0.95"
              strokeLinecap="round"
              strokeLinejoin="round"
            />
          )}

          {/* 交互交叉线与发光点 */}
          {points.map((p, index) => {
            const isHovered = hoveredIdx === index

            return (
              <g key={`check-pt-${index}`}>
                {isHovered && (
                  <line
                    className="nezha-crosshair"
                    x1={p.x}
                    y1={topY - 3}
                    x2={p.x}
                    y2={baselineY}
                  />
                )}
                {/* 鼠标灵敏捕捉区 */}
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

        {/* 交互真圆高亮指示点（HTML 像素渲染，彻底杜绝 SVG 非等比拉伸导致圆点被压扁拉长） */}
        {active && (
          <div className="nezha-chart-dots" aria-hidden="true">
            <div
              className={`nezha-indicator-dot ${
                !active.isOk
                  ? 'dot-rose'
                  : active.latency !== null && active.latency < 50
                  ? 'dot-mint'
                  : 'dot-blue'
              }`}
              style={{ left: `${active.x}%`, top: `${active.y}%` }}
            />
          </div>
        )}

        {/* Y 轴刻度标注（标准 HTML 浮层，彻底解决 SVG 非等比拉伸导致数字变形变大） */}
        {validLatencies.length > 0 && (
          <div className="nezha-y-axis-labels mono" aria-hidden="true">
            <span style={{ top: `${topY}%` }}>{maxLat} ms</span>
            <span style={{ top: `${midY}%` }}>{(maxLat / 2).toFixed(maxLat >= 10 ? 0 : 1)} ms</span>
            <span style={{ top: `${baselineY}%` }}>0 ms</span>
          </div>
        )}
      </div>

      {/* X 轴刻度指示：历史时序模式按时间显示，目标模式按目标名称显示 */}
      {isTimeMode ? (
        <div className="checks-time-axis nezha-time-axis mono" aria-hidden="true">
          <span>{formatTimeOfDay(points[0]?.time)}</span>
          {points.length > 2 && (
            <span>{formatTimeOfDay(points[Math.floor(points.length / 2)]?.time)}</span>
          )}
          <span>{formatTimeOfDay(points[points.length - 1]?.time)}</span>
        </div>
      ) : (
        <div className="checks-time-axis nezha-time-axis mono">
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
      )}
    </div>
  )
}

export function ChecksSummaryPanel({ rows, loading = false, networkHistory = [] }) {
  const [searchTerm, setSearchTerm] = useState('')
  const [kindFilter, setKindFilter] = useState('all')
  const [sortField, setSortField] = useState('lossRate')
  const [sortAsc, setSortAsc] = useState(false)

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
    const validLatencies = normalized
      .map((r) => r.latency)
      .filter((l) => l !== null && l >= 0 && Number.isFinite(l))

    // 加权平均延迟：按有效响应样本数加权
    const weightedSum = normalized.reduce(
      (acc, r) =>
        acc + (r.latency !== null && r.latency >= 0 && r.success ? r.latency * r.success : 0),
      0
    )
    const weightedSuccess = normalized.reduce(
      (acc, r) => (r.latency !== null && r.latency >= 0 && r.success ? acc + r.success : acc),
      0
    )
    const avgLatency =
      weightedSuccess > 0
        ? (weightedSum / weightedSuccess).toFixed(1)
        : validLatencies.length > 0
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
            {kpis.totalChecks > 0
              ? `累计 ${kpis.totalChecks.toLocaleString()} 次探测采样`
              : '暂无采样记录'}
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
          <div className="kpi-card-sub muted">有效响应样本加权均值</div>
        </div>

        <div className="checks-kpi-card">
          <div className="kpi-card-header">
            <WarningCircle
              size={15}
              className={kpis.lossTargets > 0 ? 'text-rose' : 'text-mint'}
            />
            <span>链路质量状态</span>
          </div>
          <div className={`kpi-card-val mono ${kpis.lossTargets > 0 ? 'text-rose' : 'text-mint'}`}>
            {kpis.totalChecks === 0
              ? '未开启探测'
              : kpis.lossTargets > 0
              ? `${kpis.lossTargets} 项丢包`
              : '链路全优 0 丢包'}
          </div>
          <div className="kpi-card-sub muted">
            {kpis.totalChecks === 0
              ? '当前周期暂无检测数据'
              : kpis.lossTargets > 0
              ? '存在丢包隐患，需关注'
              : '所有目标均保持 100% 畅通'}
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

        {/* 工具栏右侧：明确最近 24 小时聚合窗口指示 */}
        <div className="checks-toolbar-right">
          <div
            className="traffic-period-badge mono"
            title="checks/summary 统计窗口为最近 24 小时聚合"
          >
            <Clock size={12} className="text-mint" />
            <span>统计周期: 最近24小时</span>
          </div>
        </div>
      </div>

      {/* 平滑时延走势曲线视图 */}
      <ChecksLatencyLines rows={displayRows} networkHistory={networkHistory} />

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
