import { useState } from 'react'
import { ChartLineUp, CircleNotch, LockKey, Pulse, SignIn, X } from '@phosphor-icons/react'
import { numeric, safeArray, safeObject, safeText, formatAlertTime, formatTimeOfDay } from '../lib/format.js'

// 游客状态页：大屏风格，仅展示 /api/public/status 的脱敏聚合数据。
// 公开 API 不含单节点状态或在线时长，节点卡片只展示脱敏名称与装饰色环。
export function GuestView({ status, isRefreshing, onRefresh, onLoginSuccess }) {
  const [showLogin, setShowLogin] = useState(false)
  const [password, setPassword] = useState('')
  const [loginLoading, setLoginLoading] = useState(false)
  const [loginError, setLoginError] = useState('')

  const handlePasswordLogin = async (e) => {
    e.preventDefault()
    if (!password) return
    setLoginLoading(true)
    setLoginError('')
    try {
      const res = await fetch('/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'same-origin',
        body: JSON.stringify({ password }),
      })
      if (!res.ok) {
        setLoginError('密码错误或未开启密码登录')
        setLoginLoading(false)
        return
      }
      setShowLogin(false)
      if (onLoginSuccess) {
        onLoginSuccess()
      } else {
        window.location.reload()
      }
    } catch {
      setLoginError('网络连接失败，请稍后重试')
    } finally {
      setLoginLoading(false)
    }
  }

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
        <button className="button button-quiet" onClick={onRefresh} disabled={isRefreshing}>
          {isRefreshing ? <CircleNotch size={17} className="spin" /> : <ChartLineUp size={17} />}
          {isRefreshing ? '正在刷新' : '刷新数据'}
        </button>
        <button className="button button-primary" onClick={() => setShowLogin(true)}>
          <SignIn size={17} />管理员登录
        </button>
      </div>
    </header>
    <section className="guest-hero guest-hero-center">
      <div className="guest-hero-mark"><Pulse size={30} weight="bold" /></div>
      <h1>服务状态<span className="heading-period">。</span></h1>
      <span className={`guest-badge guest-badge-${badge.tone}`} role="status">{badge.label}</span>
      <p>此页面面向未登录访客，仅展示脱敏的聚合状态数据；节点明细与告警需管理员登录后查看。</p>
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

    {showLogin && (
      <div style={{ position: 'fixed', inset: 0, backgroundColor: 'rgba(0,0,0,0.7)', zIndex: 9999, display: 'flex', alignItems: 'center', justifyContent: 'center', padding: '1rem' }} onClick={() => setShowLogin(false)}>
        <div style={{ background: '#1c2128', border: '1px solid #30363d', borderRadius: '12px', width: '100%', maxWidth: '380px', padding: '24px', boxShadow: '0 20px 40px rgba(0,0,0,0.5)', color: '#e6edf3' }} onClick={(e) => e.stopPropagation()}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '16px' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '8px', fontWeight: 600, fontSize: '1.1rem' }}>
              <LockKey size={20} /> 管理员登录
            </div>
            <button className="icon-button" onClick={() => setShowLogin(false)} aria-label="关闭"><X size={18} /></button>
          </div>
          <form onSubmit={handlePasswordLogin}>
            <div style={{ marginBottom: '16px' }}>
              <input
                type="password"
                className="totp-input"
                style={{ width: '100%', textAlign: 'left', padding: '10px 14px', fontSize: '0.95rem', borderRadius: '6px', background: '#0d1117', border: '1px solid #30363d', color: '#e6edf3' }}
                placeholder="请输入管理员密码"
                autoFocus
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
              {loginError && <div style={{ color: '#f85149', fontSize: '0.85rem', marginTop: '6px' }}>{loginError}</div>}
            </div>
            <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
              <button type="submit" className="button button-primary" style={{ width: '100%', justifyContent: 'center' }} disabled={loginLoading}>
                {loginLoading ? '登录中…' : '口令登录'}
              </button>
              <div style={{ display: 'flex', alignItems: 'center', margin: '6px 0', gap: '8px', color: '#768390', fontSize: '0.8rem' }}>
                <div style={{ flex: 1, height: '1px', background: '#30363d' }} />
                <span>或</span>
                <div style={{ flex: 1, height: '1px', background: '#30363d' }} />
              </div>
              <button type="button" className="button button-quiet" style={{ width: '100%', justifyContent: 'center' }} onClick={() => { window.location.href = '/auth/github' }}>
                使用 GitHub 授权登录
              </button>
            </div>
          </form>
        </div>
      </div>
    )}
  </main>
}
