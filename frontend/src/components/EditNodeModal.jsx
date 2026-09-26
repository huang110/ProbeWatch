import { useEffect, useState } from 'react'
import { CaretDown, Check, Info, X } from '@phosphor-icons/react'
import { getNodeCustomMeta, saveNodeCustomMeta } from '../lib/billing.js'
import { updateNode } from '../lib/api.js'
import { safeText } from '../lib/format.js'

const TIMEZONES = [
  'Asia/Shanghai (UTC+8)',
  'Asia/Tokyo (UTC+9)',
  'Asia/Hong_Kong (UTC+8)',
  'Asia/Singapore (UTC+8)',
  'America/Los_Angeles (UTC-8)',
  'America/New_York (UTC-5)',
  'Europe/London (UTC+0)',
  'Europe/Frankfurt (UTC+1)',
  'UTC (UTC+0)',
]

const TRAFFIC_MODES = [
  { value: 'sum', label: '总和' },
  { value: 'max', label: '最大值(出/入取大)' },
  { value: 'in', label: '只计入站' },
  { value: 'out', label: '只计出站' },
]

export function EditNodeModal({ node, onClose, onSaved }) {
  const nodeId = safeText(node?.uuid || node?.id)
  const initial = getNodeCustomMeta(nodeId, node)

  const [name, setName] = useState(initial.customName || node?.name || '')
  const [flag, setFlag] = useState(initial.customFlag || '自动识别')
  const [tags, setTags] = useState(initial.tags || '电信CN2GIA<Red>;联通9929<blue>;移动CMIN2<Green>;')
  const [bandwidth, setBandwidth] = useState(initial.bandwidth || '500 Mbps')
  const [group, setGroup] = useState(initial.group || '')
  const [privateNote, setPrivateNote] = useState(initial.privateNote || '')
  const [publicNote, setPublicNote] = useState(initial.publicNote || '')
  const [hidden, setHidden] = useState(Boolean(initial.hidden))

  const [timezone, setTimezone] = useState(initial.timezone || 'Asia/Shanghai (UTC+8)')
  const [resetDay, setResetDay] = useState(initial.resetDay !== undefined ? String(initial.resetDay) : '22')
  const [resetTime, setResetTime] = useState(initial.resetTime || '00:00:00')
  const [trafficCalculation, setTrafficCalculation] = useState(initial.trafficCalculation || 'sum')
  const [trafficQuota, setTrafficQuota] = useState(initial.trafficQuota || '500.00 GB')
  const [resetAllowance, setResetAllowance] = useState(initial.resetAllowance || '0 B')

  const [saveSuccess, setSaveSuccess] = useState(false)

  // ESC 键关闭
  useEffect(() => {
    const handleKeyDown = (e) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [onClose])

  const handleSave = (e) => {
    e.preventDefault()
    const payload = {
      customName: name.trim() || node?.name || '',
      customFlag: flag.trim() || '自动识别',
      tags: tags.trim(),
      bandwidth: bandwidth.trim(),
      group: group.trim(),
      privateNote: privateNote.trim(),
      publicNote: publicNote.trim(),
      hidden,
      timezone,
      resetDay: Number(resetDay) || 0,
      resetTime: resetTime.trim() || '00:00:00',
      trafficCalculation,
      trafficQuota: trafficQuota.trim() || '500.00 GB',
      resetAllowance: resetAllowance.trim() || '0 B',
    }

    saveNodeCustomMeta(nodeId, payload)
    updateNode(nodeId, {
      name: payload.customName,
      tags: payload.tags,
    }).catch((err) => {
      console.warn('Sync node metadata to backend skipped or failed:', err)
    })
    setSaveSuccess(true)
    setTimeout(() => {
      if (onSaved) onSaved(payload)
      onClose()
    }, 280)
  }

  return (
    <div className="modal-overlay edit-node-modal-overlay" onClick={onClose}>
      <div className="edit-node-modal-card" onClick={(e) => e.stopPropagation()}>
        {/* Header */}
        <div className="edit-node-header">
          <div className="edit-node-header-text">
            <h2 className="edit-node-title">编辑信息</h2>
            <p className="edit-node-desc">调整服务器标识、展示信息与流量策略。</p>
          </div>
          <button type="button" className="edit-node-close-btn" onClick={onClose} aria-label="关闭">
            <X size={18} />
          </button>
        </div>

        {/* 2-Column Form Body */}
        <form onSubmit={handleSave} className="edit-node-form">
          <div className="edit-node-grid">
            {/* Left Column: Identifiers, Tags, Notes */}
            <div className="edit-node-col">
              {/* 名称 */}
              <div className="edit-field-group">
                <label className="edit-label">名称</label>
                <input
                  type="text"
                  className="edit-input"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="例如: DMIT PRO.WEE"
                />
              </div>

              {/* 国家/地区图标 */}
              <div className="edit-field-group">
                <label className="edit-label">国家/地区图标</label>
                <input
                  type="text"
                  className="edit-input"
                  value={flag}
                  onChange={(e) => setFlag(e.target.value)}
                  placeholder="自动识别"
                />
                <span className="edit-hint-text">
                  用于广播 IP 或 GeoIP 识别不准的情况；清空后恢复自动识别。
                </span>
              </div>

              {/* 标签 多个标签用';'分隔 */}
              <div className="edit-field-group">
                <div className="edit-label-with-info">
                  <label className="edit-label">
                    标签 <small className="edit-label-sub">多个标签用';'分隔</small>
                  </label>
                  <span className="edit-info-icon" title="支持颜色标签语法，如: 线路名<Red>; 特性<blue>; 优惠<Green>;">
                    <Info size={13} />
                  </span>
                </div>
                <input
                  type="text"
                  className="edit-input mono"
                  value={tags}
                  onChange={(e) => setTags(e.target.value)}
                  placeholder="电信CN2GIA<Red>;联通9929<blue>;移动CMIN2<Green>;"
                />
              </div>

              {/* 带宽 */}
              <div className="edit-field-group">
                <label className="edit-label">带宽</label>
                <input
                  type="text"
                  className="edit-input"
                  value={bandwidth}
                  onChange={(e) => setBandwidth(e.target.value)}
                  placeholder="500 Mbps"
                />
                <span className="edit-hint-text">
                  用于概览展示，可填写 100 Mbps、1 Gbps、10 G 这类带宽。
                </span>
              </div>

              {/* 分组 */}
              <div className="edit-field-group">
                <label className="edit-label">分组</label>
                <input
                  type="text"
                  className="edit-input"
                  value={group}
                  onChange={(e) => setGroup(e.target.value)}
                  placeholder="例如: 亚太直连 / 欧洲BGP"
                />
              </div>

              {/* 私有备注 */}
              <div className="edit-field-group">
                <label className="edit-label">私有备注</label>
                <input
                  type="text"
                  className="edit-input"
                  value={privateNote}
                  onChange={(e) => setPrivateNote(e.target.value)}
                  placeholder="请输入私有备注"
                />
              </div>

              {/* 公开备注 */}
              <div className="edit-field-group">
                <label className="edit-label">公开备注</label>
                <input
                  type="text"
                  className="edit-input"
                  value={publicNote}
                  onChange={(e) => setPublicNote(e.target.value)}
                  placeholder="请输入公开备注"
                />
              </div>

              {/* 隐藏节点 */}
              <div className="edit-field-group edit-hide-row">
                <div className="edit-hide-text">
                  <strong className="edit-label">隐藏节点</strong>
                  <span className="edit-hint-text">在未登陆的情况下隐藏该节点</span>
                </div>
                <button
                  type="button"
                  className={`edit-switch ${hidden ? 'is-active' : ''}`}
                  onClick={() => setHidden((v) => !v)}
                  aria-pressed={hidden}
                  aria-label="隐藏节点开关"
                >
                  <span className="edit-switch-thumb" />
                </button>
              </div>
            </div>

            {/* Right Column: Traffic Reset & Quota Policies */}
            <div className="edit-node-col">
              {/* 流量重置时间 */}
              <div className="edit-field-group">
                <label className="edit-label">流量重置时间</label>
                <div className="edit-triple-row">
                  {/* 时区选择 */}
                  <div className="edit-select-wrap flex-2">
                    <select
                      className="edit-input edit-select"
                      value={timezone}
                      onChange={(e) => setTimezone(e.target.value)}
                    >
                      {TIMEZONES.map((tz) => (
                        <option key={tz} value={tz}>
                          {tz}
                        </option>
                      ))}
                    </select>
                    <CaretDown size={14} className="edit-select-arrow" />
                  </div>

                  {/* 每月重置日 (1-31) */}
                  <div className="flex-1">
                    <input
                      type="number"
                      min="0"
                      max="31"
                      className="edit-input mono text-center"
                      value={resetDay}
                      onChange={(e) => setResetDay(e.target.value)}
                      placeholder="22"
                    />
                  </div>

                  {/* 重置时间点 */}
                  <div className="flex-1">
                    <input
                      type="text"
                      className="edit-input mono text-center"
                      value={resetTime}
                      onChange={(e) => setResetTime(e.target.value)}
                      placeholder="00:00:00"
                    />
                  </div>
                </div>
                <span className="edit-hint-text">
                  0 表示关闭；1-31 为每月重置日。流量重置时间按厂商账单填写，Lite 会换算到北京时间才重置。存量数据为北京时间 0:00。保存后同步到 Agent。
                </span>
              </div>

              {/* 统计方式 */}
              <div className="edit-field-group">
                <label className="edit-label">统计方式</label>
                <div className="edit-select-wrap">
                  <select
                    className="edit-input edit-select"
                    value={trafficCalculation}
                    onChange={(e) => setTrafficCalculation(e.target.value)}
                  >
                    {TRAFFIC_MODES.map((mode) => (
                      <option key={mode.value} value={mode.value}>
                        {mode.label}
                      </option>
                    ))}
                  </select>
                  <CaretDown size={14} className="edit-select-arrow" />
                </div>
              </div>

              {/* 流量阈值 */}
              <div className="edit-field-group">
                <label className="edit-label">流量阈值</label>
                <span className="edit-hint-text">用于首页流量进度条，设置为 0 B 禁用。</span>
                <input
                  type="text"
                  className="edit-input mono"
                  value={trafficQuota}
                  onChange={(e) => setTrafficQuota(e.target.value)}
                  placeholder="500.00 GB"
                />
              </div>

              <div className="edit-col-divider" />

              {/* 重置流量额度 */}
              <div className="edit-field-group">
                <label className="edit-label">重置流量额度</label>
                <span className="edit-hint-text">
                  同一计费周期可多次调整；与原流量限额相加，按上方流量统计方式计算，并在下个重置日自动归零。
                </span>
                <input
                  type="text"
                  className="edit-input mono"
                  value={resetAllowance}
                  onChange={(e) => setResetAllowance(e.target.value)}
                  placeholder="0 B"
                />

                <div className="edit-traffic-calc-box">
                  <div className="calc-formula">
                    原限额 {trafficQuota || '500.00 GB'} + 重置流量 {resetAllowance || '0 B'} = 本周期总限额{' '}
                    <b>{trafficQuota || '500.00 GB'}</b>
                  </div>
                  <div className="calc-explain">
                    这里只调整本周期额度，不会清零或修改真实流量，日、周、月报仍按实际产生的流量统计。
                  </div>
                </div>
              </div>
            </div>
          </div>

          {/* Footer Actions */}
          <div className="edit-node-actions">
            <button type="button" className="button button-quiet" onClick={onClose}>
              取消
            </button>
            <button type="submit" className="button button-primary">
              {saveSuccess ? (
                <>
                  <Check size={16} weight="bold" />
                  <span>已保存</span>
                </>
              ) : (
                <span>保存</span>
              )}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
