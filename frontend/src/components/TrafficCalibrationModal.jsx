import { useEffect, useState } from 'react'
import { X, Info } from '@phosphor-icons/react'
import { numeric, safeText } from '../lib/format.js'
import { getStoredBillingData, saveNodeBillingData, getNodeCustomMeta } from '../lib/billing.js'

// Format bytes with 2 decimal places when >= MB/GB
export const formatTrafficPrecise = (bytes) => {
  const n = Number(bytes) || 0
  const isNegative = n < 0
  const abs = Math.abs(n)
  if (abs === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  let size = abs
  let i = 0
  while (size >= 1024 && i < units.length - 1) {
    size /= 1024
    i += 1
  }
  const formatted = i === 0 ? Math.round(size) : size.toFixed(2)
  return `${isNegative ? '-' : ''}${formatted} ${units[i]}`
}

// Format difference: e.g. "+1.20 GB", "-500.00 MB", "0 B"
export const formatDiff = (bytes) => {
  const n = Number(bytes) || 0
  if (Math.abs(n) < 1) return '0 B'
  const formatted = formatTrafficPrecise(Math.abs(n))
  return n > 0 ? `+${formatted}` : `-${formatted}`
}

// Parse string like "37.46 GB", "37.46", "500MB", "1.2 TB" into bytes
export const parseTrafficString = (val, defaultUnit = 'GB') => {
  if (val === null || val === undefined) return 0
  const str = String(val).trim()
  if (!str) return 0
  const match = str.match(/^([+-]?\d+(?:\.\d+)?)\s*([a-zA-Z]*)$/)
  if (!match) {
    const num = parseFloat(str)
    return isNaN(num) ? 0 : Math.round(num * 1024 * 1024 * 1024)
  }
  const num = parseFloat(match[1])
  if (isNaN(num)) return 0
  const unit = (match[2] || defaultUnit).toUpperCase()
  const multipliers = {
    B: 1,
    KB: 1024,
    K: 1024,
    MB: 1024 * 1024,
    M: 1024 * 1024,
    GB: 1024 * 1024 * 1024,
    G: 1024 * 1024 * 1024,
    TB: 1024 * 1024 * 1024 * 1024,
    T: 1024 * 1024 * 1024 * 1024,
    PB: 1024 * 1024 * 1024 * 1024 * 1024,
    P: 1024 * 1024 * 1024 * 1024 * 1024,
  }
  const factor = multipliers[unit] || (1024 * 1024 * 1024)
  return Math.round(num * factor)
}

// Compute billing period string e.g. "2026年9月22日 00:00:00 - 2026年10月22日 00:00:00"
export const computeBillingPeriod = (resetDay = 22) => {
  const now = new Date()
  const day = now.getDate()
  let startYear = now.getFullYear()
  let startMonth = now.getMonth() // 0-indexed
  let endYear = startYear
  let endMonth = startMonth + 1

  if (day < resetDay) {
    startMonth -= 1
    if (startMonth < 0) {
      startMonth = 11
      startYear -= 1
    }
    endMonth = startMonth + 1
    if (endMonth > 11) {
      endMonth = 0
      endYear = startYear + 1
    } else {
      endYear = startYear
    }
  } else {
    if (endMonth > 11) {
      endMonth = 0
      endYear += 1
    }
  }

  const startDateStr = `${startYear}年${startMonth + 1}月${resetDay}日 00:00:00`
  const endDateStr = `${endYear}年${endMonth + 1}月${resetDay}日 00:00:00`
  return `${startDateStr} - ${endDateStr}`
}

export function TrafficCalibrationModal({ node, onClose, onSaveSuccess }) {
  const nodeId = safeText(node?.uuid || node?.id)
  const billingStore = getStoredBillingData()
  const nodeBilling = billingStore[nodeId] || {}
  const nodeMeta = getNodeCustomMeta(nodeId, node)

  // ESC key to close
  useEffect(() => {
    const handleKeyDown = (e) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [onClose])

  const rawTx = numeric(node?.tx) || 0
  const rawRx = numeric(node?.rx) || 0

  const existingTxOffset = Number(nodeBilling.txOffsetBytes ?? nodeBilling.trafficOffsetBytes ?? 0)
  const existingRxOffset = Number(nodeBilling.rxOffsetBytes ?? 0)

  // Initial calibrated values
  const initTxBytes = Math.max(0, rawTx + existingTxOffset)
  const initRxBytes = Math.max(0, rawRx + existingRxOffset)

  const [inputTxStr, setInputTxStr] = useState(() => formatTrafficPrecise(initTxBytes))
  const [inputRxStr, setInputRxStr] = useState(() => formatTrafficPrecise(initRxBytes))
  const [saveSuccess, setSaveSuccess] = useState(false)

  // Computed values
  const parsedTxBytes = parseTrafficString(inputTxStr, 'GB')
  const parsedRxBytes = parseTrafficString(inputRxStr, 'GB')

  const diffTxBytes = parsedTxBytes - rawTx
  const diffRxBytes = parsedRxBytes - rawRx

  const timezoneStr = nodeMeta?.timezone || 'Asia/Shanghai (UTC+8)'
  const cyclePeriodStr = computeBillingPeriod(nodeMeta?.resetDay ?? 22)

  const records = nodeBilling.calibrationRecords || []

  if (!node) return null

  const handleSave = (e) => {
    e.preventDefault()

    const newRecord = {
      id: Date.now(),
      time: new Date().toLocaleString('zh-CN', { hour12: false }),
      actualTxStr: formatTrafficPrecise(parsedTxBytes),
      actualRxStr: formatTrafficPrecise(parsedRxBytes),
      diffTxStr: formatDiff(diffTxBytes),
      diffRxStr: formatDiff(diffRxBytes),
      txOffset: diffTxBytes,
      rxOffset: diffRxBytes,
      createdAt: new Date().toISOString(),
    }

    saveNodeBillingData(nodeId, {
      txOffsetBytes: diffTxBytes,
      rxOffsetBytes: diffRxBytes,
      trafficOffsetBytes: diffTxBytes + diffRxBytes,
      calibratedTxBytes: parsedTxBytes,
      calibratedRxBytes: parsedRxBytes,
      lastCalibratedAt: new Date().toISOString(),
      calibrationRecords: [newRecord, ...records].slice(0, 10),
    })

    setSaveSuccess(true)
    if (onSaveSuccess) onSaveSuccess(diffTxBytes + diffRxBytes)
    setTimeout(() => {
      onClose()
    }, 250)
  }

  return (
    <div className="traffic-cal-modal-overlay" onClick={onClose} role="dialog" aria-modal="true">
      <div className="traffic-cal-modal-card" onClick={(e) => e.stopPropagation()}>
        {/* Header */}
        <div className="traffic-cal-header">
          <div>
            <h2 className="traffic-cal-title">流量校准</h2>
            <p className="traffic-cal-subtitle">
              将 <strong>{node.name}</strong> 当前计费周期的上传和下载用量校准为实际值。
            </p>
          </div>
          <button type="button" className="traffic-cal-close-btn" onClick={onClose} aria-label="关闭">
            <X size={18} />
          </button>
        </div>

        {/* 当前计费周期卡片 */}
        <div className="traffic-cal-cycle-card">
          <div className="traffic-cal-cycle-header">
            <span className="traffic-cal-cycle-badge">当前计费周期</span>
            <span className="traffic-cal-cycle-tz">{timezoneStr}</span>
          </div>
          <div className="traffic-cal-cycle-range">{cyclePeriodStr}</div>
        </div>

        {/* 原始统计 / 校准差额 / 校准后用量 */}
        <div className="traffic-cal-compare-grid">
          <div className="traffic-cal-compare-col">
            <div className="traffic-cal-compare-title">原始统计</div>
            <div className="traffic-cal-compare-item">上传: {formatTrafficPrecise(rawTx)}</div>
            <div className="traffic-cal-compare-item">下载: {formatTrafficPrecise(rawRx)}</div>
          </div>
          <div className="traffic-cal-compare-col">
            <div className="traffic-cal-compare-title">校准差额</div>
            <div className="traffic-cal-compare-item">上传: {formatDiff(diffTxBytes)}</div>
            <div className="traffic-cal-compare-item">下载: {formatDiff(diffRxBytes)}</div>
          </div>
          <div className="traffic-cal-compare-col">
            <div className="traffic-cal-compare-title">校准后用量</div>
            <div className="traffic-cal-compare-item">上传: {formatTrafficPrecise(parsedTxBytes)}</div>
            <div className="traffic-cal-compare-item">下载: {formatTrafficPrecise(parsedRxBytes)}</div>
          </div>
        </div>

        <form onSubmit={handleSave}>
          {/* 实际上传用量 & 实际下载用量 */}
          <div className="traffic-cal-inputs-grid">
            <div>
              <label className="traffic-cal-input-label" htmlFor="cal-upload-input">
                实际上传用量
              </label>
              <input
                id="cal-upload-input"
                type="text"
                className="traffic-cal-input"
                value={inputTxStr}
                onChange={(e) => setInputTxStr(e.target.value)}
                placeholder="例如: 37.46 GB"
                required
              />
            </div>
            <div>
              <label className="traffic-cal-input-label" htmlFor="cal-download-input">
                实际下载用量
              </label>
              <input
                id="cal-download-input"
                type="text"
                className="traffic-cal-input"
                value={inputRxStr}
                onChange={(e) => setInputRxStr(e.target.value)}
                placeholder="例如: 39.05 GB"
                required
              />
            </div>
          </div>

          {/* 提示信息 */}
          <div className="traffic-cal-tip-box">
            <Info size={18} className="traffic-cal-tip-icon" />
            <div className="traffic-cal-tip-text">
              保存后，仪表盘、流量告警、日报/周报/月报、公共接口和主题将统一使用校准后的数据。后续新增流量会继续累加，到下一个重置日时本周期校准值自动清 0。
            </div>
          </div>

          {/* 最近校准记录 */}
          <div className="traffic-cal-records-section">
            <div className="traffic-cal-records-title">最近校准记录</div>
            {records.length === 0 ? (
              <div className="traffic-cal-records-empty">本周期尚未校准</div>
            ) : (
              <div className="traffic-cal-records-list">
                {records.slice(0, 3).map((rec) => (
                  <div key={rec.id} className="traffic-cal-record-item">
                    <span className="traffic-cal-record-time">{rec.time}:</span>
                    <span>
                      上传校准为 {rec.actualTxStr} ({rec.diffTxStr})，下载校准为 {rec.actualRxStr} ({rec.diffRxStr})
                    </span>
                  </div>
                ))}
              </div>
            )}
          </div>

          {/* 底部按钮 */}
          <div className="traffic-cal-footer">
            <button type="button" className="traffic-cal-btn-cancel" onClick={onClose}>
              取消
            </button>
            <button type="submit" className="traffic-cal-btn-save">
              {saveSuccess ? '已保存' : '保存校准'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
