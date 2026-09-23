import { useEffect, useState } from 'react'
import { Calendar, Check, Coins, CurrencyCny, DotsThree, Hourglass, Plus, Sparkle, Tag, Warning } from '@phosphor-icons/react'
import { calculateRemainingValue, getNodeBilling } from '../lib/billing.js'
import { BillingModal } from './BillingModal.jsx'

export function BillingCenter({ nodes = [] }) {
  const [refreshTrigger, setRefreshTrigger] = useState(0)
  const [selectedNodeForBilling, setSelectedNodeForBilling] = useState(null)

  // Listen to billing updates
  useEffect(() => {
    const handleUpdate = () => setRefreshTrigger((prev) => prev + 1)
    window.addEventListener('probewatch_billing_updated', handleUpdate)
    return () => window.removeEventListener('probewatch_billing_updated', handleUpdate)
  }, [])

  // Calculate portfolio totals
  const portfolio = nodes.map((node) => {
    const billing = getNodeBilling(node.uuid || node.id, node.name)
    const calc = calculateRemainingValue(billing)
    return { node, billing, calc }
  })

  const totalAnnualCost = portfolio.reduce((sum, item) => sum + item.calc.annualCostCNY, 0)
  const totalMonthlyCost = totalAnnualCost / 12
  const totalRemainingValue = portfolio.reduce((sum, item) => sum + item.calc.remainingValueCNY, 0)
  const expiringSoonCount = portfolio.filter((item) => !item.calc.isExpired && item.calc.daysRemaining <= 30 && item.billing.cycle !== 'free').length

  // Sort by days remaining ascending
  const sortedItems = [...portfolio].sort((a, b) => a.calc.daysRemaining - b.calc.daysRemaining)

  return (
    <div className="billing-center-container">
      {/* 头部标题与简介 */}
      <section className="page-heading">
        <div>
          <div className="eyebrow">MJJ 资产管理 · 账单与续费日历</div>
          <h1>小鸡账单与剩余价值中心<span className="heading-period">。</span></h1>
          <p>全网 VPS 资产台账、续费周期提醒、剩余天数及二手出鸡指导价实时折算。</p>
        </div>
      </section>

      {/* 核心资产指标看板 */}
      <section className="billing-summary-grid">
        <div className="billing-stat-card">
          <div className="stat-label-row">
            <Coins size={18} className="text-mint" />
            <span>全网小鸡总剩余价值</span>
          </div>
          <div className="stat-big-value text-mint mono">
            ¥{totalRemainingValue.toFixed(2)}
          </div>
          <div className="stat-sub-text">基于当前剩余天数与官方汇率实时折算</div>
        </div>

        <div className="billing-stat-card">
          <div className="stat-label-row">
            <CurrencyCny size={18} className="text-blue" />
            <span>年化总续费支出</span>
          </div>
          <div className="stat-big-value text-blue mono">
            ¥{totalAnnualCost.toFixed(2)}
          </div>
          <div className="stat-sub-text">名下所有 VPS 一年维持费用</div>
        </div>

        <div className="billing-stat-card">
          <div className="stat-label-row">
            <Hourglass size={18} className="text-amber" />
            <span>月均维持成本</span>
          </div>
          <div className="stat-big-value text-amber mono">
            ¥{totalMonthlyCost.toFixed(2)} / 月
          </div>
          <div className="stat-sub-text">日均约 ¥{(totalAnnualCost / 365).toFixed(2)}</div>
        </div>

        <div className="billing-stat-card">
          <div className="stat-label-row">
            <Calendar size={18} className="text-rose" />
            <span>30 天内待续费</span>
          </div>
          <div className="stat-big-value text-rose mono">
            {expiringSoonCount} <small style={{ fontSize: '14px' }}>台</small>
          </div>
          <div className="stat-sub-text">避免忘记续费被商家删机</div>
        </div>
      </section>

      {/* 小鸡账单明细列表 */}
      <section className="panel billing-table-panel">
        <div className="panel-header">
          <div>
            <h2>VPS 账单台账与出鸡估值</h2>
            <p>点击任意行或编辑按钮可设置商家、购买价格、到期日，并生成论坛发帖文案</p>
          </div>
        </div>

        <div className="table-scroll">
          <table className="node-table billing-table">
            <thead>
              <tr>
                <th>服务器节点</th>
                <th>商家品牌</th>
                <th>续费价格 / 周期</th>
                <th>下次到期时间</th>
                <th>剩余天数</th>
                <th>实时剩余价值</th>
                <th>日均成本</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {sortedItems.map(({ node, billing, calc }) => (
                <tr key={node.uuid || node.id} className="billing-row">
                  <td>
                    <div className="billing-node-cell">
                      <span className="billing-flag">{node.flag || '🌐'}</span>
                      <div>
                        <strong>{node.name}</strong>
                        <small className="muted">{node.region} · {node.tag}</small>
                      </div>
                    </div>
                  </td>
                  <td>
                    <span className="merchant-tag">{billing.merchant}</span>
                  </td>
                  <td className="mono">
                    {billing.cycle === 'free' ? (
                      <span className="free-tag">永久免费</span>
                    ) : (
                      <b>{calc.symbol}{billing.price} / {billing.cycle}</b>
                    )}
                  </td>
                  <td className="mono">{billing.dueDate || '—'}</td>
                  <td>
                    <span className={`days-pill days-${calc.statusTone} mono`}>
                      {calc.statusText}
                    </span>
                  </td>
                  <td>
                    <b className="mono text-mint" style={{ fontSize: '14px' }}>
                      ¥{calc.remainingValueCNY.toFixed(2)}
                    </b>
                  </td>
                  <td className="mono muted">
                    ¥{calc.dailyCostCNY.toFixed(2)} / 天
                  </td>
                  <td>
                    <div className="action-buttons-group">
                      <button
                        type="button"
                        className="button button-quiet btn-sm"
                        onClick={() => setSelectedNodeForBilling(node)}
                      >
                        <Tag size={13} /> 配置账单
                      </button>
                      <button
                        type="button"
                        className="button button-primary btn-sm"
                        onClick={() => setSelectedNodeForBilling(node)}
                      >
                        <Sparkle size={13} /> 出鸡计算
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      {/* 弹窗配置 */}
      {selectedNodeForBilling && (
        <BillingModal
          node={selectedNodeForBilling}
          onClose={() => setSelectedNodeForBilling(null)}
          onSaved={() => setRefreshTrigger((prev) => prev + 1)}
        />
      )}
    </div>
  )
}
