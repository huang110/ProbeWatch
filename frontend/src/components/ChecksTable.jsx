import { numeric, safeArray, safeObject, dash, formatLossPercent, formatNumber, formatAlertTime } from '../lib/format.js'
import { EmptyState } from './Common.jsx'

export function ChecksSummaryPanel({ rows, loading = false }) {
  const normalized = safeArray(rows).map((row, index) => { const source = safeObject(row); return { key: `${safeText(source.target_id)}-${index}`, name: safeText(source.name) || safeText(source.target_id) || '—', kind: safeText(source.kind) || '—', total: numeric(source.total), success: numeric(source.success), failure: numeric(source.failure), lossRate: numeric(source.loss_rate), latency: numeric(source.latency_avg_ms), jitter: numeric(source.jitter_ms), lastChecked: source.last_checked_at ?? null } })
  const sorted = normalized.slice().sort((a, b) => (b.lossRate ?? -1) - (a.lossRate ?? -1))
  if (loading) return <EmptyState title="正在加载检测统计" />
  if (!sorted.length) return <EmptyState title="暂无数据" detail="checks/summary 暂无检测目标数据。" />
  return <div className="table-scroll"><table className="checks-table"><thead><tr><th>名称</th><th>类型</th><th>总数</th><th>成功</th><th>失败</th><th>丢包率</th><th>平均延迟ms</th><th>抖动ms</th><th>最近检测</th></tr></thead><tbody>{sorted.map((row) => <tr key={row.key} className={row.lossRate !== null && row.lossRate > 0 ? 'checks-row-warning' : ''}><td>{row.name}</td><td>{row.kind}</td><td>{dash(row.total)}</td><td>{dash(row.success)}</td><td>{dash(row.failure)}</td><td>{formatLossPercent(row.lossRate)}</td><td>{formatNumber(row.latency)}</td><td>{formatNumber(row.jitter)}</td><td>{formatAlertTime(row.lastChecked)}</td></tr>)}</tbody></table></div>
}
