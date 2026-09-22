import { useState } from 'react'
import { Pulse } from '@phosphor-icons/react'

export function TOTPVerifyPage() {
  const [code, setCode] = useState('')
  const [state, setState] = useState({ kind: 'idle', message: '' })
  const submit = async (event) => {
    event.preventDefault()
    const clean = code.replace(/[\s-]/g, '')
    if (!/^\d{6}$/.test(clean)) { setState({ kind: 'error', message: '请输入 6 位数字验证码。' }); return }
    setState({ kind: 'busy', message: '' })
    try {
      const response = await fetch('/auth/totp/verify', { method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ code: clean }) })
      if (response.ok) { window.location.href = '/'; return }
      const messages = { 401: '验证凭据已过期，请重新登录。', 403: '验证码错误，请重试。' }
      setState({ kind: 'error', message: messages[response.status] || '验证失败，请稍后重试。' })
    } catch { setState({ kind: 'error', message: '网络错误，请稍后重试。' }) }
  }
  return <main className="guest-shell">
    <header className="guest-header"><div className="brand-lockup"><div className="brand-mark"><Pulse size={21} weight="bold" /></div><div><strong>ProbeWatch</strong><span>两步验证</span></div></div><div className="heading-actions"><button className="button button-quiet" onClick={() => { window.location.href = '/auth/github' }}>重新登录</button></div></header>
    <section className="guest-hero"><div className="eyebrow">安全验证 · 该账号已启用两步验证</div><h1>输入验证码<span className="heading-period">。</span></h1><p>登录仍需完成第二步验证。请输入验证器应用中当前的 6 位验证码；验证凭据在 5 分钟内有效。</p></section>
    <section className="panel totp-verify-panel"><form className="totp-form" onSubmit={submit}><input className="totp-input totp-input-large" inputMode="numeric" autoComplete="one-time-code" autoFocus maxLength={7} placeholder="6 位验证码" aria-label="两步验证码" value={code} onChange={(event) => setCode(event.target.value)} /><button className="button button-primary" disabled={state.kind === 'busy'}>{state.kind === 'busy' ? '正在验证…' : '验证并登录'}</button></form>{state.kind === 'error' && <div className="api-state api-state-error" role="alert">{state.message}</div>}</section>
    <footer className="guest-footer"><span className="footer-divider" /><span>无法获取验证码时，请使用验证器应用重新同步或重新登录</span></footer>
  </main>
}
