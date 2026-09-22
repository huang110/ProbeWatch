import { ChartLineUp, CircleNotch, Pulse } from '@phosphor-icons/react'
import { numeric, safeArray, safeObject, formatAlertTime, formatTimeOfDay } from '../lib/format.js'

// 游客状态页：大屏风格，仅展示 /api/public/status 的脱敏聚合数据。
// 公开 API 不含单节点状态或在线时长，节点卡片只展示脱敏名称与装饰色环。
export function GuestView({ status, isRefreshing, onRefresh }) {
  const nodes = safeObject(status?.nodes)
  const checks = safeObject(status?.checks)
  const online = numeric(nodes.online)
  const total = numeric(nodes.total)
  const successRate = numeric(checks.success_rate)
  const avgLatency = numeric(checks.avg_latency_ms)
  const names = safeArray(nodes.names).map((name) => safeText(name)).filter(Boolean)
  const badge = total !== null && total > 0 ? (online !== null && online >= total ? { label: '全部正常', tone: 'ok' } : { label: '部分异常', tone: 'warn' }) : { label: '暂无数据', tone: 'muted' }
  const lastUpdated = status?.last_updated_at ? formatTimeOfDay(status.last_updated_at) : '—'
  return <main className="guest-shell guest-shell-wide">
    <header className="guest-header">
      <div className="brand-lockup"><div className="brand-mark"><Pulse size={21} weight="bold" /></div><div><strong>ProbeWatch</strong><span>公开状态页</span></div></div>
      <div className="heading-actions">
        <button className="button button-quiet" onClick={onRefresh} disabled={isRefreshing}>{isRefreshing ? <CircleNotch size={17} className="spin" /> : <ChartLineUp size={17} />}{isRefreshing ? '正在刷新' : '刷新数据'}</button>
        <button className="button button-primary" onClick={() => { window.location.href = '/auth/github' }}>使用 GitHub 登录</button>
      </div>
    </header>
    <section className="guest-hero guest-hero-center">
      <div className="guest-hero-mark"><Pulse size={30} weight="bold" /></div>
      <h1>服务状态<span className="heading-period">。</span></h1>
      <span className={`guest-badge guest-badge-${badge.tone}`} role="status">{badge.label}</span>
      <p>此页面面向未登录访客，仅展示脱敏的聚合状态数据；节点明细与告警需 GitHub 登录后查看。</p>
    </section>
    <section className="guest-stats-bar" aria-label="公开状态摘要">
      <div className="guest-stat"><span>节点在线</span><strong>{online !== null && total !== null ? `${online} / ${total}` : '—'}</strong></div>
      <div className="guest-stat"><span>平均延迟</span><strong>{avgLatency !== null ? `${avgLatency} ms` : '—'}</strong></div>
      <div className="guest-stat"><span>检测通过率</span><strong>{successRate !== null ? `${successRate}%` : '—'}</strong></div>
      <div className="guest-stat"><span>最近更新</span><strong>{lastUpdated}</strong></div>
    </section>
    <section className="guest-nodes">
      <div className="guest-nodes-heading"><h2>节点（脱敏名称）</h2><p>不包含 IP、UUID、内部 ID、资源明细或单节点状态</p></div>
      {names.length ? <div className="guest-node-grid">{names.map((name, index) => <article className="guest-node-card" key={`${name}-${index}`} title="公开状态页不包含单节点状态明细"><span className="guest-ring" aria-hidden="true" /><strong className="guest-node-name">{name}</strong><small>状态未公开 · 明细需登录</small></article>)}</div> : <div className="empty-state"><strong>暂无节点</strong><span>公开状态 API 尚未返回节点。</span></div>}
    </section>
    <footer className="guest-footer"><span className="footer-divider" /><span>公开页仅提供脱敏聚合数据 · 最后更新 {status?.last_updated_at ? formatAlertTime(status.last_updated_at) : '—'}</span></footer>
  </main>
}
