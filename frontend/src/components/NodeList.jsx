import { ArrowUpRight } from '@phosphor-icons/react'
import { dash, formatLossPercent, formatPercent, formatRate, relativeHeartbeat, safeText } from '../lib/format.js'
import { ProgressBar, EmptyState, StatusDot } from './Common.jsx'

const cellPercent = (value) => <span className={value !== null && value >= 85 ? 'cell-meter cell-meter-hot' : 'cell-meter'}>{formatPercent(value)}</span>

export function NodeTable({ nodes, rates = {}, lossRates = {}, selectedId = null, onSelect }) {
  const rows = nodes.map((node, index) => {
    const key = safeText(node.uuid) || safeText(node.id) || `node-${index}`
    const rate = rates[key] || null
    const loss = lossRates[key] ?? null
    return { node, key, rate, loss }
  })
  return <>
    <div className="table-scroll node-table-wrap">
      <table className="node-table">
        <thead>
          <tr><th>状态</th><th>节点</th><th>区域</th><th>CPU</th><th>内存</th><th>磁盘</th><th>网络 ↓/↑</th><th>丢包率</th><th>最后心跳</th><th aria-hidden="true" /></tr>
        </thead>
        <tbody>
          {rows.map(({ node, key, rate, loss }) => <tr key={key} tabIndex={0} className={`node-row node-row-${node.status} ${selectedId && selectedId === key ? 'node-row-selected' : ''}`} onClick={() => onSelect(node)} onKeyDown={(event) => { if (event.key === 'Enter') onSelect(node) }}>
            <td className="node-cell-status"><StatusDot status={node.status} /></td>
            <td className="node-cell-name"><span className={`node-avatar node-avatar-${node.color}`}><i aria-hidden="true">{node.name.slice(0, 1).toUpperCase()}</i></span><span className="node-name-text"><strong>{node.name}</strong><small>{node.id || '—'}</small></span></td>
            <td className="node-cell-region" title={node.hostname || undefined}>{node.hostname || '—'}</td>
            <td>{cellPercent(node.cpu)}</td>
            <td>{cellPercent(node.memory)}</td>
            <td>{cellPercent(node.disk)}</td>
            <td className="node-cell-network"><span>↓ {formatRate(rate?.down ?? null)}</span><span>↑ {formatRate(rate?.up ?? null)}</span></td>
            <td className={loss !== null && loss > 0 ? 'node-cell-loss node-cell-loss-warn' : 'node-cell-loss'}>{formatLossPercent(loss)}</td>
            <td className="node-cell-heartbeat">{relativeHeartbeat(node.lastReportedAt)}</td>
            <td className="node-cell-arrow"><ArrowUpRight size={14} /></td>
          </tr>)}
        </tbody>
      </table>
      {!rows.length && <EmptyState title="暂无节点数据" detail="API 返回了空节点数组，等待节点上报。" />}
    </div>
    <div className="node-card-list">
      {rows.map(({ node, key, rate, loss }) => <button type="button" key={key} className={`node-card ${selectedId && selectedId === key ? 'node-card-selected' : ''}`} onClick={() => onSelect(node)}>
        <span className="node-card-head"><span className={`node-avatar node-avatar-${node.color}`}>{node.name.slice(0, 1).toUpperCase()}</span><span className="node-card-title"><strong><StatusDot status={node.status} />{node.name}</strong><small>{node.hostname || node.id || '—'} · 心跳 {relativeHeartbeat(node.lastReportedAt)}</small></span></span>
        <span className="node-card-metrics">
          <span className="node-card-metric"><span>CPU {dash(node.cpu, '%')}</span><ProgressBar value={node.cpu} /></span>
          <span className="node-card-metric"><span>内存 {dash(node.memory, '%')}</span><ProgressBar value={node.memory} tone="blue" /></span>
          <span className="node-card-metric"><span>磁盘 {dash(node.disk, '%')}</span><ProgressBar value={node.disk} tone="amber" /></span>
        </span>
        <span className="node-card-foot"><span>↓ {formatRate(rate?.down ?? null)} · ↑ {formatRate(rate?.up ?? null)}</span><span>丢包 {formatLossPercent(loss)}</span></span>
      </button>)}
      {!rows.length && <EmptyState title="暂无节点数据" detail="API 返回了空节点数组，等待节点上报。" />}
    </div>
  </>
}
