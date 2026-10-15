import { useEffect, useState } from 'react'
import {
  ArrowClockwise,
  CaretDown,
  CaretRight,
  Check,
  Copy,
  DownloadSimple,
  HardDrives,
  LinuxLogo,
  ShieldCheck,
  Terminal,
  X,
} from '@phosphor-icons/react'
import { fetchCsrfToken } from '../lib/api.js'
import { safeText } from '../lib/format.js'

const formatCountdown = (ms) => {
  const total = Math.max(0, Math.floor(ms / 1000))
  const minutes = Math.floor(total / 60)
  const seconds = total % 60
  return `${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')}`
}

import { escapeShell } from '../lib/shellEscape.js'
export { escapeShell }

export function AddServerModal({ onClose, onInstalled }) {
  const [enrollment, setEnrollment] = useState(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [copied, setCopied] = useState(false)
  const [activeTab, setActiveTab] = useState('linux') // 'linux' | 'openwrt' | 'docker' | 'env'
  const [now, setNow] = useState(() => Date.now())

  // 自定义配置表单
  const [nodeName, setNodeName] = useState('')
  const [terminalEnabled, setTerminalEnabled] = useState(false)

  // 折叠高级配置
  const [showAdvanced, setShowAdvanced] = useState(false)
  const [dataDir, setDataDir] = useState('/var/lib/probewatch')
  const [httpProxy, setHttpProxy] = useState('')

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
      setError(
        err?.message === 'auth'
          ? '需要管理员登录后操作'
          : '生成接入 Token 失败，请检查主控连接'
      )
    } finally {
      setLoading(false)
    }
  }

  // Auto-generate on mount
  useEffect(() => {
    generateToken()
  }, [])

  const originUrl = window.location.origin

  // 拼接可选环境变量 (仅保留 Agent 后端真实支持的环境变量)
  const buildEnvSnippet = () => {
    const envs = []
    if (nodeName.trim()) {
      envs.push(`PROBEWATCH_AGENT_NAME="${escapeShell(nodeName.trim())}"`)
    }
    if (dataDir && dataDir !== '/var/lib/probewatch') {
      envs.push(`PROBEWATCH_AGENT_DATA="${escapeShell(dataDir.trim())}"`)
    }
    if (httpProxy.trim()) {
      envs.push(`HTTP_PROXY="${escapeShell(httpProxy.trim())}" HTTPS_PROXY="${escapeShell(httpProxy.trim())}"`)
    }
    return envs.length > 0 ? ` ${envs.join(' ')}` : ''
  }

  const terminalVal = terminalEnabled ? 'true' : 'false'
  const extraBashEnvs = buildEnvSnippet()

  const linuxScript = enrollment
    ? `curl -sSL ${originUrl}/deploy/install.sh -o install.sh && PROBEWATCH_AGENT_ENDPOINT="${escapeShell(enrollment.endpoint)}" PROBEWATCH_AGENT_NODE_UUID="${escapeShell(enrollment.nodeUuid)}" PROBEWATCH_AGENT_REGISTRATION_TOKEN="${escapeShell(enrollment.token)}" PROBEWATCH_AGENT_ENABLE_TERMINAL="${terminalVal}"${extraBashEnvs} sh install.sh agent`
    : ''

  const openwrtScript = enrollment
    ? `wget -qO /tmp/install.sh ${originUrl}/deploy/install.sh && PROBEWATCH_AGENT_ENDPOINT="${escapeShell(enrollment.endpoint)}" PROBEWATCH_AGENT_NODE_UUID="${escapeShell(enrollment.nodeUuid)}" PROBEWATCH_AGENT_REGISTRATION_TOKEN="${escapeShell(enrollment.token)}" PROBEWATCH_AGENT_ENABLE_TERMINAL="${terminalVal}"${extraBashEnvs} sh /tmp/install.sh agent`
    : ''

  const envScript = enrollment
    ? [
        `PROBEWATCH_ENV=production`,
        `PROBEWATCH_AGENT_ENDPOINT="${escapeShell(enrollment.endpoint)}"`,
        `PROBEWATCH_AGENT_NODE_UUID="${escapeShell(enrollment.nodeUuid)}"`,
        `PROBEWATCH_AGENT_REGISTRATION_TOKEN="${escapeShell(enrollment.token)}"`,
        `PROBEWATCH_AGENT_ENABLE_TERMINAL="${terminalVal}"`,
        `PROBEWATCH_AGENT_DATA="${escapeShell(dataDir)}"`,
        nodeName.trim() ? `PROBEWATCH_AGENT_NAME="${escapeShell(nodeName.trim())}"` : null,
        httpProxy.trim() ? `HTTP_PROXY="${escapeShell(httpProxy.trim())}"` : null,
        httpProxy.trim() ? `HTTPS_PROXY="${escapeShell(httpProxy.trim())}"` : null,
      ]
        .filter(Boolean)
        .join('\n')
    : ''

  const currentCommand =
    activeTab === 'linux'
      ? linuxScript
      : activeTab === 'openwrt'
      ? openwrtScript
      : activeTab === 'env'
      ? envScript
      : ''

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
              <h3>添加服务器 / 探针接入</h3>
              <p>单向主动安全上报 · 跨架构支持 · 15 分钟一次性临时 Token</p>
            </div>
          </div>
          <button
            type="button"
            className="icon-button"
            onClick={onClose}
            aria-label="关闭"
          >
            <X size={18} />
          </button>
        </div>

        {error ? (
          <div className="modal-error-box">
            <span>{error}</span>
            <button
              type="button"
              className="button button-primary btn-sm"
              onClick={generateToken}
            >
              重试
            </button>
          </div>
        ) : (
          <div className="add-server-body">
            {/* 1. 基础节点设置 (Step 1) */}
            <div className="lite-field">
              <label className="lite-field-label">节点名称 (可选)</label>
              <input
                type="text"
                className="lite-input"
                placeholder="留空自动识别服务器主机名"
                value={nodeName}
                onChange={(e) => setNodeName(e.target.value)}
                maxLength={64}
              />
              <p className="lite-field-subtext" style={{ margin: '4px 0 0' }}>
                留空自动识别主机名。服务器接入后可在服务器管理中自由配置分组标签与地域信息。
              </p>
            </div>

            {/* 远程终端权限开关 */}
            <div className="lite-field-switch-row">
              <div>
                <strong style={{ fontSize: '13px' }}>允许远程终端 (Web SSH)</strong>
                <p className="lite-field-subtext" style={{ margin: '2px 0 0' }}>
                  默认关闭。开启后允许通过主控台建立加密终端连接
                </p>
              </div>
              <button
                type="button"
                className={`switch-toggle ${terminalEnabled ? 'active' : ''}`}
                onClick={() => setTerminalEnabled(!terminalEnabled)}
                aria-pressed={terminalEnabled}
              >
                <span className="switch-thumb" />
              </button>
            </div>

            {/* 高级选项折叠 */}
            <div className="lite-advanced-section" style={{ margin: 0 }}>
              <button
                type="button"
                className="lite-advanced-toggle"
                onClick={() => setShowAdvanced(!showAdvanced)}
              >
                <span>高级选项 (数据目录、网络代理)</span>
                {showAdvanced ? <CaretDown size={14} /> : <CaretRight size={14} />}
              </button>

              {showAdvanced && (
                <div className="lite-advanced-panel">
                  <div className="lite-grid-2">
                    <div className="lite-field">
                      <label className="lite-field-label">数据存储目录</label>
                      <input
                        type="text"
                        className="lite-input mono"
                        value={dataDir}
                        onChange={(e) => setDataDir(e.target.value)}
                      />
                    </div>
                    <div className="lite-field">
                      <label className="lite-field-label">HTTP 代理 (可选)</label>
                      <input
                        type="text"
                        className="lite-input mono"
                        placeholder="http://127.0.0.1:7890"
                        value={httpProxy}
                        onChange={(e) => setHttpProxy(e.target.value)}
                      />
                    </div>
                  </div>
                </div>
              )}
            </div>

            {/* 2. 安装方式 Tab */}
            <div className="install-tabs">
              <button
                type="button"
                className={`install-tab ${activeTab === 'linux' ? 'active' : ''}`}
                onClick={() => setActiveTab('linux')}
              >
                <LinuxLogo size={16} weight="fill" />
                <span>Linux 一键安装 (推荐)</span>
              </button>
              <button
                type="button"
                className={`install-tab ${activeTab === 'openwrt' ? 'active' : ''}`}
                onClick={() => setActiveTab('openwrt')}
              >
                <Terminal size={16} />
                <span>OpenWrt / 软路由</span>
              </button>
              <button
                type="button"
                className={`install-tab ${activeTab === 'docker' ? 'active' : ''}`}
                onClick={() => setActiveTab('docker')}
              >
                <HardDrives size={16} />
                <span>Docker 容器 (未发布)</span>
              </button>
              <button
                type="button"
                className={`install-tab ${activeTab === 'env' ? 'active' : ''}`}
                onClick={() => setActiveTab('env')}
              >
                <ShieldCheck size={16} />
                <span>环境变量 (ENV)</span>
              </button>
            </div>

            {/* Token 状态横条 */}
            <div className="token-meta-strip">
              <div className="meta-item">
                <span className="meta-label">一次性 Token 状态:</span>
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

            {/* Docker 镜像状态特别提示 / 终端代码预览框 */}
            {activeTab === 'docker' ? (
              <div
                style={{
                  padding: '24px 20px',
                  borderRadius: '10px',
                  background: 'rgba(239, 68, 68, 0.08)',
                  border: '1px solid rgba(239, 68, 68, 0.25)',
                  color: '#f87171',
                  fontSize: '13px',
                  textAlign: 'center',
                  marginBottom: '16px',
                }}
              >
                <div style={{ fontWeight: 600, fontSize: '15px', marginBottom: '8px' }}>
                  Docker 镜像尚未发布，请使用 Linux/OpenWrt 安装
                </div>
                <div style={{ fontSize: '12.5px', color: 'var(--text-muted)', lineHeight: 1.6, maxWidth: '480px', margin: '0 auto' }}>
                  ProbeWatch 当前仅发布并验证了 Linux (systemd/Alpine) 与 OpenWrt 原生二进制一键安装脚本，未发布可直接拉取的官方 Docker 镜像。请切换到 “Linux 一键安装” 或 “OpenWrt / 软路由” 选项卡执行部署。
                </div>
              </div>
            ) : (
              <div className="command-terminal-box">
                <div className="terminal-header">
                  <div className="terminal-dots">
                    <span className="dot red" />
                    <span className="dot yellow" />
                    <span className="dot green" />
                  </div>
                  <span className="terminal-title">
                    {activeTab === 'linux'
                      ? 'bash - 自动化安装脚本'
                      : activeTab === 'openwrt'
                      ? 'ash / sh - 软路由脚本'
                      : 'agent.env 环境变量文件'}
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
                  <code>
                    {loading
                      ? '# 正在向控制平面申请一次性注册 Token…'
                      : currentCommand}
                  </code>
                </pre>
              </div>
            )}

            {/* 下载 Agent 二进制与安全说明 */}
            <div className="lite-agent-dl-strip">
              <span className="lite-dl-title">手动运行或离线部署？</span>
              <div className="lite-dl-links">
                <a
                  href="/downloads/probewatch-agent-linux-amd64"
                  download
                  className="lite-dl-btn"
                >
                  <DownloadSimple size={13} />
                  <span>下载 Linux x86_64</span>
                </a>
                <a
                  href="/downloads/probewatch-agent-linux-arm64"
                  download
                  className="lite-dl-btn"
                >
                  <DownloadSimple size={13} />
                  <span>下载 Linux arm64</span>
                </a>
              </div>
            </div>

            <div className="install-tips-card">
              <div className="tip-row">
                <ShieldCheck size={16} className="text-mint flex-shrink-0" />
                <span>
                  <b>零入站安全</b>：探针安装后完全无需开放服务器任何端口，以单向安全主动上报方式接入。主控绝不索要也不留存私钥。
                </span>
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
            disabled={isExpired || !enrollment || activeTab === 'docker'}
          >
            {copied ? <Check size={15} weight="bold" /> : <Copy size={15} />}
            <span>{copied ? '命令已在剪贴板' : '复制命令并在服务器运行'}</span>
          </button>
        </div>
      </div>
    </div>
  )
}
