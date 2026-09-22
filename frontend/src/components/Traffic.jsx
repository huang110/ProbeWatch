import { numeric, safeArray, safeObject, formatBytes } from '../lib/format.js'
import { EmptyState } from './Common.jsx'

export function TrafficBars({ series }) {
  const points = safeArray(series).map((point) => { const source = safeObject(point); return { time: source.time ?? null, rx: numeric(source.rx_bytes), tx: numeric(source.tx_bytes) } })
  const values = points.flatMap((point) => [point.rx, point.tx]).filter((value) => value !== null)
  if (!points.length || !values.length) return <EmptyState title="暂无数据" detail="流量序列暂无数据点。" />
  const max = Math.max(...values)
  const scale = max > 0 ? 92 / max : 0
  const slot = 100 / points.length
  const barWidth = Math.min(slot * 0.34, 3)
  return <svg className="traffic-bars" viewBox="0 0 100 100" preserveAspectRatio="none" role="img" aria-label="窗口流量序列柱状图">{points.map((point, index) => { const center = index * slot + slot / 2; const rxHeight = point.rx !== null ? Math.max(point.rx * scale, point.rx > 0 ? 1.5 : 0) : 0; const txHeight = point.tx !== null ? Math.max(point.tx * scale, point.tx > 0 ? 1.5 : 0) : 0; return <g key={point.time !== null ? `t-${point.time}` : `i-${index}`}>{point.rx !== null && <rect className="traffic-bar-rx" x={center - barWidth - 0.3} y={100 - rxHeight} width={barWidth} height={rxHeight} />}{point.tx !== null && <rect className="traffic-bar-tx" x={center + 0.3} y={100 - txHeight} width={barWidth} height={txHeight} />}</g> })}</svg>
}

export function TrafficPanel({ traffic, loading = false, period = 'day', onPeriodChange }) {
  const source = safeObject(traffic)
  const rx = numeric(source.rx_bytes)
  const tx = numeric(source.tx_bytes)
  const resets = (numeric(source.rx_resets) ?? 0) + (numeric(source.tx_resets) ?? 0)
  return <div className="traffic-panel">
    <div className="traffic-toggle" role="group" aria-label="流量统计周期">{[['day', '日'], ['week', '周'], ['month', '月']].map(([value, label]) => <button key={value} type="button" className={period === value ? 'filter-active' : ''} onClick={() => onPeriodChange(value)}>{label}</button>)}</div>
    <div className="traffic-summary"><span>接收 <b>{rx !== null ? formatBytes(rx) : '—'}</b></span><span>发送 <b>{tx !== null ? formatBytes(tx) : '—'}</b></span><span className={resets > 0 ? 'traffic-reset traffic-reset-warn' : 'traffic-reset'}>计数器重置 {resets} 次</span></div>
    {resets > 0 && <p className="traffic-reset-hint">窗口内计数器发生重置，总增量为重置后的累计值。</p>}
    {loading ? <EmptyState title="正在加载流量数据" /> : <TrafficBars series={source.series} />}
    <div className="traffic-legend" aria-hidden="true"><span className="legend-rx"><i />接收 rx</span><span className="legend-tx"><i />发送 tx</span></div>
  </div>
}
