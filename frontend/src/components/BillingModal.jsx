import { useState } from 'react'
import { Check, Clock, Coins, Copy, CurrencyCny, Info, Sparkle, Tag, X } from '@phosphor-icons/react'
import { BILLING_CYCLES, COMMON_MERCHANTS, CURRENCY_RATES, calculateRemainingValue, generateForumSalesPost, getNodeBilling, saveNodeBillingData } from '../lib/billing.js'

export function BillingModal({ node, onClose, onSaved }) {
  const initial = getNodeBilling(node.uuid || node.id, node.name)
  const [merchant, setMerchant] = useState(initial.merchant)
  const [price, setPrice] = useState(initial.price)
  const [currency, setCurrency] = useState(initial.currency)
  const [cycle, setCycle] = useState(initial.cycle)
  const [dueDate, setDueDate] = useState(initial.dueDate)
  const [markup, setMarkup] = useState(0)
  const [copied, setCopied] = useState(false)
  const [activeTab, setActiveTab] = useState('config') // 'config' | 'calculator'

  const currentBilling = { merchant, price: Number(price) || 0, currency, cycle, dueDate }
  const calc = calculateRemainingValue(currentBilling)
  const totalPriceCNY = Math.max(0, calc.remainingValueCNY + Number(markup))

  const handleSave = (e) => {
    e.preventDefault()
    saveNodeBillingData(node.uuid || node.id, { merchant, price: Number(price) || 0, currency, cycle, dueDate })
    if (onSaved) onSaved()
    onClose()
  }

  const handleCopyPost = () => {
    const text = generateForumSalesPost(node, currentBilling, calc, markup)
    navigator.clipboard.writeText(text)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="billing-modal" onClick={(e) => e.stopPropagation()}>
        {/* Header */}
        <div className="billing-modal-header">
          <div className="billing-modal-title">
            <Coins size={22} className="text-amber" weight="duotone" />
            <div>
              <strong>小鸡账单与剩余价值管理</strong>
              <small>{node.flag} {node.name} · {node.region}</small>
            </div>
          </div>
          <button className="icon-button" onClick={onClose} aria-label="关闭"><X size={18} /></button>
        </div>

        {/* Tab 导航 */}
        <div className="billing-modal-tabs">
          <button
            type="button"
            className={`billing-tab-btn ${activeTab === 'config' ? 'active' : ''}`}
            onClick={() => setActiveTab('config')}
          >
            <Tag size={15} /> 账单配置
          </button>
          <button
            type="button"
            className={`billing-tab-btn ${activeTab === 'calculator' ? 'active' : ''}`}
            onClick={() => setActiveTab('calculator')}
          >
            <Sparkle size={15} /> 论坛出鸡/收鸡计算器
          </button>
        </div>

        {/* Tab 1: 账单配置 */}
        {activeTab === 'config' && (
          <form onSubmit={handleSave} className="billing-form">
            <div className="billing-form-grid">
              <div className="input-group">
                <label>商家 / 服务商</label>
                <input
                  list="merchant-list"
                  className="modal-input"
                  value={merchant}
                  onChange={(e) => setMerchant(e.target.value)}
                  placeholder="例如: 搬瓦工 / DMIT / 腾讯云"
                />
                <datalist id="merchant-list">
                  {COMMON_MERCHANTS.map((m) => <option key={m} value={m} />)}
                </datalist>
              </div>

              <div className="input-group">
                <label>续费币种</label>
                <select className="modal-input" value={currency} onChange={(e) => setCurrency(e.target.value)}>
                  {Object.entries(CURRENCY_RATES).map(([code, info]) => (
                    <option key={code} value={code}>{code} ({info.symbol} · 汇率约 {info.rate})</option>
                  ))}
                </select>
              </div>

              <div className="input-group">
                <label>续费价格 ({CURRENCY_RATES[currency]?.symbol || '¥'})</label>
                <input
                  type="number"
                  step="0.01"
                  className="modal-input mono"
                  value={price}
                  onChange={(e) => setPrice(e.target.value)}
                  placeholder="如: 18.88"
                />
              </div>

              <div className="input-group">
                <label>支付周期</label>
                <select className="modal-input" value={cycle} onChange={(e) => setCycle(e.target.value)}>
                  {BILLING_CYCLES.map((c) => (
                    <option key={c.id} value={c.id}>{c.label} ({c.days > 0 ? `${c.days}天` : '永久'})</option>
                  ))}
                </select>
              </div>

              <div className="input-group full-width">
                <label>下次到期 / 续费日</label>
                <input
                  type="date"
                  className="modal-input mono"
                  value={dueDate}
                  onChange={(e) => setDueDate(e.target.value)}
                />
              </div>
            </div>

            {/* 实时剩余价值预览面板 */}
            <div className="billing-preview-card">
              <div className="preview-stat">
                <span>剩余天数</span>
                <b className={`mono ${calc.daysRemaining <= 7 ? 'text-rose' : calc.daysRemaining <= 30 ? 'text-amber' : 'text-mint'}`}>
                  {calc.daysRemaining > 0 ? `${calc.daysRemaining} 天` : '已到期'}
                </b>
              </div>
              <div className="preview-stat">
                <span>原币剩余价值</span>
                <b className="mono">{calc.symbol}{calc.remainingValueOriginal}</b>
              </div>
              <div className="preview-stat primary-stat">
                <span>折合剩余价值 (CNY)</span>
                <b className="mono text-mint">¥{calc.remainingValueCNY.toFixed(2)}</b>
              </div>
              <div className="preview-stat">
                <span>年化折合成本</span>
                <b className="mono text-muted">¥{calc.annualCostCNY.toFixed(0)}/年</b>
              </div>
            </div>

            <div className="modal-actions-row">
              <button type="button" className="button button-quiet" onClick={onClose}>取消</button>
              <button type="submit" className="button button-primary">保存账单配置</button>
            </div>
          </form>
        )}

        {/* Tab 2: 出鸡/收鸡交易计算器 */}
        {activeTab === 'calculator' && (
          <div className="sales-calculator-view">
            <div className="calc-banner">
              <div className="calc-banner-header">
                <div>
                  <small>官方折算剩余价值</small>
                  <div className="calc-hero-val mono">¥{calc.remainingValueCNY.toFixed(2)}</div>
                </div>
                <div className="text-right">
                  <small>建议交易总价 (包Push)</small>
                  <div className="calc-total-val mono text-amber">¥{totalPriceCNY.toFixed(2)}</div>
                </div>
              </div>

              <div className="markup-input-box">
                <label>
                  <span>心理溢价 / 骨折优惠金额 (元)</span>
                  <small className="text-muted">正数代表传家宝溢价，负数代表打折出机</small>
                </label>
                <div className="markup-input-wrap">
                  <span className="currency-prefix">¥</span>
                  <input
                    type="number"
                    className="modal-input mono"
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
            </div>

            <div className="post-preview-box">
              <div className="post-preview-header">
                <span>NodeSeek / Hostloc 论坛发帖格式预览</span>
                <button type="button" className="text-button copy-btn" onClick={handleCopyPost}>
                  {copied ? <Check size={14} className="text-mint" /> : <Copy size={14} />}
                  <span>{copied ? '已复制到剪贴板！' : '一键复制出鸡帖文'}</span>
                </button>
              </div>
              <pre className="post-code mono">{generateForumSalesPost(node, currentBilling, calc, markup)}</pre>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
