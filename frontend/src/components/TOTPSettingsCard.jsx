import { useEffect, useState } from 'react'
import { Clock, ShieldCheck } from '@phosphor-icons/react'
import { safeText } from '../lib/format.js'
import { EmptyState } from './Common.jsx'

const totpSubmitCode = (value) => {
  const clean = safeText(value).replace(/[\s-]/g, '')
  return /^\d{6}$/.test(clean) ? clean : null
}

async function fetchTOTPCsrf() {
  const response = await fetch('/api/csrf', { credentials: 'same-origin' })
  if (response.status === 401) throw new Error('auth')
  if (!response.ok) throw new Error('csrf')
  const token = response.headers.get('X-CSRF-Token')
  if (!token) throw new Error('csrf')
  return token
}

export function TOTPSettingsCard({ interval = 30, onIntervalChange }) {
  const [setup, setSetup] = useState(null)
  const [loadError, setLoadError] = useState('')
  const [code, setCode] = useState('')
  const [busy, setBusy] = useState(false)
  const [status, setStatus] = useState(null)

  const refreshSetup = async () => {
    const response = await fetch('/api/totp/setup', { credentials: 'same-origin' })
    if (response.status === 401) throw new Error('auth')
    if (!response.ok) throw new Error('setup')
    const json = await response.json()
    if (!json || typeof json !== 'object' || Array.isArray(json)) throw new Error('setup')
    setSetup(json)
  }

  useEffect(() => {
    let cancelled = false
    refreshSetup().catch((error) => {
      if (!cancelled) {
        setLoadError(error?.message === 'auth' ? '需要登录后才能管理两步验证。' : '无法加载两步验证状态，请稍后重试。')
      }
    })
    return () => {
      cancelled = true
    }
  }, [])

  const submit = async (action) => {
    const clean = totpSubmitCode(code)
    if (!clean) {
      setStatus({ kind: 'error', message: '请输入 6 位数字验证码。' })
      return
    }
    setBusy(true)
    setStatus(null)
    try {
      const csrfToken = await fetchTOTPCsrf()
      const response = await fetch(`/api/totp/${action}`, {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({ code: clean }),
      })
      if (response.status === 401) throw new Error('auth')
      if (response.status === 403) throw new Error('code')
      if (response.status === 409) throw new Error('conflict')
      if (!response.ok) throw new Error('error')
      setCode('')
      await refreshSetup()
      setStatus({ kind: 'ok', message: action === 'enable' ? '两步验证已启用。' : '两步验证已禁用。' })
    } catch (error) {
      const messages = {
        auth: '需要登录后才能管理两步验证。',
        code: '验证码错误或安全校验未通过，请刷新后重试。',
        conflict: '两步验证状态已变化，请重试。',
        csrf: '无法获取安全令牌，请刷新后重试。',
        error: '操作失败，请稍后重试。',
      }
      setStatus({ kind: 'error', message: messages[error?.message] || messages.error })
    } finally {
      setBusy(false)
    }
  }

  const form = (action, label, tone) => (
    <form
      className="totp-form"
      onSubmit={(event) => {
        event.preventDefault()
        submit(action)
      }}
    >
      <input
        className="totp-input"
        inputMode="numeric"
        autoComplete="one-time-code"
        maxLength={7}
        placeholder="6 位验证码"
        aria-label="两步验证码"
        value={code}
        onChange={(event) => setCode(event.target.value)}
      />
      <button className={`button ${tone}`} disabled={busy}>
        {busy ? '正在提交…' : label}
      </button>
    </form>
  )

  return (
    <>
      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>两步验证（TOTP）</h2>
            <p>为管理员登录增加第二重保护 · 兼容 RFC 6238 验证器应用</p>
          </div>
          <span className="metric-icon metric-icon-mint">
            <ShieldCheck size={17} weight="duotone" />
          </span>
        </div>
        {status && (
          <div className={`api-state api-state-${status.kind === 'ok' ? 'ok' : 'error'}`} role="status">
            {status.message}
          </div>
        )}
        {loadError ? (
          <EmptyState title="两步验证不可用" detail={loadError} />
        ) : !setup ? (
          <EmptyState title="正在加载两步验证状态" />
        ) : setup.enabled ? (
          <div className="totp-setup">
            <div className="policy-list detail-list">
              <div>
                <span>当前状态</span>
                <b>已启用 · 登录需要输入验证码</b>
              </div>
            </div>
            <p className="totp-hint">输入验证器应用当前显示的 6 位验证码以禁用两步验证。</p>
            {form('disable', '禁用两步验证', 'button-quiet')}
          </div>
        ) : (
          <div className="totp-setup">
            <p className="totp-hint">1. 使用验证器应用（如 Google Authenticator、Aegis）扫描下方 otpauth 链接，或手动输入密钥。</p>
            <div className="policy-list detail-list">
              <div>
                <span>密钥（Base32）</span>
                <b className="totp-secret">{safeText(setup.secret, '—')}</b>
              </div>
              <div>
                <span>otpauth 链接</span>
                <b className="totp-secret">{safeText(setup.otpauth_url, '—')}</b>
              </div>
            </div>
            <p className="totp-hint">2. 输入应用当前显示的 6 位验证码完成启用。启用前未确认的密钥会在刷新时更换。</p>
            {form('enable', '启用两步验证', 'button-primary')}
          </div>
        )}
      </div>

      {/* 数据轮询与图表时序采样时间设置 */}
      <div className="panel" style={{ marginTop: '16px' }}>
        <div className="panel-header">
          <div>
            <h2>数据轮询与图表采样时间设置</h2>
            <p>配置控制台自动刷新、探针时序聚合与流量/检测目标图表的采样周期基准</p>
          </div>
          <span className="metric-icon metric-icon-blue">
            <Clock size={17} weight="duotone" />
          </span>
        </div>
        <div className="policy-list detail-list">
          <div>
            <span>当前时序基准时间</span>
            <b className="mono text-mint">{interval} 秒</b>
          </div>
          <div>
            <span>快速切换</span>
            <div className="button-group" role="group" aria-label="图表采样与轮询时间">
              {[10, 30, 60, 120, 300].map((sec) => (
                <button
                  key={sec}
                  type="button"
                  className={`button ${interval === sec ? 'button-primary' : 'button-quiet'} btn-sm`}
                  onClick={() => onIntervalChange && onIntervalChange(sec)}
                >
                  {sec === 30 ? '30秒 (默认推荐)' : sec < 60 ? `${sec}秒` : `${sec / 60}分钟`}
                </button>
              ))}
            </div>
          </div>
          <div>
            <span>作用范围</span>
            <span className="muted" style={{ fontSize: '12px' }}>
              窗口流量曲线、探测目标时序、节点资源轮询与顶栏时钟同步
            </span>
          </div>
        </div>
      </div>
    </>
  )
}
