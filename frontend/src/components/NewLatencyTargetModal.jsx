import { useEffect, useState } from 'react'
import { CaretDown, Check, CircleNotch, Plus, WifiHigh, X } from '@phosphor-icons/react'
import { fetchCsrfToken } from '../lib/api.js'

const PROTOCOL_OPTIONS = [
  { value: 'icmp', label: 'ICMP (Ping 探测)' },
  { value: 'tcp', label: 'TCP 端口握手' },
  { value: 'http', label: 'HTTP 状态码检测' },
  { value: 'https', label: 'HTTPS SSL / 状态检测' },
  { value: 'dns', label: 'DNS 解析响应' },
]

export function NewLatencyTargetModal({ isOpen, onClose, onCreated }) {
  const [name, setName] = useState('')
  const [kind, setKind] = useState('icmp')
  const [host, setHost] = useState('')
  const [port, setPort] = useState('80')
  const [path, setPath] = useState('/')
  const [dnsType, setDnsType] = useState('A')
  const [intervalSeconds, setIntervalSeconds] = useState(30)
  const [timeoutMs, setTimeoutMs] = useState(3000)
  const [enabled, setEnabled] = useState(true)

  const [loading, setLoading] = useState(false)
  const [errorMsg, setErrorMsg] = useState('')
  const [touched, setTouched] = useState(false)

  useEffect(() => {
    const handleKeyDown = (e) => {
      if (e.key === 'Escape' && isOpen) onClose()
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [isOpen, onClose])

  // 当协议变动时设置合理的默认端口
  const handleKindChange = (newKind) => {
    setKind(newKind)
    if (newKind === 'https') setPort('443')
    else if (newKind === 'dns') setPort('53')
    else if (newKind === 'http') setPort('80')
    else if (newKind === 'tcp') setPort('80')
    else setPort('0')
  }

  const handleSubmit = async (e) => {
    e.preventDefault()
    setTouched(true)
    setErrorMsg('')

    const trimmedName = name.trim()
    const trimmedHost = host.trim()

    if (!trimmedName) {
      setErrorMsg('请填写目标名称')
      return
    }
    if (!trimmedHost) {
      setErrorMsg('请填写目标地址（IP 或域名）')
      return
    }

    setLoading(true)
    try {
      const csrfToken = await fetchCsrfToken()
      const targetId = `target-${Date.now()}-${Math.random().toString(36).substring(2, 6)}`

      const payload = {
        id: targetId,
        name: trimmedName,
        kind,
        host: trimmedHost,
        interval_seconds: Number(intervalSeconds) || 30,
        timeout_ms: Number(timeoutMs) || 3000,
        enabled: Boolean(enabled),
      }

      if (kind === 'tcp' || kind === 'http' || kind === 'https' || kind === 'dns') {
        payload.port = Number(port) || 80
      }
      if (kind === 'http' || kind === 'https') {
        payload.path = path.startsWith('/') ? path : `/${path}`
      }
      if (kind === 'dns') {
        payload.dns_type = dnsType
      }

      const res = await fetch('/api/targets', {
        method: 'POST',
        credentials: 'same-origin',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken,
        },
        body: JSON.stringify(payload),
      })

      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error(data.error || '创建探测目标失败')
      }

      const created = await res.json().catch(() => ({}))
      if (onCreated) onCreated(created || payload)
      onClose()
    } catch (err) {
      setErrorMsg(err.message || '网络异常，提交失败')
    } finally {
      setLoading(false)
    }
  }

  if (!isOpen) return null

  return (
    <div className="modal-backdrop" onClick={onClose} role="dialog" aria-modal="true">
      <div className="modal-dialog route-modal-shell" onClick={(e) => e.stopPropagation()}>
        {/* Header */}
        <div className="modal-header">
          <div className="modal-title-group">
            <div className="modal-icon-badge text-blue">
              <WifiHigh size={20} weight="bold" />
            </div>
            <div>
              <h2 className="modal-title">新建延迟监测任务</h2>
              <p className="modal-subtitle">创建全网 Ping / TCP / HTTP / DNS 时延、抖动与丢包探测目标</p>
            </div>
          </div>
          <button type="button" className="icon-button modal-close" onClick={onClose} aria-label="关闭">
            <X size={18} />
          </button>
        </div>

        {errorMsg && (
          <div className="alert-banner-error" style={{ margin: '16px 20px 0' }}>
            <span>{errorMsg}</span>
          </div>
        )}

        <form onSubmit={handleSubmit}>
          <div className="modal-body space-y-4" style={{ maxHeight: 'calc(85vh - 140px)', overflowY: 'auto' }}>
            {/* 目标名称与协议 */}
            <div className="form-grid-2">
              <div className="field">
                <label className="field-label required">目标名称</label>
                <input
                  type="text"
                  className="field-input"
                  placeholder="如: 重庆移动 5G 核心"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  maxLength={64}
                  required
                />
              </div>

              <div className="field">
                <label className="field-label required">探测协议</label>
                <select
                  className="field-select"
                  value={kind}
                  onChange={(e) => handleKindChange(e.target.value)}
                >
                  {PROTOCOL_OPTIONS.map((p) => (
                    <option key={p.value} value={p.value}>
                      {p.label}
                    </option>
                  ))}
                </select>
              </div>
            </div>

            {/* 主机地址与端口 */}
            <div className="form-grid-2">
              <div className="field">
                <label className="field-label required">目标主机 / IP</label>
                <input
                  type="text"
                  className="field-input mono"
                  placeholder="如: 198.51.100.231 或 dns.example.com"
                  value={host}
                  onChange={(e) => setHost(e.target.value)}
                  required
                />
              </div>

              {kind !== 'icmp' && (
                <div className="field">
                  <label className="field-label required">端口</label>
                  <input
                    type="number"
                    className="field-input mono"
                    placeholder="80"
                    value={port}
                    onChange={(e) => setPort(e.target.value)}
                    min={1}
                    max={65535}
                    required
                  />
                </div>
              )}
            </div>

            {/* HTTP 路径 */}
            {(kind === 'http' || kind === 'https') && (
              <div className="field">
                <label className="field-label">请求路径</label>
                <input
                  type="text"
                  className="field-input mono"
                  placeholder="/"
                  value={path}
                  onChange={(e) => setPath(e.target.value)}
                />
              </div>
            )}

            {/* DNS 记录类型 */}
            {kind === 'dns' && (
              <div className="field">
                <label className="field-label">DNS 记录类型</label>
                <select
                  className="field-select"
                  value={dnsType}
                  onChange={(e) => setDnsType(e.target.value)}
                >
                  <option value="A">A 记录 (IPv4)</option>
                  <option value="AAAA">AAAA 记录 (IPv6)</option>
                  <option value="CNAME">CNAME 别名记录</option>
                </select>
              </div>
            )}

            {/* 探测周期与超时 */}
            <div className="form-grid-2">
              <div className="field">
                <label className="field-label">探测周期 (秒)</label>
                <input
                  type="number"
                  className="field-input mono"
                  value={intervalSeconds}
                  onChange={(e) => setIntervalSeconds(e.target.value)}
                  min={10}
                  max={3600}
                />
              </div>

              <div className="field">
                <label className="field-label">超时时间 (毫秒)</label>
                <input
                  type="number"
                  className="field-input mono"
                  value={timeoutMs}
                  onChange={(e) => setTimeoutMs(e.target.value)}
                  min={200}
                  max={10000}
                />
              </div>
            </div>

            {/* 默认启用 */}
            <div className="field-switch-row" style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '10px 0' }}>
              <div>
                <strong style={{ fontSize: '13px' }}>默认启用任务</strong>
                <p className="text-muted" style={{ fontSize: '11px', margin: '2px 0 0' }}>开启后所有关联服务器将自动按设定周期执行探测</p>
              </div>
              <button
                type="button"
                className={`switch-toggle ${enabled ? 'active' : ''}`}
                onClick={() => setEnabled(!enabled)}
                aria-pressed={enabled}
              >
                <span className="switch-thumb" />
              </button>
            </div>
          </div>

          {/* Footer Actions */}
          <div className="modal-footer">
            <button type="button" className="button button-quiet" onClick={onClose} disabled={loading}>
              取消
            </button>
            <button type="submit" className="button button-primary" disabled={loading}>
              {loading ? (
                <>
                  <CircleNotch size={14} className="spin" />
                  <span>正在创建…</span>
                </>
              ) : (
                <>
                  <Plus size={14} weight="bold" />
                  <span>立即创建</span>
                </>
              )}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
