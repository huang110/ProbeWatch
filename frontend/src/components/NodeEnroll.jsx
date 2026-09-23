import { useEffect, useState } from 'react'
import { ArrowClockwise, Check, Copy, PlugsConnected } from '@phosphor-icons/react'
import { fetchCsrfToken } from '../lib/api.js'
import { safeText } from '../lib/format.js'

const ENROLL_RULE = '注册 Token 15 分钟内有效，仅可注册一个节点，成功注册后立即失效。'

const formatCountdown = (ms) => {
  const total = Math.max(0, Math.floor(ms / 1000))
  const minutes = Math.floor(total / 60)
  const seconds = total % 60
  return `${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')}`
}

// 一次性接入面板：Token 只在内存中展示，不在浏览器本地留存。
export function NodeEnroll() {
  const [enrollment, setEnrollment] = useState(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [copied, setCopied] = useState(false)
  const [copyFailed, setCopyFailed] = useState(false)
  const [now, setNow] = useState(() => Date.now())

  useEffect(() => {
    if (!enrollment) return undefined
    const id = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(id)
  }, [enrollment])

  const remaining = enrollment ? enrollment.expiresAt - now : 0
  const expired = enrollment ? remaining <= 0 : false

  const generate = async () => {
    setBusy(true)
    setError('')
    setCopied(false)
    setCopyFailed(false)
    try {
      const csrfToken = await fetchCsrfToken()
      const response = await fetch('/api/registration-tokens', { method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken }, body: '{}' })
      if (response.status === 401) throw new Error('auth')
      if (response.status === 403) throw new Error('forbidden')
      if (!response.ok) throw new Error('error')
      const json = await response.json()
      const source = json && typeof json === 'object' && !Array.isArray(json) ? json : {}
      const token = safeText(source.registration_token)
      const endpoint = safeText(source.endpoint)
      const expiresAt = new Date(source.expires_at).getTime()
      if (!token || !endpoint || !Number.isFinite(expiresAt)) throw new Error('invalid')
      setEnrollment({ token, endpoint, nodeUuid: crypto.randomUUID(), expiresAt })
      setNow(Date.now())
    } catch (caught) {
      const messages = { auth: '需要登录后才能接入节点。', forbidden: '接入命令生成被拒绝，请刷新后重试。', invalid: '接入命令 API 返回了意外格式，请稍后重试。', error: '生成接入命令失败，请稍后重试。' }
      setError(messages[caught?.message] || messages.error)
    } finally {
      setBusy(false)
    }
  }

  const script = enrollment ? [
    `export PROBEWATCH_AGENT_ENDPOINT="${enrollment.endpoint}"`,
    `export PROBEWATCH_AGENT_NODE_UUID="${enrollment.nodeUuid}"`,
    `export PROBEWATCH_AGENT_REGISTRATION_TOKEN="${enrollment.token}"`,
    '',
    '# 下载 Agent 二进制后执行',
    './probewatch-agent',
  ].join('\n') : ''

  const copy = async () => {
    if (!enrollment) return
    setCopied(false)
    setCopyFailed(false)
    try {
      await navigator.clipboard.writeText(script)
      setCopied(true)
    } catch {
      setCopyFailed(true)
    }
  }

  return <div className="panel enroll-panel">
    <div className="panel-header">
      <div><h2>接入节点</h2><p>生成一次性注册 Token 与 Agent 安装命令。</p></div>
      <span className="metric-icon metric-icon-mint"><PlugsConnected size={17} weight="duotone" /></span>
    </div>
    {error && <div className="api-state api-state-error" role="status">{error}</div>}
    {!enrollment ? <div>
      <p className="enroll-hint">{ENROLL_RULE}生成后请立即复制使用。</p>
      <div className="enroll-actions">
        <button type="button" className="button button-primary" onClick={generate} disabled={busy}>{busy ? '正在生成…' : '生成接入命令'}</button>
      </div>
    </div> : <div>
      <div className="enroll-meta">
        <span>到期时间 <b className="mono">{new Date(enrollment.expiresAt).toLocaleString('zh-CN')}</b></span>
        <span className={`enroll-countdown ${expired ? 'enroll-countdown-expired' : ''}`}>{expired ? '已过期，请重新生成' : `剩余 ${formatCountdown(remaining)}`}</span>
      </div>
      <pre className="enroll-script mono">{script}</pre>
      <div className="enroll-actions">
        <button type="button" className="button button-primary" onClick={copy} disabled={expired}>{copied ? <><Check size={14} weight="bold" />已复制</> : <><Copy size={14} />复制安装命令</>}</button>
        <button type="button" className="button button-quiet" onClick={generate} disabled={busy}><ArrowClockwise size={14} />{busy ? '正在生成…' : '重新生成'}</button>
        {copyFailed && <span className="enroll-copy-failed">复制失败，请手动选择脚本内容复制。</span>}
      </div>
      <p className="enroll-warning">安全提示：{ENROLL_RULE}节点 UUID 由浏览器随机生成；本页面不会持久化 Token，刷新或关闭页面后即不可见，请勿将脚本泄露给不受信任的第三方。</p>
    </div>}
  </div>
}
