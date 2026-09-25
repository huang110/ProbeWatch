import { useState } from 'react'
import { X, Sliders, CheckCircle, WarningCircle, ArrowCounterClockwise, Info } from '@phosphor-icons/react'
import { formatBytes, numeric } from '../lib/format.js'
import { getStoredBillingData, saveNodeBillingData } from '../lib/billing.js'

export function TrafficCalibrationModal({ node, onClose, onSaveSuccess }) {
  if (!node) return null
  const nodeId = node.uuid || node.id
  const billingStore = getStoredBillingData()
  const nodeBilling = billingStore[nodeId] || {}

  const rawRx = numeric(node.rx) || 0
  const rawTx = numeric(node.tx) || 0
  const rawTotal = rawRx + rawTx

  const existingOffset = Number(nodeBilling.trafficOffsetBytes || 0)
  const currentEffective = Math.max(0, rawTotal + existingOffset)

  const [inputVal, setInputVal] = useState(() => {
    if (currentEffective <= 0) return '0'
    const inGB = currentEffective / (1024 * 1024 * 1024)
    return inGB >= 10 ? Math.round(inGB).toString() : inGB.toFixed(2)
  })
  const [unit, setUnit] = useState('GB')
  const [reason, setReason] = useState(nodeBilling.calibrationReason || '')
  const [errorMsg, setErrorMsg] = useState('')
  const [savedSuccess, setSavedSuccess] = useState(false)

  // 计算输入的字节数
  const multiplier = unit === 'TB' ? 1024 * 1024 * 1024 * 1024 : 1024 * 1024 * 1024
  const targetBytes = (Number(inputVal) || 0) * multiplier
  const newOffset = targetBytes - rawTotal

  const handleApply = (e) => {
    e.preventDefault()
    setErrorMsg('')
    if (targetBytes < 0) {
      setErrorMsg('生效用量不能为负数')
      return
    }
    if (newOffset < -rawTotal) {
      setErrorMsg('向下校准幅度过大，超出当前周期历史用量，无法冲减')
      return
    }

    saveNodeBillingData(nodeId, {
      trafficOffsetBytes: newOffset,
      calibrationReason: reason.trim() || '手动服务商后台对账校准',
      lastCalibratedAt: new Date().toISOString(),
    })

    setSavedSuccess(true)
    if (onSaveSuccess) onSaveSuccess(newOffset)
    setTimeout(() => {
      onClose()
    }, 600)
  }

  const handleResetOffset = () => {
    saveNodeBillingData(nodeId, {
      trafficOffsetBytes: 0,
      calibrationReason: '',
      lastCalibratedAt: null,
    })
    setSavedSuccess(true)
    if (onSaveSuccess) onSaveSuccess(0)
    setTimeout(() => {
      onClose()
    }, 500)
  }

  return (
    <div className="modal-backdrop" onClick={onClose} role="dialog" aria-modal="true">
      <div className="modal-card modal-lg" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header">
          <div className="modal-title-wrap">
            <span className="modal-icon-badge"><Sliders size={18} /></span>
            <div>
              <h3>流量校准 (Traffic Calibration)</h3>
              <p className="modal-subtitle">{node.name} · 对齐 VPS 服务商后台实际流量计费</p>
            </div>
          </div>
          <button type="button" className="icon-button" onClick={onClose} aria-label="关闭">
            <X size={18} />
          </button>
        </div>

        <div className="modal-body space-y-4">
          <div className="calibration-notice">
            <Info size={18} className="text-blue" />
            <div>
              <strong>校准不会改写原始监测计数</strong>
              <p>系统记录一个相对偏差量 (Offset)，本周期大屏、报告和告警均按校准后的生效用量展示。进入下一个流量重置日后，校准自动归零。</p>
            </div>
          </div>

          <div className="calibration-stats-grid">
            <div className="calibration-stat-card">
              <span className="stat-label">Agent 原始采集</span>
              <strong className="stat-val mono">{formatBytes(rawTotal)}</strong>
              <span className="stat-desc">上行 {formatBytes(rawTx)} · 下行 {formatBytes(rawRx)}</span>
            </div>
            <div className="calibration-stat-card">
              <span className="stat-label">当前校准调整量 (Offset)</span>
              <strong className={`stat-val mono ${existingOffset > 0 ? 'text-amber' : existingOffset < 0 ? 'text-blue' : ''}`}>
                {existingOffset > 0 ? `+${formatBytes(existingOffset)}` : existingOffset < 0 ? `-${formatBytes(Math.abs(existingOffset))}` : '0 B'}
              </strong>
              <span className="stat-desc">{nodeBilling.lastCalibratedAt ? `上次校准: ${nodeBilling.lastCalibratedAt.slice(0, 10)}` : '未设置校准'}</span>
            </div>
            <div className="calibration-stat-card">
              <span className="stat-label">当前全站生效用量</span>
              <strong className="stat-val text-mint mono">{formatBytes(currentEffective)}</strong>
              <span className="stat-desc">大屏与成本中心当前读取值</span>
            </div>
          </div>

          <form onSubmit={handleApply} className="space-y-4 pt-2">
            <div className="form-group">
              <label htmlFor="cal-target" className="form-label font-medium">
                修正为服务商后台显示的实际数值：
              </label>
              <div className="input-group-row">
                <input
                  id="cal-target"
                  type="number"
                  step="0.01"
                  min="0"
                  className="input flex-1 mono text-lg font-bold"
                  value={inputVal}
                  onChange={(e) => setInputVal(e.target.value)}
                  placeholder="例如: 120"
                  required
                />
                <select
                  className="select w-28"
                  value={unit}
                  onChange={(e) => setUnit(e.target.value)}
                >
                  <option value="GB">GB</option>
                  <option value="TB">TB</option>
                </select>
              </div>
              <p className="text-xs text-muted mt-1">
                预计校准调整量: <b className="mono">{newOffset >= 0 ? `+${formatBytes(newOffset)}` : `-${formatBytes(Math.abs(newOffset))}`}</b>
              </p>
            </div>

            <div className="form-group">
              <label htmlFor="cal-reason" className="form-label">
                校准原因 / 备注 (可选)：
              </label>
              <input
                id="cal-reason"
                type="text"
                className="input w-full"
                value={reason}
                onChange={(e) => setReason(e.target.value)}
                placeholder="例如: 重装系统后补齐已用流量 / 抵扣搬瓦工后台多计费差额"
              />
            </div>

            {errorMsg && (
              <div className="alert-error-banner">
                <WarningCircle size={16} />
                <span>{errorMsg}</span>
              </div>
            )}

            {savedSuccess && (
              <div className="alert-success-banner">
                <CheckCircle size={16} />
                <span>流量校准已保存并即时生效！</span>
              </div>
            )}

            <div className="modal-actions justify-between pt-2">
              <div>
                {existingOffset !== 0 && (
                  <button
                    type="button"
                    className="button button-quiet text-rose btn-sm"
                    onClick={handleResetOffset}
                  >
                    <ArrowCounterClockwise size={14} />
                    <span>清除校准偏差</span>
                  </button>
                )}
              </div>
              <div className="flex gap-2">
                <button type="button" className="button button-quiet" onClick={onClose}>
                  取消
                </button>
                <button type="submit" className="button button-primary">
                  保存并应用校准
                </button>
              </div>
            </div>
          </form>
        </div>
      </div>
    </div>
  )
}
