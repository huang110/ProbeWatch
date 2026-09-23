import { useEffect, useState } from 'react'
import { ArrowClockwise, Check, Copy, HardDrives, LinuxLogo, ShieldCheck, Terminal, X } from '@phosphor-icons/react'
import { fetchCsrfToken } from '../lib/api.js'
import { safeText } from '../lib/format.js'

const formatCountdown = (ms) => {
  const total = Math.max(0, Math.floor(ms / 1000))
  const minutes = Math.floor(total / 60)
  const seconds = total % 60
  return `${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')}`
}

export function AddServerModal({ onClose, onInstalled }) {
  const [enrollment, setEnrollment] = useState(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [copied, setCopied] = useState(false)
  const [activeTab, setActiveTab] = useState('linux') // 'linux' | 'env'
  const [now, setNow] = useState(() => Date.now())

  // Close on ESC
  useEffect(() => {
    const handleKeyDown = (e) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [onClose])

  // Countdown timer
  useEffect(() => {
    if (!enrollment) return undefined
    const id = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(id)
  }, [enrollment])

  const remaining = enrollment ? enrollment.expiresAt - now : 0
  const isExpired = enrollment ? remaining <= 0 : false

  const generateToken = async () => {
    setLoading(true)
    setError('')
    setCopied(false)
    try {
      const csrfToken = await fetchCsrfToken()
      const res = await fetch('/api/registration-tokens', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: '{}',
      })
      if (!res.ok) {
        if (res.status === 401) throw new Error('auth')
        throw new Error('fail')
      }
      const json = await res.json()
      const token = safeText(json?.registration_token)
      const endpoint = safeText(json?.endpoint) || `${window.location.origin}/api/agent/v1`
      const expiresAt = new Date(json?.expires_at).getTime()
      if (!token) throw new Error('invalid')

      setEnrollment({
        token,
        endpoint,
        nodeUuid: crypto.randomUUID(),
        expiresAt,
      })
      setNow(Date.now())
    } catch (err) {
      setError(err?.message === 'auth' ? '需要管理员登录后操作' : '生成接入 Token 失败，请检查主控连接')
    } finally {
      setLoading(false)
    }
  }

  // Auto-generate on mount
  useEffect(() => {
    generateToken()
  }, [])

  const originUrl = window.location.origin

  const linuxScript = enrollment
    ? `curl -sSL ${originUrl}/deploy/install.sh -o install.sh && sudo PROBEWATCH_AGENT_ENDPOINT="${enrollment.endpoint}" PROBEWATCH_AGENT_NODE_UUID="${enrollment.nodeUuid}" PROBEWATCH_AGENT_REGISTRATION_TOKEN="${enrollment.token}" bash install.sh agent`
    : ''

  const envScript = enrollment
    ? [
        `PROBEWATCH_ENV=production`,
        `PROBEWATCH_AGENT_ENDPOINT="${enrollment.endpoint}"`,
        `PROBEWATCH_AGENT_NODE_UUID="${enrollment.nodeUuid}"`,
        `PROBEWATCH_AGENT_REGISTRATION_TOKEN="${enrollment.token}"`,
        `PROBEWATCH_AGENT_DATA=/var/lib/probewatch`,
      ].join('\n')
    : ''

  const currentCommand = activeTab === 'linux' ? linuxScript : envScript

  const handleCopy = async () => {
    if (!currentCommand) return
    try {
      await navigator.clipboard.writeText(currentCommand)
      setCopied(true)
      setTimeout(() => setCopied(false), 2500)
    } catch {
      // Fallback
    }
  }

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="add-server-modal" onClick={(e) => e.stopPropagation()}>
        <div className="add-server-header">
          <div className="title-block">
            <div className="modal-icon-wrap">
              <Terminal size={22} weight="duotone" className="text-mint" />
            </div>
            <div>
              <h3>添加服务器 / 探针接入向导</h3>
              <p>哪吒/Komari 标准部署流程 · 15 分钟临时安全 Token</p>
            </div>
          </div>
          <button type="button" className="icon-button" onClick={onClose} aria-label="关闭">
            <X size={18} />
          </button>
        </div>

        {error ? (
          <div className="modal-error-box">
            <span>{error}</span>
            <button type="button" className="button button-primary btn-sm" onClick={generateToken}>
              重试
            </button>
          </div>
        ) : (
          <div className="add-server-body">
            <div className="install-tabs">
              <button
                type="button"
                className={`install-tab ${activeTab === 'linux' ? 'active' : ''}`}
                onClick={() => setActiveTab('linux')}
              >
                <LinuxLogo size={16} weight="fill" />
                <span>Linux 一键安装命令 (推荐)</span>
              </button>
              <button
                type="button"
                className={`install-tab ${activeTab === 'env' ? 'active' : ''}`}
                onClick={() => setActiveTab('env')}
              >
                <HardDrives size={16} />
                <span>系统环境配置 (Systemd / Docker)</span>
              </button>
            </div>

            <div className="token-meta-strip">
              <div className="meta-item">
                <span className="meta-label">临时 Token 状态:</span>
                <span className={`meta-val ${isExpired ? 'text-rose' : 'text-mint'}`}>
                  {isExpired ? '已过期' : `剩余有效时间: ${formatCountdown(remaining)}`}
                </span>
              </div>
              <button
                type="button"
                className="button button-quiet btn-xs"
                onClick={generateToken}
                disabled={loading}
                title="重新生成新的 Token"
              >
                <ArrowClockwise size={13} className={loading ? 'spin' : ''} />
                <span>重新生成</span>
              </button>
            </div>

            <div className="command-terminal-box">
              <div className="terminal-header">
                <div className="terminal-dots">
                  <span className="dot red" />
                  <span className="dot yellow" />
                  <span className="dot green" />
                </div>
                <span className="terminal-title">
                  {activeTab === 'linux' ? 'bash - 自动化安装脚本' : 'agent.env 环境变量文件'}
                </span>
                <button
                  type="button"
                  className={`terminal-copy-btn ${copied ? 'copied' : ''}`}
                  onClick={handleCopy}
                  disabled={isExpired || !enrollment}
                >
                  {copied ? <Check size={14} weight="bold" /> : <Copy size={14} />}
                  <span>{copied ? '已复制命令！' : '一键复制'}</span>
                </button>
              </div>
              <pre className="terminal-code mono">
                <code>{loading ? '# 正在向控制平面申请一次性注册 Token…' : currentCommand}</code>
              </pre>
            </div>

            <div className="install-tips-card">
              <div className="tip-row">
                <ShieldCheck size={16} className="text-mint flex-shrink-0" />
                <span><b>零入站安全</b>：探针安装后完全无需开放服务器任何端口，以单向安全主动上报方式接入。</span>
              </div>
              <div className="tip-row">
                <span className="tip-dot" />
                <span>支持主流发行版：Ubuntu 20/22/24、Debian 10/11/12、Alpine、CentOS/RHEL、Rocky Linux 等 (x86_64 / arm64)。</span>
              </div>
              <div className="tip-row">
                <span className="tip-dot" />
                <span>被控端运行在专用 <code>probewatch</code> 非 root 账号下，默认配置 Systemd 沙箱加固与开机自启。</span>
              </div>
            </div>
          </div>
        )}

        <div className="modal-footer-actions">
          {onInstalled && (
            <button
              type="button"
              className="button button-quiet"
              onClick={onInstalled}
              title="已在服务器运行完成，刷新节点列表"
            >
              已执行，刷新列表
            </button>
          )}
          <button type="button" className="button button-quiet" onClick={onClose}>
            关闭
          </button>
          <button
            type="button"
            className="button button-primary"
            onClick={handleCopy}
            disabled={isExpired || !enrollment}
          >
            {copied ? <Check size={15} weight="bold" /> : <Copy size={15} />}
            <span>{copied ? '命令已在剪贴板' : '复制命令并在服务器粘贴运行'}</span>
          </button>
        </div>
      </div>
    </div>
  )
}
