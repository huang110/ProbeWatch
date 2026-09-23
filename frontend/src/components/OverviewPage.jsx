import { useEffect, useMemo, useState } from 'react'
import {
  ArrowClockwise,
  ArrowDown,
  ArrowUp,
  ArrowUpRight,
  CircleNotch,
  Coins,
  Cpu,
  Gauge,
  HardDrives,
  MagnifyingGlass,
  Memory,
  Package,
  Plus,
  Pulse,
  Rows,
  SortAscending,
  SquaresFour,
  X,
} from '@phosphor-icons/react'
import { formatBytes, formatRate, numeric, safeText } from '../lib/format.js'
import { calculateRemainingValue, getNodeBilling } from '../lib/billing.js'
import { NodeTable, getRegionalGroup } from './NodeList.jsx'
import { RankCard } from './RankList.jsx'
import { RecentAlerts } from './AlertList.jsx'
import { AddServerModal } from './AddServerModal.jsx'

export function OverviewPage({
  data = [],
  overview,
  alerts = [],
  lossRates = {},
  rates = {},
  statHistory = [],
  onAck,
  ackingId,
  selectedNode,
  onSelectNode,
  onNavigate,
  isRefreshing,
  onRefresh,
  lastSyncText,
  apiState,
}) {
  const [query, setQuery] = useState('')
  const [statusFilter, setStatusFilter] = useState('全部')
  const [groupTab, setGroupTab] = useState('all') // 'all' | 'asia' | 'america' | 'europe' | 'direct' | 'other'
  const [sortBy, setSortBy] = useState('default')
  const [viewMode, setViewMode] = useState('grid') // 'grid' | 'table' | 'compact'
  const [showAddServer, setShowAddServer] = useState(false)
  const [billingVersion, setBillingVersion] = useState(0)

  useEffect(() => {
    const handleUpdate = () => setBillingVersion((v) => v + 1)
    window.addEventListener('probewatch_billing_updated', handleUpdate)
    return () => window.removeEventListener('probewatch_billing_updated', handleUpdate)
  }, [])

  // 1. 实时全网带宽与累计流量聚合计算（严格过滤离线与计数器越界异常值）
  const { totalDownRate, totalUpRate, totalRxBytes, totalTxBytes, totalRemainingCNY, countsByGroup, onlineCount, totalCount } = useMemo(() => {
    let down = 0
    let up = 0
    let rx = 0
    let tx = 0
    let remaining = 0
    let online = 0
    const counts = { all: data.length, asia: 0, america: 0, europe: 0, direct: 0, other: 0 }

    data.forEach((node, index) => {
      const key = safeText(node.uuid) || safeText(node.id) || `node-${index}`
      const isNodeOnline = node.status === 'online'
      if (isNodeOnline) online += 1

      const rate = rates[key]
      if (isNodeOnline && rate) {
        if (typeof rate.down === 'number' && rate.down > 0 && rate.down < 1e11) down += rate.down
        if (typeof rate.up === 'number' && rate.up > 0 && rate.up < 1e11) up += rate.up
      }

      if (typeof node.rx === 'number' && node.rx >= 0) rx += node.rx
      if (typeof node.tx === 'number' && node.tx >= 0) tx += node.tx

      const billing = getNodeBilling(key, node.name)
      const calc = calculateRemainingValue(billing)
      if (calc && typeof calc.remainingValueCNY === 'number') {
        remaining += calc.remainingValueCNY
      }

      const group = getRegionalGroup(node)
      if (counts[group] !== undefined) counts[group] += 1
      else counts.other += 1
    })

    return {
      totalDownRate: down,
      totalUpRate: up,
      totalRxBytes: rx,
      totalTxBytes: tx,
      totalRemainingCNY: remaining,
      countsByGroup: counts,
      onlineCount: online,
      totalCount: data.length,
    }
  }, [data, rates, billingVersion])

  const checks = overview?.checks || {}
  const avgLatency = numeric(checks.avg_latency_ms)
  const successRate = numeric(checks.success_rate)

  // 2. 节点过滤与多维排序
  const filteredNodes = useMemo(() => {
    const list = data.filter((node) => {
      // 区域分组过滤
      if (groupTab !== 'all') {
        const group = getRegionalGroup(node)
        if (group !== groupTab) return false
      }
      // 在线状态过滤
      if (statusFilter === '在线' && node.status !== 'online') return false
      if (statusFilter === '需关注' && node.status !== 'attention') return false
      if (statusFilter === '离线' && node.status !== 'offline') return false
      // 关键字搜索过滤
      if (query.trim()) {
        const q = query.trim().toLowerCase()
        const match = `${node.name || ''} ${node.tag || ''} ${node.region || ''} ${node.os || ''} ${node.uuid || ''} ${node.id || ''}`.toLowerCase()
        if (!match.includes(q)) return false
      }
      return true
    })

    // 排序
    const sorted = [...list]
    if (sortBy === 'status') {
      const order = { online: 0, attention: 1, offline: 2 }
      sorted.sort((a, b) => (order[a.status] ?? 3) - (order[b.status] ?? 3))
    } else if (sortBy === 'downRate') {
      sorted.sort((a, b) => {
        const rateA = rates[a.uuid || a.id]?.down || 0
        const rateB = rates[b.uuid || b.id]?.down || 0
        return rateB - rateA
      })
    } else if (sortBy === 'upRate') {
      sorted.sort((a, b) => {
        const rateA = rates[a.uuid || a.id]?.up || 0
        const rateB = rates[b.uuid || b.id]?.up || 0
        return rateB - rateA
      })
    } else if (sortBy === 'cpu') {
      sorted.sort((a, b) => (b.cpu || 0) - (a.cpu || 0))
    } else if (sortBy === 'mem') {
      sorted.sort((a, b) => (b.memory || 0) - (a.memory || 0))
    } else if (sortBy === 'remainingValue') {
      sorted.sort((a, b) => {
        const valA = calculateRemainingValue(getNodeBilling(a.uuid || a.id, a.name)).remainingValueCNY
        const valB = calculateRemainingValue(getNodeBilling(b.uuid || b.id, b.name)).remainingValueCNY
        return valB - valA
      })
    } else if (sortBy === 'dueDate') {
      sorted.sort((a, b) => {
        const daysA = calculateRemainingValue(getNodeBilling(a.uuid || a.id, a.name)).daysRemaining
        const daysB = calculateRemainingValue(getNodeBilling(b.uuid || b.id, b.name)).daysRemaining
        return daysA - daysB
      })
    } else if (sortBy === 'name') {
      sorted.sort((a, b) => (a.name || '').localeCompare(b.name || ''))
    }

    return sorted
  }, [data, groupTab, statusFilter, query, sortBy, rates, billingVersion])

  const selectedId = selectedNode ? (selectedNode.uuid || selectedNode.id) : null

  // 运维情报 Top 5 榜单 (useMemo 避免频繁排序重算)
  const cpuTop = useMemo(() => {
    return data
      .filter((node) => node.cpu !== null)
      .sort((a, b) => (b.cpu || 0) - (a.cpu || 0))
      .slice(0, 5)
      .map((node) => ({
        key: node.uuid || node.id,
        label: node.name,
        value: node.cpu,
        percent: node.cpu,
        tone: 'mint',
        kind: 'cpu',
      }))
  }, [data])

  const memTop = useMemo(() => {
    return data
      .filter((node) => node.memory !== null)
      .sort((a, b) => (b.memory || 0) - (a.memory || 0))
      .slice(0, 5)
      .map((node) => ({
        key: node.uuid || node.id,
        label: node.name,
        value: node.memory,
        percent: node.memory,
        tone: 'blue',
        kind: 'mem',
      }))
  }, [data])

  const lossTop = useMemo(() => {
    return data
      .filter((node) => numeric(lossRates[node.uuid || node.id]) !== null)
      .sort((a, b) => (lossRates[b.uuid || b.id] || 0) - (lossRates[a.uuid || a.id] || 0))
      .slice(0, 5)
      .map((node) => ({
        key: node.uuid || node.id,
        label: node.name,
        value: lossRates[node.uuid || node.id],
        percent: Math.min((lossRates[node.uuid || node.id] || 0) * 100, 100),
        tone: 'amber',
        kind: 'loss',
      }))
  }, [data, lossRates])

  return (
    <div className="overview-page-wrap">
      {/* 顶部标题区 */}
      <section className="page-heading komari-page-heading">
        <div className="heading-left">
          <div className="eyebrow">全球探针集群 · 极速自驱上报</div>
          <h1>
            服务器总览<span className="heading-period">。</span>
          </h1>
          <p>全景实时网络流速、核心负载与小鸡资产估值看板</p>
        </div>
        <div className="heading-actions">
          <span className="last-sync">
            最后同步 <b>{lastSyncText}</b>
          </span>
          <button
            type="button"
            className="button button-quiet"
            onClick={onRefresh}
            disabled={isRefreshing}
          >
            {isRefreshing ? <CircleNotch size={16} className="spin" /> : <ArrowClockwise size={16} />}
            <span>{isRefreshing ? '正在同步…' : '刷新数据'}</span>
          </button>
          <button
            type="button"
            className="button button-primary btn-add-server"
            onClick={() => setShowAddServer(true)}
          >
            <Plus size={16} weight="bold" />
            <span>添加服务器</span>
          </button>
        </div>
      </section>

      {/* 登录/状态异常通知条 */}
      {apiState.message && (
        <div className={`api-state api-state-${apiState.kind}`} role="status">
          {apiState.message}
          {apiState.kind === 'auth' && (
            <button
              className="button button-primary"
              onClick={() => { window.location.href = '/auth/github' }}
            >
              使用 GitHub 登录
            </button>
          )}
        </div>
      )}

      {/* 🌟 1. Komari / Nezha 核心特色：全网实时总吞吐大看板 (Global Throughput Bar) */}
      <section className="global-throughput-bar" aria-label="全网实时指标看板">
        {/* 在线比例 */}
        <div className="throughput-item">
          <div className="throughput-icon-wrapper tone-mint">
            <span className="status-dot-pulse" />
          </div>
          <div className="throughput-info">
            <span className="throughput-label">节点在线状态</span>
            <div className="throughput-value mono">
              <b>{onlineCount}</b> / {totalCount}
              <small className="online-ratio-tag">
                {totalCount > 0 ? `${Math.round((onlineCount / totalCount) * 100)}%` : '0%'}
              </small>
            </div>
          </div>
        </div>

        {/* 实时下行总带宽 */}
        <div className="throughput-item">
          <div className="throughput-icon-wrapper tone-mint">
            <ArrowDown size={18} weight="bold" />
          </div>
          <div className="throughput-info">
            <span className="throughput-label">全网实时下载 ↓</span>
            <div className="throughput-value mono text-mint">
              <b>{formatRate(totalDownRate)}</b>
            </div>
          </div>
        </div>

        {/* 实时上行总带宽 */}
        <div className="throughput-item">
          <div className="throughput-icon-wrapper tone-blue">
            <ArrowUp size={18} weight="bold" />
          </div>
          <div className="throughput-info">
            <span className="throughput-label">全网实时上传 ↑</span>
            <div className="throughput-value mono text-blue">
              <b>{formatRate(totalUpRate)}</b>
            </div>
          </div>
        </div>

        {/* 累计出入站流量 */}
        <div className="throughput-item">
          <div className="throughput-icon-wrapper tone-violet">
            <HardDrives size={18} />
          </div>
          <div className="throughput-info">
            <span className="throughput-label">全网累计出入流量</span>
            <div className="throughput-value mono">
              <b>{formatBytes(totalRxBytes + totalTxBytes)}</b>
              <small className="traffic-split-tag" title={`出站: ${formatBytes(totalTxBytes)} | 入站: ${formatBytes(totalRxBytes)}`}>
                Tx {formatBytes(totalTxBytes)}
              </small>
            </div>
          </div>
        </div>

        {/* 检测延迟与可用率 */}
        <div className="throughput-item">
          <div className="throughput-icon-wrapper tone-amber">
            <Gauge size={18} />
          </div>
          <div className="throughput-info">
            <span className="throughput-label">平均延迟 / SLA</span>
            <div className="throughput-value mono">
              <b>{avgLatency !== null ? `${avgLatency}ms` : '—'}</b>
              <small className="sla-tag text-mint">
                {successRate !== null ? `${successRate}%` : '99.9%'}
              </small>
            </div>
          </div>
        </div>

        {/* 🌟 小鸡资产总折合剩余价值 (MJJ 模式) */}
        <div className="throughput-item throughput-item-billing">
          <div className="throughput-icon-wrapper tone-amber-gold">
            <Coins size={18} weight="duotone" />
          </div>
          <div className="throughput-info">
            <span className="throughput-label">小鸡资产总剩余价值</span>
            <div className="throughput-value mono text-amber">
              ¥<b>{totalRemainingCNY.toFixed(1)}</b>
              <small className="billing-cny-badge">CNY 折算</small>
            </div>
          </div>
        </div>
      </section>

      {/* 🌟 2. Komari / Nezha 核心互交：分组 Tab 与智能工具栏 (Toolbar) */}
      <section className="komari-toolbar-container">
        {/* 区域分组标签页 */}
        <div className="komari-group-tabs" role="tablist" aria-label="服务器地区分组">
          <button
            type="button"
            role="tab"
            aria-selected={groupTab === 'all'}
            className={`komari-tab-btn ${groupTab === 'all' ? 'active' : ''}`}
            onClick={() => setGroupTab('all')}
          >
            <span>全部</span>
            <b className="tab-count mono">{countsByGroup.all}</b>
          </button>

          <button
            type="button"
            role="tab"
            aria-selected={groupTab === 'asia'}
            className={`komari-tab-btn ${groupTab === 'asia' ? 'active' : ''}`}
            onClick={() => setGroupTab('asia')}
          >
            <span>🇨🇳 亚太</span>
            {countsByGroup.asia > 0 && <b className="tab-count mono">{countsByGroup.asia}</b>}
          </button>

          <button
            type="button"
            role="tab"
            aria-selected={groupTab === 'america'}
            className={`komari-tab-btn ${groupTab === 'america' ? 'active' : ''}`}
            onClick={() => setGroupTab('america')}
          >
            <span>🇺🇸 美洲</span>
            {countsByGroup.america > 0 && <b className="tab-count mono">{countsByGroup.america}</b>}
          </button>

          <button
            type="button"
            role="tab"
            aria-selected={groupTab === 'europe'}
            className={`komari-tab-btn ${groupTab === 'europe' ? 'active' : ''}`}
            onClick={() => setGroupTab('europe')}
          >
            <span>🇪🇺 欧洲</span>
            {countsByGroup.europe > 0 && <b className="tab-count mono">{countsByGroup.europe}</b>}
          </button>

          <button
            type="button"
            role="tab"
            aria-selected={groupTab === 'direct'}
            className={`komari-tab-btn ${groupTab === 'direct' ? 'active' : ''}`}
            onClick={() => setGroupTab('direct')}
          >
            <span>⚡ 直连优化</span>
            {countsByGroup.direct > 0 && <b className="tab-count mono">{countsByGroup.direct}</b>}
          </button>

          <button
            type="button"
            role="tab"
            aria-selected={groupTab === 'other'}
            className={`komari-tab-btn ${groupTab === 'other' ? 'active' : ''}`}
            onClick={() => setGroupTab('other')}
          >
            <span>🌐 其他</span>
            {countsByGroup.other > 0 && <b className="tab-count mono">{countsByGroup.other}</b>}
          </button>
        </div>

        {/* 搜索、状态筛选、多维排序与视图切换 */}
        <div className="komari-controls-row">
          {/* 实时搜索 */}
          <div className="komari-search-wrapper">
            <MagnifyingGlass size={16} className="search-icon" />
            <input
              type="text"
              className="komari-search-input"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="搜索服务器名称、IP、标签、地区..."
              aria-label="搜索服务器"
            />
            {query && (
              <button
                type="button"
                className="search-clear-btn"
                onClick={() => setQuery('')}
                aria-label="清空搜索"
              >
                <X size={13} />
              </button>
            )}
          </div>

          <div className="komari-filter-actions">
            {/* 在线状态筛选 */}
            <div className="filter-group komari-status-filters" role="group" aria-label="在线状态筛选">
              {['全部', '在线', '需关注', '离线'].map((item) => (
                <button
                  key={item}
                  type="button"
                  className={statusFilter === item ? 'filter-active' : ''}
                  onClick={() => setStatusFilter(item)}
                >
                  {item}
                </button>
              ))}
            </div>

            {/* 多维排序下拉 */}
            <div className="komari-sort-wrapper">
              <SortAscending size={15} className="sort-icon" />
              <select
                className="komari-sort-select"
                value={sortBy}
                onChange={(e) => setSortBy(e.target.value)}
                aria-label="服务器排序方式"
              >
                <option value="default">默认排列</option>
                <option value="status">在线状态优先</option>
                <option value="downRate">实时下载流速 ↓</option>
                <option value="upRate">实时上传流速 ↑</option>
                <option value="cpu">CPU 占用率从高到低</option>
                <option value="mem">内存占用率从高到低</option>
                <option value="remainingValue">剩余价值从高到低</option>
                <option value="dueDate">到期时间临近优先</option>
                <option value="name">节点名称 (A-Z)</option>
              </select>
            </div>

            {/* 3 种视图模式切换 (卡片 / 表格 / 极简) */}
            <div className="view-mode-toggles">
              <button
                type="button"
                className={`view-toggle-btn ${viewMode === 'grid' ? 'active' : ''}`}
                onClick={() => setViewMode('grid')}
                title="哪吒 / Komari 经典卡片视图"
              >
                <SquaresFour size={15} weight={viewMode === 'grid' ? 'bold' : 'regular'} />
                <span>卡片</span>
              </button>
              <button
                type="button"
                className={`view-toggle-btn ${viewMode === 'table' ? 'active' : ''}`}
                onClick={() => setViewMode('table')}
                title="紧凑运维表格视图"
              >
                <Rows size={15} weight={viewMode === 'table' ? 'bold' : 'regular'} />
                <span>表格</span>
              </button>
              <button
                type="button"
                className={`view-toggle-btn ${viewMode === 'compact' ? 'active' : ''}`}
                onClick={() => setViewMode('compact')}
                title="Komari 极简胶囊视图"
              >
                <Pulse size={15} weight={viewMode === 'compact' ? 'bold' : 'regular'} />
                <span>极简</span>
              </button>
            </div>
          </div>
        </div>
      </section>

      {/* 🌟 3. 100% 满屏服务器列表 (Full Width Grid/Table/Compact) */}
      <section className="full-width-nodes-section">
        <NodeTable
          nodes={filteredNodes}
          rates={rates}
          lossRates={lossRates}
          selectedId={selectedId}
          onSelect={onSelectNode}
          viewMode={viewMode}
          onToggleViewMode={setViewMode}
          hideViewToggle={true}
        />
      </section>

      {/* 🌟 4. 底部全宽运维情报与告警摘要 (4-Column Insights Grid) */}
      <section className="panel-sub-insights" aria-label="全网运维排行与告警">
        <RankCard
          title="CPU 占用 Top5"
          hint="当前 CPU 使用率排行"
          icon={Cpu}
          tone="mint"
          items={cpuTop}
        />
        <RankCard
          title="内存占用 Top5"
          hint="物理内存使用率排行"
          icon={Memory}
          tone="blue"
          items={memTop}
        />
        <RankCard
          title="丢包 Top5"
          hint="链路聚合丢包率排行"
          icon={Package}
          tone="amber"
          items={lossTop}
        />
        <section className="panel recent-alerts-panel">
          <div className="panel-header">
            <div>
              <h2>最近告警事件</h2>
              <p>{alerts.length ? `共 ${alerts.length} 条 open / acked 告警` : '暂无告警事件'}</p>
            </div>
            <button className="text-button" onClick={() => onNavigate('alerts')}>
              查看全部 <ArrowUpRight size={15} />
            </button>
          </div>
          <RecentAlerts alerts={alerts} onNavigate={onNavigate} />
        </section>
      </section>

      {/* 🌟 5. 哪吒风格一键添加服务器弹窗 (AddServerModal) */}
      {showAddServer && (
        <AddServerModal
          onClose={() => setShowAddServer(false)}
          onInstalled={() => {
            setShowAddServer(false)
            onRefresh && onRefresh()
          }}
        />
      )}
    </div>
  )
}
