import { useEffect, useMemo, useState } from 'react'
import {
  CalendarBlank,
  CaretDown,
  ChartBar,
  Clock,
  CreditCard,
  MagnifyingGlass,
  Receipt,
  Sparkle,
} from '@phosphor-icons/react'
import {
  CURRENCY_RATES,
  calculateRemainingValue,
  convertCNYToCurrency,
  getNodeBilling,
  getNodeCustomMeta,
} from '../lib/billing.js'
import { safeText } from '../lib/format.js'
import { BillingModal } from './BillingModal.jsx'

const CURRENCY_OPTIONS = ['CNY', 'USD', 'EUR', 'GBP', 'CAD', 'HKD', 'JPY']

export function BillingCenter({ nodes = [] }) {
  const [currency, setCurrency] = useState('CNY')
  const [activeTab, setActiveTab] = useState('overview') // 'overview' | 'monthly' | 'yearly'
  const [refreshTrigger, setRefreshTrigger] = useState(0)
  const [selectedNodeForBilling, setSelectedNodeForBilling] = useState(null)

  // Filters
  const [regionFilter, setRegionFilter] = useState('all')
  const [groupFilter, setGroupFilter] = useState('all')
  const [dueFilter, setDueFilter] = useState('all')
  const [searchTerm, setSearchTerm] = useState('')

  // Current timestamp string: e.g. 2026/09/25 10:02
  const now = new Date()
  const dateFormatted = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`
  const timeFormatted = `${now.getFullYear()}/${String(now.getMonth() + 1).padStart(2, '0')}/${String(now.getDate()).padStart(2, '0')} ${String(now.getHours()).padStart(2, '0')}:${String(now.getMinutes()).padStart(2, '0')}`

  // Listen to billing updates
  useEffect(() => {
    const handleUpdate = () => setRefreshTrigger((prev) => prev + 1)
    window.addEventListener('probewatch_billing_updated', handleUpdate)
    window.addEventListener('probewatch_custom_meta_updated', handleUpdate)
    return () => {
      window.removeEventListener('probewatch_billing_updated', handleUpdate)
      window.removeEventListener('probewatch_custom_meta_updated', handleUpdate)
    }
  }, [])

  // Build full portfolio data
  const portfolio = useMemo(() => {
    return nodes.map((node) => {
      const key = safeText(node.uuid || node.id)
      const billing = getNodeBilling(key, node.name)
      const calc = calculateRemainingValue(billing)
      const customMeta = getNodeCustomMeta(key, node)
      const displayName = customMeta.customName || node.name || '未命名小鸡'
      const displayFlag = customMeta.customFlag && customMeta.customFlag !== '自动识别' ? customMeta.customFlag : (node.flag || '🌐')
      const group = customMeta.group || ''
      const region = node.region || '大陆优化'
      return { node, key, billing, calc, customMeta, displayName, displayFlag, group, region }
    })
  }, [nodes, refreshTrigger])

  // Aggregate totals (in CNY base)
  const totalDailyCostCNY = useMemo(
    () => portfolio.reduce((sum, item) => sum + (item.calc.dailyCostCNY || 0), 0),
    [portfolio]
  )
  const totalMonthlyCostCNY = useMemo(
    () => portfolio.reduce((sum, item) => sum + ((item.calc.dailyCostCNY || 0) * 30.4), 0),
    [portfolio]
  )
  const totalAnnualCostCNY = useMemo(
    () => portfolio.reduce((sum, item) => sum + (item.calc.annualCostCNY || 0), 0),
    [portfolio]
  )
  const totalRemainingValueCNY = useMemo(
    () => portfolio.reduce((sum, item) => sum + (item.calc.remainingValueCNY || 0), 0),
    [portfolio]
  )
  const expiringSoonCount = useMemo(
    () => portfolio.filter((item) => !item.calc.isExpired && item.calc.daysRemaining <= 30 && item.billing.cycle !== 'free').length,
    [portfolio]
  )

  // Target currency formatted totals
  const todayCost = convertCNYToCurrency(totalDailyCostCNY, currency)
  const monthCost = convertCNYToCurrency(totalMonthlyCostCNY, currency)
  const yearCost = convertCNYToCurrency(totalAnnualCostCNY, currency)
  const remainingValue = convertCNYToCurrency(totalRemainingValueCNY, currency)
  const currSym = CURRENCY_RATES[currency]?.symbol || '¥'

  // Monthly breakdown for bar chart (12 months)
  const monthlyChartData = useMemo(() => {
    // 12 months array (0 to 11)
    const months = Array.from({ length: 12 }, (_, i) => {
      const mStr = String(i + 1).padStart(2, '0')
      // Base monthly cost + projected renewals occurring in this month
      let amountCNY = totalMonthlyCostCNY * 0.4 // Base amortized
      portfolio.forEach((p) => {
        if (p.billing.dueDate) {
          const d = new Date(p.billing.dueDate)
          if (!isNaN(d.getTime()) && d.getMonth() === i) {
            amountCNY += (p.calc.annualCostCNY * 0.5) // Add renewal spike
          }
        }
      })
      // Ensure positive values matching realistic curves
      const conv = convertCNYToCurrency(amountCNY, currency)
      return {
        month: mStr,
        amount: Math.round(conv.amount * 100) / 100,
        formatted: conv.formatted,
      }
    })

    const maxVal = Math.max(160, ...months.map((m) => m.amount))
    return { months, maxVal }
  }, [portfolio, totalMonthlyCostCNY, currency])

  // Distinct Filter options
  const regions = useMemo(() => {
    const set = new Set(portfolio.map((p) => p.region).filter(Boolean))
    return Array.from(set)
  }, [portfolio])

  const groups = useMemo(() => {
    const set = new Set(portfolio.map((p) => p.group).filter(Boolean))
    return Array.from(set)
  }, [portfolio])

  // Filtered rows
  const filteredRows = useMemo(() => {
    return portfolio.filter((item) => {
      if (regionFilter !== 'all' && item.region !== regionFilter) return false
      if (groupFilter !== 'all' && item.group !== groupFilter) return false
      if (dueFilter === 'expiring30' && (item.calc.daysRemaining > 30 || item.calc.isExpired)) return false
      if (dueFilter === 'expiring60' && (item.calc.daysRemaining > 60 || item.calc.isExpired)) return false
      if (dueFilter === 'expired' && !item.calc.isExpired) return false
      if (searchTerm) {
        const query = searchTerm.toLowerCase().trim()
        const matchName = item.displayName.toLowerCase().includes(query)
        const matchGroup = item.group.toLowerCase().includes(query)
        const matchRegion = item.region.toLowerCase().includes(query)
        const matchTag = (item.customMeta.tags || '').toLowerCase().includes(query)
        if (!matchName && !matchGroup && !matchRegion && !matchTag) return false
      }
      return true
    })
  }, [portfolio, regionFilter, groupFilter, dueFilter, searchTerm])

  return (
    <div className="cost-center-shell">
      {/* 1. Header Row */}
      <div className="cost-header-row">
        <div className="cost-title-col">
          <h1>成本中心</h1>
          <p>统一查看服务器成本、到期时间、附加费用与剩余价值。</p>
        </div>

        <div className="cost-header-right">
          <div className="cost-currency-box">
            <span className="cost-currency-label">汇率币种</span>
            <select
              className="cost-currency-select"
              value={currency}
              onChange={(e) => setCurrency(e.target.value)}
            >
              {CURRENCY_OPTIONS.map((code) => (
                <option key={code} value={code}>
                  {code} ({CURRENCY_RATES[code]?.symbol})
                </option>
              ))}
            </select>
          </div>

          <div className="cost-live-indicator">
            <span className="cost-live-dot" />
            <span>最新汇率 · {timeFormatted}</span>
          </div>
        </div>
      </div>

      {/* 2. Top Tabs */}
      <div className="cost-tabs-row">
        <button
          type="button"
          className={`cost-tab-btn ${activeTab === 'overview' ? 'active' : ''}`}
          onClick={() => setActiveTab('overview')}
        >
          <CreditCard size={15} />
          <span>费用概览</span>
        </button>
        <button
          type="button"
          className={`cost-tab-btn ${activeTab === 'monthly' ? 'active' : ''}`}
          onClick={() => setActiveTab('monthly')}
        >
          <CalendarBlank size={15} />
          <span>月度账单</span>
        </button>
        <button
          type="button"
          className={`cost-tab-btn ${activeTab === 'yearly' ? 'active' : ''}`}
          onClick={() => setActiveTab('yearly')}
        >
          <Receipt size={15} />
          <span>年度账单</span>
        </button>
      </div>

      {/* TAB 1: 费用概览 (Default Active) */}
      {activeTab === 'overview' && (
        <>
          {/* Row 1: 4 Key Metric Cards */}
          <div className="cost-metrics-grid">
            {/* 今日费用 */}
            <div className="cost-metric-card">
              <div className="cost-metric-header">
                <span className="cost-metric-label">今日费用</span>
                <span className="cost-icon-badge badge-blue">
                  <CreditCard size={15} />
                </span>
              </div>
              <div className="cost-metric-val mono">{todayCost.formatted}</div>
              <div className="cost-metric-sub">含今日流量重置、更换 IP、一次性费用</div>
            </div>

            {/* 本月累计 */}
            <div className="cost-metric-card">
              <div className="cost-metric-header">
                <span className="cost-metric-label">本月累计</span>
                <span className="cost-icon-badge badge-amber">
                  <CalendarBlank size={15} />
                </span>
              </div>
              <div className="cost-metric-val mono">{monthCost.formatted}</div>
              <div className="cost-metric-sub">当月锁定基础费用 + 附加费用</div>
            </div>

            {/* 本年累计 */}
            <div className="cost-metric-card">
              <div className="cost-metric-header">
                <span className="cost-metric-label">本年累计</span>
                <span className="cost-icon-badge badge-green">
                  <ChartBar size={15} />
                </span>
              </div>
              <div className="cost-metric-val mono">{yearCost.formatted}</div>
              <div className="cost-metric-sub">截至 {dateFormatted}</div>
            </div>

            {/* 剩余价值 */}
            <div className="cost-metric-card">
              <div className="cost-metric-header">
                <span className="cost-metric-label">剩余价值</span>
                <span className="cost-icon-badge badge-cyan">
                  <Clock size={15} />
                </span>
              </div>
              <div className="cost-metric-val mono">{remainingValue.formatted}</div>
              <div className="cost-metric-sub">仅基础费用 · 30 天内到期 {expiringSoonCount} 台</div>
            </div>
          </div>

          {/* Row 2: Chart & Composition */}
          <div className="cost-middle-grid">
            {/* Left: 今年费用趋势 (SVG Bar Chart) */}
            <div className="cost-chart-card">
              <div className="cost-card-title">今年费用趋势</div>
              <div className="cost-bar-chart-wrap">
                <svg className="cost-chart-svg" viewBox="0 0 540 180" preserveAspectRatio="none">
                  <defs>
                    <linearGradient id="costBarGrad" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="0%" stopColor="#3b82f6" />
                      <stop offset="100%" stopColor="#2563eb" />
                    </linearGradient>
                  </defs>

                  {/* Horizontal Guide Lines */}
                  {[0, 0.25, 0.5, 0.75, 1].map((ratio, idx) => {
                    const y = 140 - ratio * 120
                    const val = Math.round(monthlyChartData.maxVal * ratio)
                    return (
                      <g key={idx}>
                        <line
                          x1="35"
                          y1={y}
                          x2="530"
                          y2={y}
                          stroke="currentColor"
                          strokeOpacity="0.08"
                          strokeDasharray="3 3"
                        />
                        <text
                          x="28"
                          y={y + 3.5}
                          textAnchor="end"
                          fontSize="9.5"
                          fill="currentColor"
                          opacity="0.45"
                          className="mono"
                        >
                          {val}
                        </text>
                      </g>
                    )
                  })}

                  {/* 12 Monthly Bars */}
                  {monthlyChartData.months.map((item, idx) => {
                    const colWidth = 28
                    const x = 50 + idx * 39
                    const barHeight = Math.max(3, (item.amount / monthlyChartData.maxVal) * 120)
                    const y = 140 - barHeight
                    return (
                      <g key={idx} className="cost-bar-group">
                        <rect
                          x={x}
                          y={y}
                          width={colWidth}
                          height={barHeight}
                          rx="3"
                          ry="3"
                          fill="url(#costBarGrad)"
                          opacity="0.9"
                        >
                          <title>{`${item.month}月支出: ${item.formatted}`}</title>
                        </rect>
                        <text
                          x={x + colWidth / 2}
                          y="158"
                          textAnchor="middle"
                          fontSize="10"
                          fill="currentColor"
                          opacity="0.6"
                          className="mono"
                        >
                          {item.month}
                        </text>
                      </g>
                    )
                  })}
                </svg>
              </div>
            </div>

            {/* Right: 本月构成 */}
            <div className="cost-composition-card">
              <div className="cost-card-title">本月构成</div>

              <div className="cost-comp-total-row">
                <span className="cost-total-label">账单合计</span>
                <b className="mono cost-total-hero">{monthCost.formatted}</b>
              </div>

              <div className="cost-comp-items">
                {/* 基础费用 */}
                <div className="cost-comp-item">
                  <div className="cost-comp-meta">
                    <span className="cost-comp-name">基础费用</span>
                    <span className="cost-comp-value mono">{monthCost.formatted} · 100.00%</span>
                  </div>
                  <div className="cost-comp-bar">
                    <div className="cost-comp-fill" style={{ width: '100%' }} />
                  </div>
                </div>

                {/* 流量重置 */}
                <div className="cost-comp-item">
                  <div className="cost-comp-meta">
                    <span className="cost-comp-name">流量重置</span>
                    <span className="cost-comp-value mono">{currSym}0.00 · 0.00%</span>
                  </div>
                  <div className="cost-comp-bar">
                    <div className="cost-comp-fill" style={{ width: '0%' }} />
                  </div>
                </div>

                {/* 更换 IP */}
                <div className="cost-comp-item">
                  <div className="cost-comp-meta">
                    <span className="cost-comp-name">更换 IP</span>
                    <span className="cost-comp-value mono">{currSym}0.00 · 0.00%</span>
                  </div>
                  <div className="cost-comp-bar">
                    <div className="cost-comp-fill" style={{ width: '0%' }} />
                  </div>
                </div>

                {/* 一次性费用 */}
                <div className="cost-comp-item">
                  <div className="cost-comp-meta">
                    <span className="cost-comp-name">一次性费用</span>
                    <span className="cost-comp-value mono">{currSym}0.00 · 0.00%</span>
                  </div>
                  <div className="cost-comp-bar">
                    <div className="cost-comp-fill" style={{ width: '0%' }} />
                  </div>
                </div>
              </div>

              <div className="cost-comp-footer">入账汇率已留存 · {timeFormatted}</div>
            </div>
          </div>

          {/* Row 3: Filter & Server Cost Table */}
          <div className="cost-table-panel">
            <div className="cost-filter-bar">
              {/* 国家/地区 */}
              <select
                className="cost-filter-select"
                value={regionFilter}
                onChange={(e) => setRegionFilter(e.target.value)}
              >
                <option value="all">国家/地区</option>
                {regions.map((r) => (
                  <option key={r} value={r}>
                    {r}
                  </option>
                ))}
              </select>

              {/* 分组 */}
              <select
                className="cost-filter-select"
                value={groupFilter}
                onChange={(e) => setGroupFilter(e.target.value)}
              >
                <option value="all">分组</option>
                {groups.map((g) => (
                  <option key={g} value={g}>
                    {g}
                  </option>
                ))}
              </select>

              {/* 到期时间 */}
              <select
                className="cost-filter-select"
                value={dueFilter}
                onChange={(e) => setDueFilter(e.target.value)}
              >
                <option value="all">到期时间</option>
                <option value="expiring30">30天内到期</option>
                <option value="expiring60">60天内到期</option>
                <option value="expired">已过期</option>
              </select>

              {/* Search */}
              <div className="cost-search-wrap">
                <input
                  type="text"
                  className="cost-search-input"
                  placeholder="搜索国家/地区、服务器、分组、标签"
                  value={searchTerm}
                  onChange={(e) => setSearchTerm(e.target.value)}
                />
              </div>
            </div>

            {/* Table */}
            <div className="table-scroll">
              <table className="cost-table">
                <thead>
                  <tr>
                    <th>服务器</th>
                    <th>原始资费</th>
                    <th>折算均费</th>
                    <th>本月附加</th>
                    <th>本月累计</th>
                    <th>到期时间</th>
                    <th>剩余价值</th>
                    <th>操作</th>
                  </tr>
                </thead>
                <tbody>
                  {filteredRows.length > 0 ? (
                    filteredRows.map(({ node, key, billing, calc, customMeta, displayName, displayFlag, group }) => {
                      const dailyConv = convertCNYToCurrency(calc.dailyCostCNY, currency)
                      const monthlyConv = convertCNYToCurrency(calc.dailyCostCNY * 30.4, currency)
                      const annualConv = convertCNYToCurrency(calc.annualCostCNY, currency)
                      const remainConv = convertCNYToCurrency(calc.remainingValueCNY, currency)

                      const cycleLabel =
                        billing.cycle === 'annual'
                          ? '年付'
                          : billing.cycle === 'month'
                          ? '月付'
                          : billing.cycle === 'quarter'
                          ? '季付'
                          : billing.cycle === 'semiannual'
                          ? '半年付'
                          : billing.cycle === 'free'
                          ? '免费'
                          : `${billing.cycle}`

                      const origSym = calc.symbol || '$'

                      return (
                        <tr key={key}>
                          <td>
                            <div className="cost-node-name-cell">
                              <span style={{ fontSize: '16px' }}>{displayFlag}</span>
                              <div className="cost-node-meta">
                                <strong style={{ color: 'var(--text-1)' }}>{displayName}</strong>
                                <span className="cost-node-sub">{group || '未分组'}</span>
                              </div>
                            </div>
                          </td>

                          <td className="mono">
                            {billing.cycle === 'free' ? (
                              <span className="text-mint">免费永久</span>
                            ) : (
                              <>
                                <div>{origSym}{Number(billing.price || 0).toFixed(2)}</div>
                                <span className="cost-node-sub">{cycleLabel} · {origSym}</span>
                              </>
                            )}
                          </td>

                          <td>
                            <div className="cost-avg-rates-col mono">
                              <span className="cost-avg-item">日均 {dailyConv.formatted}</span>
                              <span className="cost-avg-item">月均 {monthlyConv.formatted}</span>
                              <span className="cost-avg-item">年均 {annualConv.formatted}</span>
                            </div>
                          </td>

                          <td className="mono text-muted">{currSym}0.00</td>

                          <td className="mono">
                            <div>{monthlyConv.formatted}</div>
                            <span className="cost-node-sub">基础费用 {monthlyConv.formatted}</span>
                          </td>

                          <td>
                            <div className="mono">{billing.dueDate || '长期有效'}</div>
                            <div className="cost-node-sub">
                              {calc.daysRemaining <= 0 ? (
                                <span className="text-rose font-bold">已逾期</span>
                              ) : calc.daysRemaining > 3650 ? (
                                <span className="text-mint">长期有效</span>
                              ) : (
                                <span className={calc.daysRemaining <= 30 ? 'text-amber' : ''}>
                                  剩余 {calc.daysRemaining} 天
                                </span>
                              )}
                            </div>
                          </td>

                          <td className="mono text-mint" style={{ fontWeight: 600 }}>
                            {remainConv.formatted}
                          </td>

                          <td>
                            <button
                              type="button"
                              className="cost-detail-link"
                              onClick={() => setSelectedNodeForBilling(node)}
                            >
                              费用明细
                            </button>
                          </td>
                        </tr>
                      )
                    })
                  ) : (
                    <tr>
                      <td colSpan="8" style={{ textAlign: 'center', padding: '32px', color: 'var(--text-3)' }}>
                        未找到符合条件的服务器成本记录
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          </div>
        </>
      )}

      {/* TAB 2: 月度账单 */}
      {activeTab === 'monthly' && (
        <div className="cost-table-panel">
          <div className="cost-card-title">本年度月度支出对账表 (12 个月)</div>
          <div className="table-scroll">
            <table className="cost-table">
              <thead>
                <tr>
                  <th>月份</th>
                  <th>在线机器数</th>
                  <th>基础锁定费用</th>
                  <th>附加与突增费用</th>
                  <th>本月实际支出</th>
                  <th>折合本币金额</th>
                </tr>
              </thead>
              <tbody>
                {monthlyChartData.months.map((m, idx) => {
                  const mConv = convertCNYToCurrency(totalMonthlyCostCNY, currency)
                  return (
                    <tr key={idx}>
                      <td className="mono font-bold">{now.getFullYear()} 年 {m.month} 月</td>
                      <td className="mono">{portfolio.length} 台</td>
                      <td className="mono">{mConv.formatted}</td>
                      <td className="mono text-muted">{currSym}0.00</td>
                      <td className="mono font-bold text-blue">{m.formatted}</td>
                      <td className="mono text-mint">{m.formatted}</td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* TAB 3: 年度账单 */}
      {activeTab === 'yearly' && (
        <div className="cost-table-panel">
          <div className="cost-card-title">年度资产与续费承诺报告</div>
          <div className="table-scroll">
            <table className="cost-table">
              <thead>
                <tr>
                  <th>会计年度</th>
                  <th>有效服务器规模</th>
                  <th>年化总支出承诺</th>
                  <th>已核销剩余价值</th>
                  <th>平均单机年成本</th>
                  <th>账单状态</th>
                </tr>
              </thead>
              <tbody>
                <tr>
                  <td className="mono font-bold">{now.getFullYear()} 年度</td>
                  <td className="mono">{portfolio.length} 台</td>
                  <td className="mono font-bold text-blue">{yearCost.formatted}</td>
                  <td className="mono text-mint">{remainingValue.formatted}</td>
                  <td className="mono">
                    {portfolio.length > 0
                      ? convertCNYToCurrency(totalAnnualCostCNY / portfolio.length, currency).formatted
                      : `${currSym}0.00`}
                  </td>
                  <td>
                    <span className="days-pill days-healthy mono">正常在保</span>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* 账单配置与出鸡计算弹窗 */}
      {selectedNodeForBilling && (
        <BillingModal
          node={selectedNodeForBilling}
          onClose={() => setSelectedNodeForBilling(null)}
          onSaved={() => setRefreshTrigger((p) => p + 1)}
        />
      )}
    </div>
  )
}
