import { useEffect, useState } from 'react'
import { CalendarBlank, CaretDown, Check, Copy, Info, Sparkle, X } from '@phosphor-icons/react'
import {
  calculateRemainingValue,
  generateForumSalesPost,
  getNodeBilling,
  resolveCurrency,
  saveNodeBillingData,
} from '../lib/billing.js'
import { safeText } from '../lib/format.js'

export function BillingModal({ node, onClose, onSaved }) {
  const nodeId = safeText(node?.uuid || node?.id)
  const initial = getNodeBilling(nodeId, node?.name)

  const [price, setPrice] = useState(initial.price !== undefined ? initial.price : 39.9)
  const [currency, setCurrency] = useState(initial.currency || '$')
  const [cycle, setCycle] = useState(initial.cycle || 'annual')
  const [dueDate, setDueDate] = useState(initial.dueDate || '2026-12-22')
  const [autoRenew, setAutoRenew] = useState(initial.autoRenew !== undefined ? initial.autoRenew : true)

  const [showCalculator, setShowCalculator] = useState(false)
  const [markup, setMarkup] = useState(0)
  const [copied, setCopied] = useState(false)
  const [saveSuccess, setSaveSuccess] = useState(false)

  // ESC 键关闭
  useEffect(() => {
    const handleKeyDown = (e) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [onClose])

  const currentBilling = {
    merchant: initial.merchant || '云服务器',
    price: Number(price) || 0,
    currency: currency.trim() || '$',
    cycle,
    dueDate,
    autoRenew,
  }

  const calc = calculateRemainingValue(currentBilling)
  const totalPriceCNY = Math.max(0, calc.remainingValueCNY + Number(markup))

  const handleSave = (e) => {
    e.preventDefault()
    saveNodeBillingData(nodeId, {
      ...currentBilling,
    })
    setSaveSuccess(true)
    setTimeout(() => {
      if (onSaved) onSaved()
      onClose()
    }, 280)
  }

  const handleCopyPost = () => {
    const text = generateForumSalesPost(node, currentBilling, calc, markup)
    if (navigator?.clipboard?.writeText) {
      navigator.clipboard.writeText(text).then(() => {
        setCopied(true)
        setTimeout(() => setCopied(false), 2000)
      }).catch(() => {
        setCopied(false)
      })
    }
  }

  return (
    <div className="modal-overlay edit-node-modal-overlay" onClick={onClose}>
      <div className="edit-node-modal-card billing-lite-modal-card" onClick={(e) => e.stopPropagation()}>
        {/* Header */}
        <div className="edit-node-header">
          <div className="edit-node-header-text">
            <h2 className="edit-node-title">账单</h2>
            <p className="edit-node-desc">设置价格、计费周期与到期续费策略。</p>
          </div>
          <button type="button" className="edit-node-close-btn" onClick={onClose} aria-label="关闭">
            <X size={18} />
          </button>
        </div>

        {/* 2-Column Form Body */}
        <form onSubmit={handleSave} className="edit-node-form">
          <div className="edit-node-grid">
            {/* Left Column: 价格与货币 */}
            <div className="edit-node-col">
              {/* 价格 */}
              <div className="edit-field-group">
                <label className="edit-label">
                  价格 <span className="edit-label-sub">0不显示，-1表示免费</span>
                </label>
                <input
                  type="number"
                  step="any"
                  className="edit-input mono"
                  value={price}
                  onChange={(e) => setPrice(e.target.value)}
                  placeholder="39.9"
                />
              </div>

              {/* 货币 */}
              <div className="edit-field-group">
                <label className="edit-label">
                  货币 <span className="edit-label-sub">¥-人民币，$-美元，€-欧元，£-英镑，C$-加元，HK$-港币</span>
                </label>
                <input
                  type="text"
                  className="edit-input mono"
                  value={currency}
                  onChange={(e) => setCurrency(e.target.value)}
                  placeholder="$"
                />
              </div>
            </div>

            {/* Right Column: 计费周期、到期时间、自动续费 */}
            <div className="edit-node-col">
              {/* 计费周期 */}
              <div className="edit-field-group">
                <div className="edit-label-with-info">
                  <label className="edit-label">计费周期</label>
                  <span className="edit-info-icon" title="根据计费周期核算小鸡剩余价值与自动续费间隔">
                    <Info size={13} />
                  </span>
                </div>
                <div className="edit-select-wrap">
                  <select
                    className="edit-input edit-select"
                    value={cycle}
                    onChange={(e) => setCycle(e.target.value)}
                  >
                    <option value="annual">年</option>
                    <option value="semiannual">半年</option>
                    <option value="quarter">季</option>
                    <option value="month">月</option>
                    <option value="biennial">两年</option>
                    <option value="triennial">三年</option>
                    <option value="free">一次性/长期免费</option>
                  </select>
                  <CaretDown size={14} className="edit-select-arrow" />
                </div>
              </div>

              {/* 到期时间 */}
              <div className="edit-field-group">
                <label className="edit-label">到期时间</label>
                <div className="billing-date-row">
                  <div className="billing-date-wrap">
                    <input
                      type="date"
                      className="edit-input mono billing-date-input"
                      value={dueDate}
                      onChange={(e) => setDueDate(e.target.value)}
                    />
                    <CalendarBlank size={14} className="billing-cal-icon" />
                  </div>
                  <button
                    type="button"
                    className="button button-quiet btn-sm billing-permanent-btn"
                    onClick={() => {
                      setDueDate('2099-12-31')
                      if (price === 0 || price === '0') setPrice(-1)
                    }}
                  >
                    设置为长期
                  </button>
                </div>
              </div>

              {/* 自动续费 */}
              <div className="edit-field-group edit-hide-row billing-autorenew-row">
                <div className="edit-hide-text">
                  <strong className="edit-label">自动续费</strong>
                  <span className="edit-hint-text">
                    如果服务器过期且当前在线，Lite 将自动将到期时间设置为下个自然月（年）
                  </span>
                </div>
                <button
                  type="button"
                  className={`edit-switch ${autoRenew ? 'is-active' : ''}`}
                  onClick={() => setAutoRenew((v) => !v)}
                  aria-pressed={autoRenew}
                  aria-label="自动续费开关"
                >
                  <span className="edit-switch-thumb" />
                </button>
              </div>
            </div>
          </div>

          {/* 实时折算信息与论坛发帖计算器可折叠工具条 */}
          <div className="billing-mjj-toggle-bar">
            <div className="billing-calc-summary">
              <span>折合剩余价值：</span>
              <b className="mono text-mint">¥{calc.remainingValueCNY.toFixed(2)}</b>
              <span className={`days-pill days-${calc.statusTone} mono`}>{calc.statusText}</span>
            </div>
            <button
              type="button"
              className="billing-calc-toggle-btn"
              onClick={() => setShowCalculator((v) => !v)}
            >
              <Sparkle size={13} className="text-amber" />
              <span>{showCalculator ? '收起发帖文案' : '论坛出鸡计算器'}</span>
            </button>
          </div>

          {showCalculator && (
            <div className="billing-calc-expandable">
              <div className="markup-input-box">
                <div className="inline-flex items-center justify-between w-full">
                  <label className="edit-label">心理溢价 / 骨折优惠金额 (元)</label>
                  <span className="mono text-amber">
                    建议总价: <b>¥{totalPriceCNY.toFixed(2)}</b>
                  </span>
                </div>
                <div className="markup-input-wrap">
                  <input
                    type="number"
                    className="edit-input mono"
                    value={markup}
                    onChange={(e) => setMarkup(Number(e.target.value))}
                    placeholder="如: 50 或 -20"
                  />
                  <div className="quick-markup-tags">
                    <button type="button" onClick={() => setMarkup(0)}>平价出</button>
                    <button type="button" onClick={() => setMarkup(30)}>+¥30</button>
                    <button type="button" onClick={() => setMarkup(50)}>+¥50</button>
                    <button type="button" onClick={() => setMarkup(100)}>+¥100</button>
                    <button type="button" onClick={() => setMarkup(-20)}>-¥20</button>
                  </div>
                </div>
              </div>

              <div className="post-preview-box">
                <div className="post-preview-header">
                  <span>Hostloc / NodeSeek 论坛发帖格式预览</span>
                  <button type="button" className="text-button copy-btn" onClick={handleCopyPost}>
                    {copied ? <Check size={14} className="text-mint" /> : <Copy size={14} />}
                    <span>{copied ? '已复制！' : '一键复制'}</span>
                  </button>
                </div>
                <pre className="post-code mono">{generateForumSalesPost(node, currentBilling, calc, markup)}</pre>
              </div>
            </div>
          )}

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
