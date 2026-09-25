import { useEffect, useState } from 'react'
import { CaretDown, Check, CircleNotch, Globe, Info, X } from '@phosphor-icons/react'
import { fetchCsrfToken } from '../lib/api.js'
import { safeArray, safeText } from '../lib/format.js'

// 运营商与常用优质骨干线路智能映射
const ISP_OPTIONS = [
  { value: '中国移动', label: '中国移动', defaultRoute: 'CMIN2' },
  { value: '中国电信', label: '中国电信', defaultRoute: 'CN2 GIA' },
  { value: '中国联通', label: '中国联通', defaultRoute: 'AS9929' },
  { value: '教育网', label: '教育网 (CERNET)', defaultRoute: 'BGP直连' },
  { value: '其他', label: '其他 / 国际骨干', defaultRoute: 'BGP直连' },
]

const REGION_OPTIONS = [
  { value: '华东', label: '华东' },
  { value: '华南', label: '华南' },
  { value: '华北', label: '华北' },
  { value: '华中', label: '华中' },
  { value: '西南', label: '西南' },
  { value: '西北', label: '西北' },
  { value: '东北', label: '东北' },
  { value: '海外', label: '海外 / 国际' },
]

const ROUTE_OPTIONS = [
  'CMIN2',
  'CN2 GIA',
  'CN2 GT',
  'AS9929',
  'AS4837',
  '10099',
  '普通163/169',
  'BGP直连',
]

const PROTOCOL_OPTIONS = [
  { value: 'icmp', label: '内置 ICMP（推荐）' },
  { value: 'tcp', label: 'TCP 握手探测' },
  { value: 'udp', label: 'UDP 探测' },
]

export function NewRouteMonitorModal({ isOpen, onClose, nodes = [], onCreated }) {
  const [taskName, setTaskName] = useState('')
  const [selectedNodes, setSelectedNodes] = useState([])
  const [nodeDropdownOpen, setNodeDropdownOpen] = useState(false)
  const [isp, setIsp] = useState('中国移动')
  const [region, setRegion] = useState('华东')
  const [targetHost, setTargetHost] = useState('')
  const [ipType, setIpType] = useState('IPv4')
  const [expectedRoute, setExpectedRoute] = useState('CMIN2')
  const [protocol, setProtocol] = useState('icmp')
  const [intervalSeconds, setIntervalSeconds] = useState(180)
  const [switchThreshold, setSwitchThreshold] = useState(2)
  const [recoverThreshold, setRecoverThreshold] = useState(3)
  const [wallCheck, setWallCheck] = useState(false)
  const [cooldown, setCooldown] = useState(1800)
  const [notifySwitch, setNotifySwitch] = useState(true)
  const [notifyRecover, setNotifyRecover] = useState(true)
  const [enabled, setEnabled] = useState(true)

  const [loading, setLoading] = useState(false)
  const [errorMsg, setErrorMsg] = useState('')
  const [hostTouched, setHostTouched] = useState(false)

  // ESC 键关闭
  useEffect(() => {
    const handleKeyDown = (e) => {
      if (e.key === 'Escape' && isOpen) onClose()
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [isOpen, onClose])

  // 默认选中第一个节点
  useEffect(() => {
    if (nodes && nodes.length > 0 && selectedNodes.length === 0) {
      const firstId = safeText(nodes[0].uuid) || safeText(nodes[0].id)
      if (firstId) setSelectedNodes([firstId])
    }
  }, [nodes, selectedNodes.length])

  // 当运营商切换时，自动推荐对应最优预期线路与示例名称
  const handleIspChange = (newIsp) => {
    setIsp(newIsp)
    const match = ISP_OPTIONS.find((o) => o.value === newIsp)
    if (match) {
      setExpectedRoute(match.defaultRoute)
    }
  }

  const toggleNodeSelection = (id) => {
    setSelectedNodes((prev) =>
      prev.includes(id) ? prev.filter((i) => i !== id) : [...prev, id]
    )
  }

  const handleSubmit = async (e) => {
    e.preventDefault()
    setHostTouched(true)
    const host = targetHost.trim()
    if (!host) {
      setErrorMsg('请填写目标 IP 或域名字段。')
      return
    }

    setLoading(true)
    setErrorMsg('')
    try {
      const csrfToken = await fetchCsrfToken()
      const finalName = taskName.trim() || `${isp} ${region} 回程 (${expectedRoute})`
      const safeId = `mtr-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 6)}`

      const payload = {
        id: safeId,
        name: finalName,
        kind: 'mtr',
        host,
        max_hops: 20,
        interval_seconds: Number(intervalSeconds) || 180,
        timeout_ms: 3000,
        enabled,
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
        let errText = '创建回程监测任务失败'
        try {
          const json = await res.json()
          if (json?.error) errText = json.error
        } catch {}
        throw new Error(errText)
      }

      const created = await res.json()
      if (onCreated) onCreated(created)
      onClose()
    } catch (err) {
      setErrorMsg(err.message || '网络连接异常，无法创建监测任务')
    } finally {
      setLoading(false)
    }
  }

  if (!isOpen) return null

  return (
    <div className="modal-overlay route-modal-overlay" onClick={onClose}>
      <div
        className="route-modal-card"
        onClick={(e) => {
          e.stopPropagation()
          setNodeDropdownOpen(false)
        }}
      >
        {/* 顶部标题栏与关闭按钮 */}
        <div className="route-modal-header">
          <div className="route-header-titles">
            <h2 className="route-modal-title">新建回程监测</h2>
            <p className="route-modal-desc">
              从所选服务器探测到国内目标的逐跳路径，并在线路变化稳定后通知。
            </p>
          </div>
          <button
            type="button"
            className="route-modal-close"
            onClick={onClose}
            aria-label="关闭"
          >
            <X size={18} />
          </button>
        </div>

        <form onSubmit={handleSubmit} className="route-modal-form">
          {/* 1. 基本信息 */}
          <div className="route-form-section">
            <h3 className="route-section-heading">基本信息</h3>
            <div className="route-grid-2col">
              {/* 任务名称 */}
              <div className="route-field-group">
                <label className="route-label">任务名称</label>
                <input
                  type="text"
                  className="route-input"
                  placeholder="例如：东京到上海移动"
                  value={taskName}
                  onChange={(e) => setTaskName(e.target.value)}
                />
              </div>

              {/* 探测节点 (支持多选) */}
              <div
                className="route-field-group"
                onClick={(e) => e.stopPropagation()}
              >
                <label className="route-label">探测节点</label>
                <div className="route-dropdown-wrap">
                  <button
                    type="button"
                    className="route-input route-select-trigger"
                    onClick={() => setNodeDropdownOpen((v) => !v)}
                  >
                    <span className="route-selected-text">
                      {selectedNodes.length === 0
                        ? '选择服务器（支持多选）'
                        : `已选择 ${selectedNodes.length} 台服务器`}
                    </span>
                    <CaretDown size={14} className="route-caret" />
                  </button>

                  {nodeDropdownOpen && (
                    <div className="route-node-menu">
                      {nodes.map((node) => {
                        const id = safeText(node.uuid) || safeText(node.id)
                        const isChecked = selectedNodes.includes(id)
                        return (
                          <div
                            key={id}
                            className={`route-node-option ${isChecked ? 'is-checked' : ''}`}
                            onClick={() => toggleNodeSelection(id)}
                          >
                            <span className="route-checkbox">
                              {isChecked && <Check size={12} weight="bold" />}
                            </span>
                            <span className="route-option-flag">{node.flag || '🌐'}</span>
                            <span className="route-option-name">{node.name}</span>
                            <span className="route-option-sub muted">
                              {node.region || '公网'}
                            </span>
                          </div>
                        )
                      })}
                      {nodes.length === 0 && (
                        <div className="route-node-empty muted">暂无可选在线节点</div>
                      )}
                    </div>
                  )}
                </div>
              </div>

              {/* 运营商 */}
              <div className="route-field-group">
                <label className="route-label">运营商</label>
                <div className="route-select-wrapper">
                  <select
                    className="route-input route-select"
                    value={isp}
                    onChange={(e) => handleIspChange(e.target.value)}
                  >
                    {ISP_OPTIONS.map((opt) => (
                      <option key={opt.value} value={opt.value}>
                        {opt.label}
                      </option>
                    ))}
                  </select>
                  <CaretDown size={14} className="route-select-arrow" />
                </div>
              </div>

              {/* 地区（仅用于标记） */}
              <div className="route-field-group">
                <label className="route-label">地区（仅用于标记）</label>
                <div className="route-select-wrapper">
                  <select
                    className="route-input route-select"
                    value={region}
                    onChange={(e) => setRegion(e.target.value)}
                  >
                    {REGION_OPTIONS.map((opt) => (
                      <option key={opt.value} value={opt.value}>
                        {opt.label}
                      </option>
                    ))}
                  </select>
                  <CaretDown size={14} className="route-select-arrow" />
                </div>
              </div>

              {/* 目标 IP 或域名 */}
              <div className="route-field-group">
                <label className="route-label">
                  目标 IP 或域名 <span className="text-rose">*</span>
                </label>
                <div className="route-input-wrap">
                  <input
                    type="text"
                    required
                    className={`route-input mono ${hostTouched && !targetHost.trim() ? 'is-invalid' : ''}`}
                    placeholder="运营商测试目标"
                    value={targetHost}
                    onChange={(e) => {
                      setTargetHost(e.target.value)
                      if (errorMsg) setErrorMsg('')
                    }}
                    onBlur={() => setHostTouched(true)}
                  />
                  {hostTouched && !targetHost.trim() && (
                    <div className="route-tooltip-bubble" role="tooltip">
                      请填写此字段。
                    </div>
                  )}
                </div>
              </div>

              {/* 地址类型 */}
              <div className="route-field-group">
                <label className="route-label">地址类型</label>
                <div className="route-select-wrapper">
                  <select
                    className="route-input route-select"
                    value={ipType}
                    onChange={(e) => setIpType(e.target.value)}
                  >
                    <option value="IPv4">IPv4</option>
                    <option value="IPv6">IPv6</option>
                  </select>
                  <CaretDown size={14} className="route-select-arrow" />
                </div>
              </div>
            </div>
          </div>

          {/* 2. 判定规则 */}
          <div className="route-form-section">
            <h3 className="route-section-heading">判定规则</h3>
            <div className="route-grid-2col">
              {/* 预期线路 */}
              <div className="route-field-group">
                <label className="route-label">预期线路</label>
                <div className="route-select-wrapper">
                  <select
                    className="route-input route-select"
                    value={expectedRoute}
                    onChange={(e) => setExpectedRoute(e.target.value)}
                  >
                    {ROUTE_OPTIONS.map((r) => (
                      <option key={r} value={r}>
                        {r}
                      </option>
                    ))}
                  </select>
                  <CaretDown size={14} className="route-select-arrow" />
                </div>
              </div>

              {/* 探测协议 */}
              <div className="route-field-group">
                <label className="route-label">探测协议</label>
                <div className="route-select-wrapper">
                  <select
                    className="route-input route-select"
                    value={protocol}
                    onChange={(e) => setProtocol(e.target.value)}
                  >
                    {PROTOCOL_OPTIONS.map((p) => (
                      <option key={p.value} value={p.value}>
                        {p.label}
                      </option>
                    ))}
                  </select>
                  <CaretDown size={14} className="route-select-arrow" />
                </div>
              </div>

              {/* 探测间隔 (秒) */}
              <div className="route-field-group">
                <label className="route-label">探测间隔（秒）</label>
                <input
                  type="number"
                  min="10"
                  max="86400"
                  className="route-input mono"
                  value={intervalSeconds}
                  onChange={(e) => setIntervalSeconds(e.target.value)}
                />
              </div>

              {/* 切线确认次数 */}
              <div className="route-field-group">
                <label className="route-label">切线确认次数</label>
                <input
                  type="number"
                  min="1"
                  max="20"
                  className="route-input mono"
                  value={switchThreshold}
                  onChange={(e) => setSwitchThreshold(e.target.value)}
                />
              </div>

              {/* 恢复确认次数 */}
              <div className="route-field-group">
                <label className="route-label">恢复确认次数</label>
                <input
                  type="number"
                  min="1"
                  max="20"
                  className="route-input mono"
                  value={recoverThreshold}
                  onChange={(e) => setRecoverThreshold(e.target.value)}
                />
              </div>
            </div>

            {/* 参与疑似被墙判定（实验室功能） */}
            <div className="route-lab-banner">
              <div className="lab-info-block">
                <div className="lab-title-line">
                  <strong>参与疑似被墙判定（实验室功能）</strong>
                </div>
                <p className="lab-desc">
                  需要同一节点、同一地址类型下至少两个不同运营商任务同时开启，并各自关联一条延迟监测任务。
                </p>
              </div>
              <button
                type="button"
                className={`route-switch ${wallCheck ? 'is-active' : ''}`}
                onClick={() => setWallCheck((v) => !v)}
                aria-pressed={wallCheck}
                aria-label="参与疑似被墙判定开关"
              >
                <span className="route-switch-thumb" />
              </button>
            </div>
          </div>

          {/* 3. 通知与状态 */}
          <div className="route-form-section">
            <h3 className="route-section-heading">通知与状态</h3>
            <div className="route-grid-2col">
              {/* 左侧：切线通知冷却时间 */}
              <div className="route-field-group">
                <label className="route-label">切线通知冷却时间（秒）</label>
                <input
                  type="number"
                  min="60"
                  max="86400"
                  className="route-input mono"
                  value={cooldown}
                  onChange={(e) => setCooldown(e.target.value)}
                />
              </div>

              {/* 右侧：开关组 */}
              <div className="route-toggles-col">
                <div className="route-toggle-item">
                  <span className="toggle-label">发送切线通知</span>
                  <button
                    type="button"
                    className={`route-switch ${notifySwitch ? 'is-active' : ''}`}
                    onClick={() => setNotifySwitch((v) => !v)}
                    aria-pressed={notifySwitch}
                    aria-label="发送切线通知"
                  >
                    <span className="route-switch-thumb" />
                  </button>
                </div>

                <div className="route-toggle-item">
                  <span className="toggle-label">发送恢复通知</span>
                  <button
                    type="button"
                    className={`route-switch ${notifyRecover ? 'is-active' : ''}`}
                    onClick={() => setNotifyRecover((v) => !v)}
                    aria-pressed={notifyRecover}
                    aria-label="发送恢复通知"
                  >
                    <span className="route-switch-thumb" />
                  </button>
                </div>

                <div className="route-toggle-item">
                  <span className="toggle-label">启用任务</span>
                  <button
                    type="button"
                    className={`route-switch ${enabled ? 'is-active' : ''}`}
                    onClick={() => setEnabled((v) => !v)}
                    aria-pressed={enabled}
                    aria-label="启用任务"
                  >
                    <span className="route-switch-thumb" />
                  </button>
                </div>
              </div>
            </div>
          </div>

          {errorMsg && <div className="route-error-banner">{errorMsg}</div>}

          {/* 底部按钮栏 */}
          <div className="route-modal-actions">
            <button
              type="button"
              className="button button-quiet"
              onClick={onClose}
              disabled={loading}
            >
              取消
            </button>
            <button
              type="submit"
              className="button button-primary"
              disabled={loading}
            >
              {loading ? (
                <>
                  <CircleNotch size={15} className="spin" />
                  <span>正在创建…</span>
                </>
              ) : (
                <span>创建监测任务</span>
              )}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
