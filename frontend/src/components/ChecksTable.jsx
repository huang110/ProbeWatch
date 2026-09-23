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

export function ChecksSummaryPanel({ rows, loading = false }) {
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

      {/* 搜索与快捷过滤工具栏 */}
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
      </div>

      {/* 目标质量表格 */}
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
