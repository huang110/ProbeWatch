import { formatLossPercent, formatPercent } from '../lib/format.js'
import { EmptyState, ProgressBar } from './Common.jsx'

export function RankCard({ title, hint, icon: Icon, tone = 'mint', items }) {
  return <section className="rank-card panel">
    <div className="panel-header">
      <div><h2>{title}</h2><p>{hint}</p></div>
      {Icon && <span className={`metric-icon metric-icon-${tone}`}><Icon size={15} weight="duotone" /></span>}
    </div>
    {items.length ? <ol className="rank-list">{items.map((item, index) => <li key={item.key} className="rank-row">
      <span className="rank-index">{index + 1}</span>
      <span className="rank-body"><span className="rank-label">{item.label}</span><ProgressBar value={item.percent} tone={item.tone || 'mint'} /></span>
      <span className="rank-value">{item.kind === 'loss' ? formatLossPercent(item.value) : formatPercent(item.value)}</span>
    </li>)}</ol> : <EmptyState title="暂无数据" detail="当前没有可参与排行的节点数据。" />}
  </section>
}
